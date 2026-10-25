// SPDX-License-Identifier: Apache-2.0

// Package notionapi is a hand-written client for the Notion API endpoints this
// collector reads. Every request carries the token as a bearer credential and
// the Notion-Version header. A 429 or 529 is retried after the Retry-After the
// response names; every other non-2xx answer is returned as an [*APIError]
// carrying Notion's error object, and a rate limit the client stopped retrying
// as a [*RateLimitError].
package notionapi

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
)

const (
	// DefaultBaseURL is the Notion API root.
	DefaultBaseURL = "https://api.notion.com/v1"

	// Version is the Notion-Version header every request sends.
	Version = "2026-03-11"

	// PageSize is the page_size every paginated request sends, Notion's
	// documented maximum.
	PageSize = 100

	// MaxAttempts is how many requests one call makes while the answer is 429
	// or 529, each after the Retry-After the previous answer named. Sized by the
	// decision of 2026-10-15 on the API-key collectors' retry and pacing numbers.
	MaxAttempts = 3

	// ConnectionRetryAfterMax is the longest Retry-After Notion documents for
	// its per-connection rate limit.
	ConnectionRetryAfterMax = 60 * time.Second

	// RetryDeadline bounds the total time one call waits on Retry-After: one
	// per-connection maximum for each retry after the first attempt.
	RetryDeadline = (MaxAttempts - 1) * ConnectionRetryAfterMax

	// StatusServiceOverload is the status Notion answers when it is
	// overloaded; it is retried like a 429.
	StatusServiceOverload = 529

	// BlockedReason is the rate_limit_reason of a 429 that no retry clears.
	BlockedReason = "public_api_request_blocked"

	// errorBodyLimit is how many bytes of a non-2xx body are read.
	errorBodyLimit = 4096
)

// WaitFunc blocks for d or until ctx is done, returning ctx.Err() in the
// second case.
type WaitFunc func(ctx context.Context, d time.Duration) error

// Client calls the Notion API with one token.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string

	// Wait is how the client sleeps on a Retry-After. Nil means a timer that
	// honors the context.
	Wait WaitFunc
}

// New builds the client the shipped binary uses: the Notion API root and an
// http.Client whose transport honors the proxy environment.
func New(token string) *Client {
	return NewAt(DefaultBaseURL, &http.Client{}, token)
}

// NewAt builds a client against baseURL through httpClient.
func NewAt(baseURL string, httpClient *http.Client, token string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient, token: token}
}

// APIError is a non-2xx answer other than a retried rate limit. Code,
// Message and RateLimitReason come from Notion's error object when the body
// carries one.
type APIError struct {
	Method          string
	Path            string
	Status          int
	Code            string
	Message         string
	RateLimitReason string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("notion %s %s answered %d %s", e.Method, e.Path, e.Status, e.Code)
	if e.RateLimitReason != "" {
		msg += " (" + e.RateLimitReason + ")"
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// RateLimitError is a call that ended on 429 or 529. Reason says why the
// client did not retry it again.
type RateLimitError struct {
	Method     string
	Path       string
	Status     int
	RetryAfter string
	Reason     string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("notion %s %s answered %d: %s", e.Method, e.Path, e.Status, e.Reason)
}

// errorObject is Notion's error body.
type errorObject struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	AdditionalData struct {
		RateLimitReason string `json:"rate_limit_reason"`
	} `json:"additional_data"`
}

// get issues GET path?query and decodes a 2xx body into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	target := path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return c.call(ctx, http.MethodGet, target, nil, out)
}

// post issues POST path with body as JSON and decodes a 2xx body into out.
func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("notion POST %s: encoding the request body: %w", path, err)
	}
	return c.call(ctx, http.MethodPost, path, raw, out)
}

