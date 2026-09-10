// SPDX-License-Identifier: Apache-2.0

// Package bbclient is a hand-written HTTP client for the Bitbucket REST API
// v2.0: page-following enumeration, retry on the one status the provider uses to
// rate-limit, and a typed error carrying the status a caller classifies on.
//
// WHY A HAND-WRITTEN CLIENT AND NOT AN SDK. The built-in provider this module
// reproduces uses one, and its pagination shape, its retry budget and its error
// type are what this module's parity is measured against; an SDK would change
// all three while the graph looked the same. Adopting one is explicitly out of
// this module's scope.
//
// THE SEAM IS THE BASE URL AND THE *http.Client, which is what makes every
// behavior below testable with no credential and no network: a test stands a
// real HTTP server in for the provider and both sides of the seam are real. The
// shipped binary reaches only [New], which takes neither.
package bbclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the provider's own API root.
	DefaultBaseURL = "https://api.bitbucket.org/2.0"
	// DefaultTimeout bounds one request end to end. It is the source provider's
	// value; a request that exceeds it is a transport failure, and the walk
	// reports the read it was making as incomplete rather than empty.
	DefaultTimeout = 30 * time.Second
	// MaxPagelen is the provider's own maximum items per page.
	MaxPagelen = 100
	// MaxRetries is how many times a 429 is retried INSIDE the retry loop.
	//
	// IT IS NOT THE NUMBER OF REQUESTS A RATE-LIMITED URL RECEIVES. The loop runs
	// this many attempts and is followed by one unconditional final request, so a
	// persistently rate-limited URL is fetched FOUR times. That is the source
	// provider's shape and it is reproduced rather than tidied, because the
	// request budget a provider sees is part of what this collector is.
	MaxRetries = 3
)

// Client is a thin HTTP client for the Bitbucket REST API v2.0. Auth is HTTP
// Basic with a username and an app password.
type Client struct {
	baseURL     string
	username    string
	appPassword string
	httpClient  *http.Client
}

// New builds the client the shipped binary uses: the provider's own base URL and
// the default timeout.
func New(username, appPassword string) *Client {
	return NewAt(DefaultBaseURL, &http.Client{Timeout: DefaultTimeout}, username, appPassword)
}

// NewAt builds a client against a caller-supplied base URL and HTTP client.
//
// IT EXISTS FOR THIS MODULE'S OWN TESTS, which stand a real HTTP server in for
// the provider so the pagination, the retry budget, the Retry-After parse, the
// cancellation arm and every per-status outcome are exercised offline. It is in
// an internal package of this module, so nothing an operator builds or installs
// can reach it: the binary's only path to a client is [New].
func NewAt(baseURL string, httpClient *http.Client, username, appPassword string) *Client {
	return &Client{
		baseURL:     baseURL,
		username:    username,
		appPassword: appPassword,
		httpClient:  httpClient,
	}
}

// APIError is a non-2xx response from the Bitbucket API. It carries the STATUS
// because that is what a caller classifies on: a refusal, a rate limit and an
// outage arrive on the same call and differ only in the status, and a classifier
// that matched on message text would turn an outage into a permanent "grant a
// permission" instruction the operator cannot act on.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bitbucket API %d: %s", e.StatusCode, e.Body)
}

// GetPaginated iterates every page of a paginated endpoint. A Bitbucket page is
// `{"values": [...], "next": "url"}`; handler is called once per page with the
// raw `values` array.
//
// THE CONTEXT IS CHECKED AT THE TOP OF EVERY PAGE, so a cancelled walk stops
// following `next` rather than draining a large enumeration it will discard.
func (c *Client) GetPaginated(
	ctx context.Context,
	path string,
	handler func(raw json.RawMessage) error,
) error {
	url := c.resolveURL(path)
	if !strings.Contains(url, "pagelen=") {
		separator := "?"
		if strings.Contains(url, "?") {
			separator = "&"
		}
		url += separator + "pagelen=" + strconv.Itoa(MaxPagelen)
	}

	for url != "" {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, nextURL, err := c.fetchPage(ctx, url)
		if err != nil {
			return err
		}
		if page != nil {
			if err := handler(page); err != nil {
				return fmt.Errorf("page handler: %w", err)
			}
		}
		url = nextURL
	}
	return nil
}

