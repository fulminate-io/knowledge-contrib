// SPDX-License-Identifier: Apache-2.0

// Package slackapi is a hand-written client for the read methods of the Slack
// Web API this collector calls. Each call is one GET of
// <base>/<method>?<args> with the token in the Authorization header; a 429 is
// retried after the Retry-After the response names, and every other outcome
// is returned to the caller typed: [*APIError] for an `"ok": false` body,
// [*RateLimitError] for a rate limit the client stopped retrying, and
// [*StatusError] for any other non-2xx status.
package slackapi

import (
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
)

const (
	// DefaultBaseURL is the Slack Web API root.
	DefaultBaseURL = "https://slack.com/api"

	// MaxAttempts is how many requests one call makes while the answer is 429,
	// each after the Retry-After the previous answer named. Sized by the
	// decision of 2026-10-15 on the API-key collectors' retry and pacing numbers.
	MaxAttempts = 3

	// RateLimitWindow is the window Slack's rate-limit tiers are counted over:
	// every tier is stated as requests per minute.
	RateLimitWindow = time.Minute

	// RetryDeadline bounds the total time one call waits on Retry-After: one
	// rate-limit window for each retry after the first attempt.
	RetryDeadline = (MaxAttempts - 1) * RateLimitWindow

	// errorBodyLimit is how many bytes of a non-2xx body a StatusError keeps.
	errorBodyLimit = 2048
)

// WaitFunc blocks for d or until ctx is done, returning ctx.Err() in the
// second case.
type WaitFunc func(ctx context.Context, d time.Duration) error

// Client calls the Slack Web API with one token.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string

	// Wait is how the client sleeps on a Retry-After. Nil means a timer that
	// honors the context.
	Wait WaitFunc
}

// New builds the client the shipped binary uses: the Slack API root and an
// http.Client whose transport honors the proxy environment.
func New(token string) *Client {
	return NewAt(DefaultBaseURL, &http.Client{}, token)
}

// NewAt builds a client against baseURL through httpClient.
func NewAt(baseURL string, httpClient *http.Client, token string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient, token: token}
}

// APIError is an HTTP 200 answer whose body carries `"ok": false`. Code is
// Slack's `error` value; Needed and Provided are the scope lists Slack sends
// beside missing_scope.
type APIError struct {
	Method   string
	Code     string
	Needed   string
	Provided string
}

func (e *APIError) Error() string {
	if e.Needed != "" {
		return fmt.Sprintf("slack %s answered %s (needed %s, provided %s)", e.Method, e.Code, e.Needed, e.Provided)
	}
	return fmt.Sprintf("slack %s answered %s", e.Method, e.Code)
}

// RateLimitError is a call that ended on HTTP 429. Reason says why the client
// did not retry it again: the attempts were spent, the response carried no
// usable Retry-After, or the requested wait exceeds what remains of
// RetryDeadline.
type RateLimitError struct {
	Method     string
	RetryAfter string
	Reason     string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("slack %s answered 429: %s", e.Method, e.Reason)
}

// StatusError is a non-2xx answer other than 429.
type StatusError struct {
	Method     string
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("slack %s answered HTTP %d: %s", e.Method, e.StatusCode, e.Body)
}

// envelope is the part of every Web API body this client reads itself.
type envelope struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error"`
	Needed   string `json:"needed"`
	Provided string `json:"provided"`
}

// Call issues method with args and decodes a successful body into out.
func (c *Client) Call(ctx context.Context, method string, args url.Values, out any) error {
	body, err := c.do(ctx, method, args)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("slack %s: decoding the answer: %w", method, err)
	}
	if !env.OK {
		return &APIError{Method: method, Code: env.Error, Needed: env.Needed, Provided: env.Provided}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("slack %s: decoding the answer: %w", method, err)
	}
	return nil
}

// do returns the body of the first non-429 answer, retrying a 429 up to
// MaxAttempts requests in total and waiting no longer than RetryDeadline
// across them.
func (c *Client) do(ctx context.Context, method string, args url.Values) ([]byte, error) {
	waited := time.Duration(0)
	for attempt := 1; ; attempt++ {
		ans, err := c.attempt(ctx, method, args)
		if err != nil {
			return nil, err
		}
		if ans.status != http.StatusTooManyRequests {
			if ans.status < 200 || ans.status >= 300 {
				return nil, &StatusError{Method: method, StatusCode: ans.status, Body: string(ans.body)}
			}
			return ans.body, nil
		}
		wait, rlErr := c.retryWait(method, ans.retryAfter, attempt, waited)
		if rlErr != nil {
			return nil, rlErr
		}
		if err := c.wait(ctx, wait); err != nil {
			return nil, err
		}
		waited += wait
	}
}

// answer is one HTTP response, read and closed.
type answer struct {
	status     int
	retryAfter string
	body       []byte
}

// attempt issues one request and reads its body: the whole body of a 2xx,
// and the first errorBodyLimit bytes of any other status.
func (c *Client) attempt(ctx context.Context, method string, args url.Values) (answer, error) {
	resp, err := c.get(ctx, method, args)
	if err != nil {
		return answer{}, err
	}
	defer resp.Body.Close()
	reader := io.Reader(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		reader = io.LimitReader(resp.Body, errorBodyLimit)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return answer{}, fmt.Errorf("slack %s: reading the answer: %w", method, err)
	}
	return answer{status: resp.StatusCode, retryAfter: resp.Header.Get("Retry-After"), body: body}, nil
}

// retryWait returns how long to wait before the next attempt after a 429, or
// the RateLimitError that ends the call.
func (c *Client) retryWait(method, header string, attempt int, waited time.Duration) (time.Duration, error) {
	if header == "" {
		return 0, &RateLimitError{Method: method, Reason: "the response carried no Retry-After header"}
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || seconds < 0 {
		return 0, &RateLimitError{Method: method, RetryAfter: header,
			Reason: fmt.Sprintf("the Retry-After header %q is not a whole number of seconds", header)}
	}
	if attempt >= MaxAttempts {
		return 0, &RateLimitError{Method: method, RetryAfter: header,
			Reason: fmt.Sprintf("still rate-limited after %d attempts (last Retry-After %ss)", attempt, header)}
	}
	wait := time.Duration(seconds) * time.Second
	if waited+wait > RetryDeadline {
		return 0, &RateLimitError{Method: method, RetryAfter: header,
			Reason: fmt.Sprintf("Retry-After %ss exceeds the %s left of this read's %s retry deadline",
				header, RetryDeadline-waited, RetryDeadline)}
	}
	return wait, nil
}

func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.Wait != nil {
		return c.Wait(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// get issues one authenticated GET of method with args as the query string.
func (c *Client) get(ctx context.Context, method string, args url.Values) (*http.Response, error) {
	target := c.baseURL + "/" + url.PathEscape(method)
	if len(args) > 0 {
		target += "?" + args.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("slack %s: building the request: %w", method, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req) //nolint:gosec // the host is the configured API root; only the method name and query vary
	if err != nil {
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			return nil, fmt.Errorf("slack %s: %w", method, urlErr.Err)
		}
		return nil, fmt.Errorf("slack %s: %w", method, err)
	}
	return resp, nil
}