// call returns after the first answer that is not a retried rate limit,
// making up to MaxAttempts requests and waiting no longer than RetryDeadline
// across them.
func (c *Client) call(ctx context.Context, method, target string, body []byte, out any) error {
	path, _, _ := strings.Cut(target, "?")
	waited := time.Duration(0)
	for attempt := 1; ; attempt++ {
		ans, err := c.attempt(ctx, method, target, body)
		if err != nil {
			return err
		}
		if ans.status >= 200 && ans.status < 300 {
			if err := json.Unmarshal(ans.body, out); err != nil {
				return fmt.Errorf("notion %s %s: decoding the answer: %w", method, path, err)
			}
			return nil
		}
		var obj errorObject
		_ = json.Unmarshal(ans.body, &obj) //nolint:errcheck // a body that is not Notion's error object leaves the fields empty
		limited := ans.status == http.StatusTooManyRequests || ans.status == StatusServiceOverload
		if !limited || obj.AdditionalData.RateLimitReason == BlockedReason {
			apiErr := &APIError{Method: method, Path: path, Status: ans.status, Code: obj.Code,
				Message: obj.Message, RateLimitReason: obj.AdditionalData.RateLimitReason}
			if obj.Code == "" && obj.Message == "" {
				apiErr.Message = string(ans.body)
			}
			return apiErr
		}
		wait, rlErr := retryWait(method, path, ans, attempt, waited)
		if rlErr != nil {
			return rlErr
		}
		if err := c.wait(ctx, wait); err != nil {
			return err
		}
		waited += wait
	}
}

// retryWait returns how long to wait before the next attempt after a 429 or
// 529, or the RateLimitError that ends the call.
func retryWait(method, path string, ans answer, attempt int, waited time.Duration) (time.Duration, error) {
	limit := func(reason string) error {
		return &RateLimitError{Method: method, Path: path, Status: ans.status, RetryAfter: ans.retryAfter, Reason: reason}
	}
	if ans.retryAfter == "" {
		return 0, limit("the response carried no Retry-After header")
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(ans.retryAfter))
	if err != nil {
		return 0, limit(fmt.Sprintf("the Retry-After header %q is not a whole number of seconds: %v", ans.retryAfter, err))
	}
	if seconds < 0 {
		return 0, limit(fmt.Sprintf("the Retry-After header %q is negative", ans.retryAfter))
	}
	if attempt >= MaxAttempts {
		return 0, limit(fmt.Sprintf("still rate-limited after %d attempts (last Retry-After %ss)", attempt, ans.retryAfter))
	}
	wait := time.Duration(seconds) * time.Second
	if waited+wait > RetryDeadline {
		return 0, limit(fmt.Sprintf("Retry-After %ss exceeds the %s left of this read's %s retry deadline",
			ans.retryAfter, RetryDeadline-waited, RetryDeadline))
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

// endpoint resolves target, a path relative to the API root with an optional
// query, against the base URL.
func (c *Client) endpoint(target string) (string, error) {
	ref, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("notion: the request target %q does not parse: %w", target, err)
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("notion: the API root %q does not parse: %w", c.baseURL, err)
	}
	u := base.JoinPath(ref.Path)
	u.RawQuery = ref.RawQuery
	return u.String(), nil
}

// answer is one HTTP response, read and closed.
type answer struct {
	status     int
	retryAfter string
	body       []byte
}

// attempt issues one request and reads its body: the whole body of a 2xx,
// and the first errorBodyLimit bytes of any other status.
func (c *Client) attempt(ctx context.Context, method, target string, body []byte) (answer, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	endpoint, err := c.endpoint(target)
	if err != nil {
		return answer{}, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return answer{}, fmt.Errorf("notion %s %s: building the request: %w", method, target, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Notion-Version", Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req) //nolint:gosec // the host is the configured API root; only the path and query vary
	if err != nil {
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			return answer{}, fmt.Errorf("notion %s: %w", method, urlErr.Err)
		}
		return answer{}, fmt.Errorf("notion %s: %w", method, err)
	}
	defer resp.Body.Close()
	limited := io.Reader(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited = io.LimitReader(resp.Body, errorBodyLimit)
	}
	raw, err := io.ReadAll(limited)
	if err != nil {
		return answer{}, fmt.Errorf("notion %s: reading the answer: %w", method, err)
	}
	return answer{status: resp.StatusCode, retryAfter: resp.Header.Get("Retry-After"), body: raw}, nil
}
