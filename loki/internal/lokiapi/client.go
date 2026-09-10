// SPDX-License-Identifier: Apache-2.0

package lokiapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// client.go — the HTTP client, built from the parsed environment, and the one
// request function every call goes through.
//
// NEVER http.DefaultClient: it has no timeout and a Loki that accepts a
// connection and never answers would hang the collect forever. One client is
// built per collect and shared by every page, which is what keeps the
// connection pool useful across a multi-page walk.
//
// EVERY REQUEST TAKES THE CALLER'S CONTEXT. The context arrives as the tool
// handler's, is threaded through the paging loop, and reaches every request, so
// a cancelled collect stops at the next page boundary rather than running to
// completion into a caller that has gone away.

// requestTimeout bounds ONE request, not the collect. A collect's own bound is
// its caller's context: this collector imposes no ceiling of its own on how
// long a walk may take or how much it may return.
const requestTimeout = 30 * time.Second

// Transport-level timeouts. They are set explicitly rather than left at the
// package defaults so a hung TLS handshake or a server that accepts and never
// responds fails at a named boundary.
const (
	dialTimeout           = 10 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	responseHeaderTimeout = 30 * time.Second
	idleConnTimeout       = 90 * time.Second
	expectContinueTimeout = 1 * time.Second
)

// Client speaks Loki's HTTP API at one address with one set of credentials.
type Client struct {
	address  string
	settings Settings
	bearer   string
	http     *http.Client
}

// NewClient parses and validates the address, resolves the credentials and
// builds the transport.
//
// THE ADDRESS IS VALIDATED HERE, BEFORE ANY REQUEST IS BUILT. It arrives as a
// tool parameter from an operator or an agent, and the knowledge client's
// pre-call check enforces only that the argument is a required string — so
// whatever the string says reaches this function. A relative reference, a
// file:// scheme or a URL with no host would otherwise become a request against
// something nobody meant.
func NewClient(address string, s Settings) (*Client, error) {
	normalized, err := NormalizeAddress(address)
	if err != nil {
		return nil, err
	}
	bearer, err := resolveBearer(s)
	if err != nil {
		return nil, err
	}
	transport, err := buildTransport(s)
	if err != nil {
		return nil, err
	}
	return &Client{
		address:  normalized,
		settings: s,
		bearer:   bearer,
		http:     &http.Client{Timeout: requestTimeout, Transport: transport},
	}, nil
}

// Address is the normalized endpoint this client talks to.
func (c *Client) Address() string { return c.address }