// paginatedResponse captures the Bitbucket pagination envelope.
type paginatedResponse struct {
	Values json.RawMessage `json:"values"`
	Next   string          `json:"next"`
}

// fetchPage fetches one page and returns its values array and the next URL.
func (c *Client) fetchPage(
	ctx context.Context, url string,
) (json.RawMessage, string, error) {
	resp, err := c.doWithRetry(ctx, url)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", readAPIError(resp)
	}

	var page paginatedResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, "", fmt.Errorf("decode page: %w", err)
	}
	return page.Values, page.Next, nil
}

// GetRaw performs one GET and returns the whole body. It is for the endpoints
// that answer with something other than a pagination envelope — a repository's
// bitbucket-pipelines.yml is fetched through it.
func (c *Client) GetRaw(ctx context.Context, path string) ([]byte, error) {
	resp, err := c.doWithRetry(ctx, c.resolveURL(path))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readAPIError(resp)
	}
	return io.ReadAll(resp.Body)
}

// resolveURL returns an absolute URL. A `next` URL arrives absolute and is
// returned as-is; anything else is joined to the base.
func (c *Client) resolveURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

// readAPIError reads a bounded prefix of an error body and wraps it.
//
// THE BODY IS BOUNDED because an error body is a diagnostic rather than data: a
// provider answering an unbounded body on a failure would otherwise be read into
// memory in full and carried into a log line.
func readAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit)) //nolint:errcheck // a truncated diagnostic is still the diagnostic
	return &APIError{StatusCode: resp.StatusCode, Body: string(body)}
}

// errorBodyLimit bounds how much of a failure response is carried into the
// error. It is the source provider's value.
const errorBodyLimit = 4096

// doWithRetry executes one GET, retrying only on 429.
//
// THE CREDENTIAL RIDES SetBasicAuth AND NEVER THE URL. Every diagnostic below
// logs the url, so a credential in it would reach an operator's log; building
// one with userinfo is the mistake this comment exists to prevent.
//
// THE ATTEMPT COUNT IS FOUR, NOT THREE, on a persistently rate-limited URL: the
// loop runs [MaxRetries] attempts and the request after it is unconditional.
// That is the source provider's shape, reproduced deliberately.
func (c *Client) doWithRetry(ctx context.Context, url string) (*http.Response, error) {
	for attempt := range MaxRetries {
		resp, err := c.get(ctx, url)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		resp.Body.Close()
		wait := ParseRetryAfter(resp.Header.Get("Retry-After"))
		slog.Warn("bitbucket-pipelines: rate limited",
			"attempt", attempt+1, "wait", wait, "url", url)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	// The final attempt. Whatever it answers is returned, including another 429,
	// which the caller classifies as a rate-limited read rather than as a refusal
	// or an outage.
	return c.get(ctx, url)
}

// get issues one authenticated GET.
func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil) //nolint:gosec // URL from trusted API path
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(c.username, c.appPassword)
	resp, err := c.httpClient.Do(req) //nolint:gosec // URL from trusted API path
	if err != nil {
		return nil, fmt.Errorf("http GET %s: %w", url, err)
	}
	return resp, nil
}

// ParseRetryAfter reads a Retry-After header value in seconds.
//
// EVERY ARM IS THE SOURCE PROVIDER'S: absent or unparseable is two seconds, a
// zero or negative value is floored at one second rather than taken literally,
// and a positive integer is honored. A zero taken literally would turn a
// rate-limit into a hot loop against the provider.
func ParseRetryAfter(header string) time.Duration {
	if header == "" {
		return 2 * time.Second
	}
	seconds, err := strconv.Atoi(header)
	if err != nil {
		return 2 * time.Second
	}
	if seconds <= 0 {
		return 1 * time.Second
	}
	return time.Duration(seconds) * time.Second
}
