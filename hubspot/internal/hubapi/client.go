// SPDX-License-Identifier: Apache-2.0

// Package hubapi is a hand-written client for the HubSpot CRM endpoints this
// collector reads. Every request carries the token as a bearer credential. A
// 429 is retried after the wait HubSpot's own signal names (the
// X-HubSpot-RateLimit-Interval-Milliseconds header, or one second on a search
// endpoint), a 477 after its Retry-After; a DAILY 429, a 429 with an unknown
// policyName and a 429 without its wait signal end the call as a
// [*RateLimitError]. Search requests are paced at HubSpot's per-account search
// rate. Every other non-2xx answer is returned as an [*APIError].
package hubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	// DefaultBaseURL is the HubSpot API root.
	DefaultBaseURL = "https://api.hubapi.com"

	// MaxAttempts is how many requests one call makes while it is rate-limited,
	// each after the wait HubSpot's previous answer named. Sized by the decision
	// of 2026-10-15 on the API-key collectors' retry and pacing numbers.
	MaxAttempts = 3

	// RateLimitWindow is the window HubSpot's per-app rate limit is counted
	// over: its secondly policy is TEN_SECONDLY_ROLLING.
	RateLimitWindow = 10 * time.Second

	// RetryDeadline bounds the total time one call waits on HubSpot's rate
	// limit signal: one window for each retry after the first attempt.
	RetryDeadline = (MaxAttempts - 1) * RateLimitWindow

	// SearchWindow is the wait after a search endpoint's 429, whose limit is
	// counted per second.
	SearchWindow = time.Second

	// SearchRate is how many search requests per second this client sends:
	// HubSpot's per-account search limit, the whole of the account's budget.
	// Sized by the decision of 2026-10-15 on the API-key collectors' retry and
	// pacing numbers.
	SearchRate = 5

	// StatusMigration is the status HubSpot answers while an account is being
	// migrated; its Retry-After names the wait.
	StatusMigration = 477

	// PolicyDaily and PolicySecondly are the policyName values of HubSpot's
	// two rate limits.
	PolicyDaily    = "DAILY"
	PolicySecondly = "TEN_SECONDLY_ROLLING"

	headerInterval  = "X-HubSpot-RateLimit-Interval-Milliseconds"
	headerRemaining = "X-HubSpot-RateLimit-Remaining"
	errorBodyLimit  = 4096
)

// WaitFunc blocks for d or until ctx is done, returning ctx.Err() in the
// second case.
type WaitFunc func(ctx context.Context, d time.Duration) error

// Client calls the HubSpot API with one token.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
	search     *rate.Limiter
	// pending is the wait the last answer's exhausted rate-limit window asks
	// for before the next non-search request.
	pending time.Duration

	// Wait is how the client sleeps on a rate limit. Nil means a timer that
	// honors the context.
	Wait WaitFunc
	// Pace is how the client sleeps between search requests. Nil means a
	// timer that honors the context.
	Pace WaitFunc
	// Now is the clock search pacing reads. Nil means time.Now.
	Now func() time.Time
}

// New builds the client the shipped binary uses: the HubSpot API root and an
// http.Client whose transport honors the proxy environment.
func New(token string) *Client {
	return NewAt(DefaultBaseURL, &http.Client{}, token)
}

// NewAt builds a client against baseURL through httpClient.
func NewAt(baseURL string, httpClient *http.Client, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient, token: token,
		search: rate.NewLimiter(rate.Limit(SearchRate), 1),
	}
}