// NormalizeAddress parses an operator-supplied Loki address and returns it with
// any trailing slash removed, so path concatenation downstream cannot produce a
// double slash.
//
// It refuses anything that is not an absolute http or https URL with a host.
// The three refused shapes each have a distinct message, because "invalid
// address" tells an operator nothing about which part of theirs is wrong.
func NormalizeAddress(address string) (string, error) {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return "", fmt.Errorf("lokiapi: the Loki address is empty; it names the endpoint this collect reads from")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("lokiapi: the Loki address %q does not parse as a URL: %w", address, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf(
			"lokiapi: the Loki address %q has scheme %q; it must be an absolute http or https URL, for example http://localhost:3100",
			address, u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("lokiapi: the Loki address %q names no host", address)
	}
	return strings.TrimRight(trimmed, "/"), nil
}

// resolveBearer reads the bearer token, from the file when one is named.
//
// A NAMED FILE THAT CANNOT BE READ IS AN ERROR NAMING BOTH THE VARIABLE AND THE
// PATH. Nothing upstream sees such a failure: the collector runs as a spawned
// child whose working directory is a temporary one and whose environment is
// exactly its config entry, so a mistyped path would otherwise become an
// unauthenticated request that Loki refuses for a reason the operator cannot
// connect to their typo.
func resolveBearer(s Settings) (string, error) {
	if s.BearerTokenFile == "" {
		return s.BearerToken, nil
	}
	raw, err := os.ReadFile(s.BearerTokenFile)
	if err != nil {
		return "", fmt.Errorf("lokiapi: reading the bearer token named by %s=%q: %w", EnvBearerTokenFile, s.BearerTokenFile, err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("lokiapi: the file named by %s=%q is empty; it must hold the bearer token",
			EnvBearerTokenFile, s.BearerTokenFile)
	}
	return token, nil
}

// buildTransport assembles the TLS and proxy configuration.
//
// THE PROXY DECISION IS MADE ONCE, HERE, and that placement matters: net/http's
// environment proxy function reads its six variables at the FIRST request and
// caches the answer for the life of the process, so a proxy chosen after a
// request has gone out decides nothing.
func buildTransport(s Settings) (*http.Transport, error) {
	tlsConfig, err := buildTLSConfig(s)
	if err != nil {
		return nil, err
	}
	t := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       tlsConfig,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		IdleConnTimeout:       idleConnTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
		ForceAttemptHTTP2:     true,
		// Compression is negotiated explicitly rather than left to the
		// transport's own transparent gzip, so the flag an operator sets is the
		// flag that decides it.
		DisableCompression: !s.HTTPCompression,
	}
	switch {
	case s.HTTPProxyURL != "":
		proxyURL, err := url.Parse(s.HTTPProxyURL)
		if err != nil {
			return nil, fmt.Errorf("lokiapi: %s=%q does not parse as a URL: %w", EnvHTTPProxyURL, s.HTTPProxyURL, err)
		}
		if proxyURL.Host == "" {
			return nil, fmt.Errorf("lokiapi: %s=%q names no host", EnvHTTPProxyURL, s.HTTPProxyURL)
		}
		t.Proxy = http.ProxyURL(proxyURL)
	case s.EnvProxy:
		t.Proxy = http.ProxyFromEnvironment
	default:
		// NO PROXY AT ALL, stated rather than left to the default. The
		// transport's zero Proxy is nil, which is already "no proxy", but a
		// reader has to know that an unset LOKI_ENV_PROXY means the six
		// standard proxy variables are IGNORED rather than quietly honored.
		t.Proxy = nil
	}
	return t, nil
}

// buildTLSConfig assembles the certificate authority and client certificate.
//
// EVERY PATH IT OPENS IS A PATH THE OPERATOR NAMED, and a failure to open one
// is an error naming the variable and the path for the same reason
// resolveBearer's is: the alternative is a TLS failure the operator cannot
// trace back to the entry they wrote.
func buildTLSConfig(s Settings) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12} //nolint:gosec // InsecureSkipVerify is set only from the operator's own explicit LOKI_TLS_SKIP_VERIFY
	if s.TLSSkipVerify {
		cfg.InsecureSkipVerify = true
	}
	if s.CACertPath != "" {
		pem, err := os.ReadFile(s.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("lokiapi: reading the certificate authority named by %s=%q: %w", EnvCACertPath, s.CACertPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("lokiapi: the file named by %s=%q holds no PEM certificate", EnvCACertPath, s.CACertPath)
		}
		cfg.RootCAs = pool
	}
	if s.ClientCertPath != "" {
		cert, err := tls.LoadX509KeyPair(s.ClientCertPath, s.ClientKeyPath)
		if err != nil {
			return nil, fmt.Errorf("lokiapi: loading the client certificate named by %s=%q and %s=%q: %w",
				EnvClientCertPath, s.ClientCertPath, EnvClientKeyPath, s.ClientKeyPath, err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// get issues one GET against a path on the Loki endpoint and returns the body.
//
// It retries only the classes a retry can fix — a transport failure and a 5xx —
// and never a 4xx, which is the server saying the request itself is wrong.
func (c *Client) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		body, retryable, err := c.attempt(ctx, path, params)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retryable || attempt >= c.settings.ClientRetries {
			return nil, lastErr
		}
		if err := sleepBackoff(ctx, c.settings, attempt); err != nil {
			return nil, err
		}
	}
}

// attempt issues one request and reports whether its failure is worth retrying.
func (c *Client) attempt(ctx context.Context, path string, params url.Values) (body []byte, retryable bool, err error) {
	target := c.address + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, false, fmt.Errorf("lokiapi: building a request for %s: %w", path, err)
	}
	c.applyHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		// A cancelled or expired context is the caller's decision, not a
		// transient failure, so it is never retried.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, false, fmt.Errorf("lokiapi: requesting %s from %s: %w", path, c.address, err)
		}
		return nil, true, fmt.Errorf("lokiapi: requesting %s from %s: %w", path, c.address, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("lokiapi: reading the response to %s from %s: %w", path, c.address, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode >= 500, fmt.Errorf("lokiapi: %s answered %s with HTTP %d: %s",
			c.address, path, resp.StatusCode, excerpt(raw))
	}
	return raw, false, nil
}

// applyHeaders writes the credentials and the request-behavior headers.
// Basic auth wins over a bearer token when both are configured, matching the
// order the built-in adapter uses, and the pair check in LoadSettings is what
// keeps a half-set credential from reaching here at all.
func (c *Client) applyHeaders(req *http.Request) {
	switch {
	case c.settings.Username != "":
		req.SetBasicAuth(c.settings.Username, c.settings.Password)
	case c.bearer != "":
		req.Header.Set(c.settings.AuthHeader, "Bearer "+c.bearer)
	}
	if c.settings.OrgID != "" {
		req.Header.Set("X-Scope-OrgID", c.settings.OrgID)
	}
	if c.settings.QueryTags != "" {
		req.Header.Set("X-Query-Tags", c.settings.QueryTags)
	}
	if c.settings.NoCache {
		req.Header.Set("Cache-Control", "no-cache")
	}
	if c.settings.HTTPCompression {
		req.Header.Set("Accept-Encoding", "gzip")
	}
}

// sleepBackoff waits before the next attempt, doubling from the minimum up to
// the maximum, and returns early when the context ends so a cancelled collect
// does not sit in a sleep.
func sleepBackoff(ctx context.Context, s Settings, attempt int) error {
	wait := s.ClientMinBackoff
	for i := 0; i < attempt && wait < s.ClientMaxBackoff; i++ {
		wait *= 2
	}
	if wait > s.ClientMaxBackoff {
		wait = s.ClientMaxBackoff
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("lokiapi: waiting to retry: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// excerpt bounds an error body so a Loki page of HTML does not become the whole
// error message. It is a MESSAGE-RENDERING bound, not a bound on what this
// collector reads or returns.
func excerpt(body []byte) string {
	const max = 200
	s := strings.TrimSpace(string(body))
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