// APIError is a non-2xx answer other than a retried rate limit. Category and
// Message come from HubSpot's error body when it carries them.
type APIError struct {
	Method   string
	Path     string
	Status   int
	Category string
	Message  string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("hubspot %s %s answered %d", e.Method, e.Path, e.Status)
	if e.Category != "" {
		msg += " " + e.Category
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// RateLimitError is a call that ended rate-limited. Reason says why the
// client did not retry it again.
type RateLimitError struct {
	Method string
	Path   string
	Status int
	Policy string
	Reason string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("hubspot %s %s answered %d: %s", e.Method, e.Path, e.Status, e.Reason)
}

// errorBody is HubSpot's error body; every field is optional.
type errorBody struct {
	Message    string `json:"message"`
	Category   string `json:"category"`
	ErrorType  string `json:"errorType"`
	PolicyName string `json:"policyName"`
}

// request is one call: its method, its path under the API root, its query,
// its JSON body and whether it is a search request.
type request struct {
	method string
	path   string
	query  url.Values
	body   any
	search bool
}

// do runs req and decodes a 2xx body into out, returning the status, which
// is 200 or 207 for a batch read.
func (c *Client) do(ctx context.Context, req request, out any) (int, error) {
	var raw []byte
	if req.body != nil {
		var err error
		if raw, err = json.Marshal(req.body); err != nil {
			return 0, fmt.Errorf("hubspot %s %s: encoding the request body: %w", req.method, req.path, err)
		}
	}
	waited := time.Duration(0)
	for attempt := 1; ; attempt++ {
		if err := c.beforeRequest(ctx, req.search); err != nil {
			return 0, err
		}
		ans, err := c.attempt(ctx, req, raw)
		if err != nil {
			return 0, err
		}
		if ans.status >= 200 && ans.status < 300 {
			c.noteRemaining(req.search, ans)
			if err := json.Unmarshal(ans.body, out); err != nil {
				return 0, fmt.Errorf("hubspot %s %s: decoding the answer: %w", req.method, req.path, err)
			}
			return ans.status, nil
		}
		var body errorBody
		_ = json.Unmarshal(ans.body, &body) //nolint:errcheck // a body that is not HubSpot's error object leaves the fields empty
		if ans.status != http.StatusTooManyRequests && ans.status != StatusMigration {
			apiErr := &APIError{Method: req.method, Path: req.path, Status: ans.status, Category: body.Category, Message: body.Message}
			if body.Category == "" && body.Message == "" {
				apiErr.Message = string(ans.body)
			}
			return 0, apiErr
		}
		wait, rlErr := retryWait(req, ans, body, attempt, waited)
		if rlErr != nil {
			return 0, rlErr
		}
		if err := c.wait(ctx, wait); err != nil {
			return 0, err
		}
		waited += wait
	}
}

// retryWait returns how long to wait before the next attempt after a 429 or
// 477, or the RateLimitError that ends the call.
func retryWait(req request, ans answer, body errorBody, attempt int, waited time.Duration) (time.Duration, error) {
	limit := func(reason string) error {
		return &RateLimitError{Method: req.method, Path: req.path, Status: ans.status, Policy: body.PolicyName, Reason: reason}
	}
	var wait time.Duration
	switch {
	case ans.status == StatusMigration:
		seconds, err := strconv.Atoi(strings.TrimSpace(ans.retryAfter))
		if err != nil {
			return 0, limit(fmt.Sprintf("the account is being migrated (477) and Retry-After %q is not a whole number of seconds", ans.retryAfter))
		}
		wait = time.Duration(seconds) * time.Second
	case req.search:
		wait = SearchWindow
	case body.PolicyName == PolicyDaily:
		return 0, limit("the daily request limit is spent (policyName DAILY); it resets at midnight in the account's time zone")
	case body.PolicyName != PolicySecondly:
		return 0, limit(fmt.Sprintf("the 429 names policyName %q, which is not %s", body.PolicyName, PolicySecondly))
	default:
		ms, err := strconv.Atoi(strings.TrimSpace(ans.interval))
		if err != nil {
			return 0, limit(fmt.Sprintf("the %s 429 carried no usable %s header (%q)", PolicySecondly, headerInterval, ans.interval))
		}
		wait = time.Duration(ms) * time.Millisecond
	}
	if wait < 0 {
		return 0, limit(fmt.Sprintf("the requested wait %s is negative", wait))
	}
	if attempt >= MaxAttempts {
		return 0, limit(fmt.Sprintf("still rate-limited after %d attempts", attempt))
	}
	if waited+wait > RetryDeadline {
		return 0, limit(fmt.Sprintf("the requested wait of %s exceeds the %s left of this read's %s retry deadline",
			wait, RetryDeadline-waited, RetryDeadline))
	}
	return wait, nil
}

// beforeRequest paces a search request at SearchRate, and waits out an
// exhausted rate-limit window before any other request.
func (c *Client) beforeRequest(ctx context.Context, search bool) error {
	if search {
		now := c.now()
		delay := c.search.ReserveN(now, 1).DelayFrom(now)
		if delay <= 0 {
			return nil
		}
		return c.pace(ctx, delay)
	}
	if c.pending <= 0 {
		return nil
	}
	d := c.pending
	c.pending = 0
	return c.wait(ctx, d)
}

// noteRemaining records the wait a non-search answer's exhausted window asks
// for: its interval, when X-HubSpot-RateLimit-Remaining is 0. An interval
// that is absent, not an integer or not positive records no wait.
func (c *Client) noteRemaining(search bool, ans answer) {
	if search || strings.TrimSpace(ans.remaining) != "0" {
		return
	}
	ms, err := strconv.Atoi(strings.TrimSpace(ans.interval))
	if err != nil {
		return
	}
	if ms > 0 {
		c.pending = time.Duration(ms) * time.Millisecond
	}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.Wait != nil {
		return c.Wait(ctx, d)
	}
	return sleep(ctx, d)
}

func (c *Client) pace(ctx context.Context, d time.Duration) error {
	if c.Pace != nil {
		return c.Pace(ctx, d)
	}
	return sleep(ctx, d)
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// answer is one HTTP response, read and closed.
type answer struct {
	status     int
	retryAfter string
	interval   string
	remaining  string
	body       []byte
}

// endpoint resolves path and query against the base URL.
func (c *Client) endpoint(path string, query url.Values) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("hubspot: the API root %q does not parse: %w", c.baseURL, err)
	}
	u := base.JoinPath(path)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// attempt issues one request and reads its body: the whole body of a 2xx,
// and the first errorBodyLimit bytes of any other status.
func (c *Client) attempt(ctx context.Context, req request, body []byte) (answer, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	endpoint, err := c.endpoint(req.path, req.query)
	if err != nil {
		return answer{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.method, endpoint, reader)
	if err != nil {
		return answer{}, fmt.Errorf("hubspot %s %s: building the request: %w", req.method, req.path, err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(httpReq) //nolint:gosec // the host is the configured API root; only the path and query vary
	if err != nil {
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			return answer{}, fmt.Errorf("hubspot %s %s: %w", req.method, req.path, urlErr.Err)
		}
		return answer{}, fmt.Errorf("hubspot %s %s: %w", req.method, req.path, err)
	}
	defer resp.Body.Close()
	limited := io.Reader(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited = io.LimitReader(resp.Body, errorBodyLimit)
	}
	raw, err := io.ReadAll(limited)
	if err != nil {
		return answer{}, fmt.Errorf("hubspot %s %s: reading the answer: %w", req.method, req.path, err)
	}
	return answer{
		status: resp.StatusCode, retryAfter: resp.Header.Get("Retry-After"),
		interval: resp.Header.Get(headerInterval), remaining: resp.Header.Get(headerRemaining), body: raw,
	}, nil
}
