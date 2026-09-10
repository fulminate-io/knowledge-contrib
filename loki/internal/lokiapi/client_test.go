// SPDX-License-Identifier: Apache-2.0

package lokiapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// client_test.go — address validation, the credential headers, and the retry.

// TestTheAddressIsValidatedBeforeAnyRequestIsBuilt is the obligation the
// knowledge client's own pre-call check leaves to this collector: that check
// enforces only that the argument is a required string, so whatever the string
// says arrives here.
func TestTheAddressIsValidatedBeforeAnyRequestIsBuilt(t *testing.T) {
	cases := []struct {
		name    string
		address string
		want    string
	}{
		{"empty", "", "is empty"},
		{"only whitespace", "   ", "is empty"},
		{"a bare host with no scheme", "loki.example:3100", "scheme"},
		{"a relative path", "/loki/api/v1", "scheme"},
		{"the file scheme", "file:///etc/passwd", "scheme"},
		{"a scheme with no host", "http://", "no host"},
		{"an unparseable URL", "http://[::1", "does not parse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NormalizeAddress(tc.address); err == nil {
				t.Fatalf("%q was accepted", tc.address)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error does not say %q: %v", tc.want, err)
			}
			// NewClient refuses it too, which is the path a collect takes.
			if _, err := NewClient(tc.address, Settings{}); err == nil {
				t.Fatalf("NewClient accepted %q", tc.address)
			}
		})
	}
}

// TestAValidAddressIsAcceptedAndNormalized is the control for the refusals
// above, and it pins the one normalization: a trailing slash is removed so path
// concatenation cannot produce a double slash.
func TestAValidAddressIsAcceptedAndNormalized(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://localhost:3100", "http://localhost:3100"},
		{"https://loki.example.com", "https://loki.example.com"},
		{"http://localhost:3100/", "http://localhost:3100"},
		{"http://localhost:3100///", "http://localhost:3100"},
		{"  http://localhost:3100  ", "http://localhost:3100"},
		{"https://loki.example.com/prefix", "https://loki.example.com/prefix"},
		{"http://[::1]:3100", "http://[::1]:3100"},
	}
	for _, tc := range cases {
		got, err := NormalizeAddress(tc.in)
		if err != nil {
			t.Fatalf("NormalizeAddress(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeAddress(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestTheCredentialHeadersAreWrittenFromTheEnvironment covers each arm, and
// asserts NON-REVERSIBLE properties of the credential rather than its value.
func TestTheCredentialHeadersAreWrittenFromTheEnvironment(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		assert func(t *testing.T, req map[string]string)
	}{
		{
			"basic auth",
			map[string]string{EnvUsername: "u", EnvPassword: "p"},
			func(t *testing.T, req map[string]string) {
				got := req["header:Authorization"]
				if !strings.HasPrefix(got, "Basic ") {
					t.Fatalf("Authorization = %q, want a Basic credential", got)
				}
			},
		},
		{
			"a bearer token in the default header",
			map[string]string{EnvBearerToken: "t0ken"},
			func(t *testing.T, req map[string]string) {
				if got := req["header:Authorization"]; got != "Bearer t0ken" {
					t.Fatalf("Authorization = %q, want a Bearer credential", got)
				}
			},
		},
		{
			"a bearer token in a named header",
			map[string]string{EnvBearerToken: "t0ken", EnvAuthHeader: "X-Gateway-Auth"},
			func(t *testing.T, req map[string]string) {
				if got := req["header:X-Gateway-Auth"]; got != "Bearer t0ken" {
					t.Fatalf("X-Gateway-Auth = %q, want the bearer credential", got)
				}
				if _, ok := req["header:Authorization"]; ok {
					t.Fatal("the default header was also written")
				}
			},
		},
		{
			"basic auth wins over a bearer token when both are set",
			map[string]string{EnvUsername: "u", EnvPassword: "p", EnvBearerToken: "t0ken"},
			func(t *testing.T, req map[string]string) {
				if got := req["header:Authorization"]; !strings.HasPrefix(got, "Basic ") {
					t.Fatalf("Authorization = %q, want the Basic credential to win", got)
				}
			},
		},
		{
			"the tenant header",
			map[string]string{EnvOrgID: "tenant-1"},
			func(t *testing.T, req map[string]string) {
				if got := req["header:X-Scope-Orgid"]; got != "tenant-1" {
					t.Fatalf("X-Scope-OrgID = %q, want %q", got, "tenant-1")
				}
			},
		},
		{
			"the query tags header",
			map[string]string{EnvQueryTags: "team=platform"},
			func(t *testing.T, req map[string]string) {
				if got := req["header:X-Query-Tags"]; got != "team=platform" {
					t.Fatalf("X-Query-Tags = %q, want %q", got, "team=platform")
				}
			},
		},
		{
			"the no-cache header",
			map[string]string{EnvNoCache: "true"},
			func(t *testing.T, req map[string]string) {
				if got := req["header:Cache-Control"]; got != "no-cache" {
					t.Fatalf("Cache-Control = %q, want no-cache", got)
				}
			},
		},
		{
			"compression is negotiated only when it is asked for",
			map[string]string{EnvHTTPCompression: "true"},
			func(t *testing.T, req map[string]string) {
				if got := req["header:Accept-Encoding"]; got != "gzip" {
					t.Fatalf("Accept-Encoding = %q, want gzip", got)
				}
			},
		},
		{
			"an unauthenticated collect sends no credential at all",
			nil,
			func(t *testing.T, req map[string]string) {
				for _, header := range []string{"header:Authorization", "header:X-Scope-Orgid", "header:X-Query-Tags", "header:Cache-Control"} {
					if v, ok := req[header]; ok {
						t.Fatalf("%s was sent as %q with nothing configured", header, v)
					}
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeLoki(t, func(map[string]string) (any, int) {
				return successBody(map[string]string{"app": "c"}, windowEndAt.UnixNano(), 1), http.StatusOK
			})
			c, err := NewClient(f.URL, mustSettings(t, tc.env))
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if _, err := c.Walk(context.Background(), Query{}, windowStartAt, windowEndAt); err != nil {
				t.Fatalf("Walk: %v", err)
			}
			if len(f.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(f.requests))
			}
			tc.assert(t, f.requests[0])
		})
	}
}

// TestARetryableFailureIsRetriedAndA4xxIsNot. A retry can fix a transport
// failure or a 5xx; a 4xx is the server saying the request itself is wrong, and
// repeating it wastes an operator's collect.
func TestARetryableFailureIsRetriedAndA4xxIsNot(t *testing.T) {
	t.Run("a 5xx is retried and then succeeds", func(t *testing.T) {
		attempts := 0
		f := newFakeLoki(t, func(map[string]string) (any, int) {
			attempts++
			if attempts < 3 {
				return "temporarily unavailable", http.StatusServiceUnavailable
			}
			return successBody(map[string]string{"app": "c"}, windowEndAt.UnixNano(), 1), http.StatusOK
		})
		c, err := NewClient(f.URL, mustSettings(t, map[string]string{
			EnvClientRetries: "3", EnvClientMinBackoff: "1ms", EnvClientMaxBackoff: "5ms",
		}))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		got, err := c.Walk(context.Background(), Query{}, windowStartAt, windowEndAt)
		if err != nil {
			t.Fatalf("Walk: %v", err)
		}
		if attempts != 3 {
			t.Fatalf("attempts = %d, want 3", attempts)
		}
		if len(got.Entries) != 1 {
			t.Fatalf("entries = %d, want 1", len(got.Entries))
		}
	})

	t.Run("a 4xx is not retried", func(t *testing.T) {
		attempts := 0
		f := newFakeLoki(t, func(map[string]string) (any, int) {
			attempts++
			return "no such tenant", http.StatusNotFound
		})
		c, err := NewClient(f.URL, mustSettings(t, map[string]string{
			EnvClientRetries: "3", EnvClientMinBackoff: "1ms", EnvClientMaxBackoff: "5ms",
		}))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if _, err := c.Walk(context.Background(), Query{}, windowStartAt, windowEndAt); err == nil {
			t.Fatal("a 404 returned no error")
		}
		if attempts != 1 {
			t.Fatalf("attempts = %d, want 1; a 4xx is the server refusing the request, not a transient failure", attempts)
		}
	})

	t.Run("with retries at zero a 5xx fails on the first attempt", func(t *testing.T) {
		attempts := 0
		f := newFakeLoki(t, func(map[string]string) (any, int) {
			attempts++
			return "boom", http.StatusInternalServerError
		})
		c, err := NewClient(f.URL, mustSettings(t, nil))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if _, err := c.Walk(context.Background(), Query{}, windowStartAt, windowEndAt); err == nil {
			t.Fatal("no error")
		}
		if attempts != 1 {
			t.Fatalf("attempts = %d, want 1 with the default of no retries", attempts)
		}
	})
}

// TestTheRetryBackoffGrowsAndIsCapped covers the backoff arithmetic directly,
// so the retry test above can stay fast without leaving the growth unasserted.
func TestTheRetryBackoffGrowsAndIsCapped(t *testing.T) {
	s := Settings{ClientMinBackoff: 10 * time.Millisecond, ClientMaxBackoff: 40 * time.Millisecond}
	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{
		{0, 10 * time.Millisecond},
		{1, 20 * time.Millisecond},
		{2, 40 * time.Millisecond},
		{3, 40 * time.Millisecond},
		{9, 40 * time.Millisecond},
	} {
		start := time.Now()
		if err := sleepBackoff(context.Background(), s, tc.attempt); err != nil {
			t.Fatalf("sleepBackoff: %v", err)
		}
		// The assertion is a FLOOR, not a window: a loaded machine sleeps
		// longer, and asserting an upper bound would make this flake.
		if elapsed := time.Since(start); elapsed < tc.want {
			t.Fatalf("attempt %d slept %s, want at least %s", tc.attempt, elapsed, tc.want)
		}
	}
}

// TestTheBackoffReturnsEarlyOnACancelledContext. Without this a cancelled
// collect would sit in a sleep it can no longer be useful after.
func TestTheBackoffReturnsEarlyOnACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := sleepBackoff(ctx, Settings{ClientMinBackoff: 5 * time.Second, ClientMaxBackoff: 5 * time.Second}, 0)
	if err == nil {
		t.Fatal("the backoff returned no error under a cancelled context")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the backoff slept %s under a cancelled context", elapsed)
	}
}

// TestAMalformedProxyURLIsRefused covers the transport arm's own bad input.
func TestAMalformedProxyURLIsRefused(t *testing.T) {
	for _, value := range []string{"://nonsense", "not-a-url"} {
		_, err := NewClient("http://loki.example:3100", mustSettings(t, map[string]string{EnvHTTPProxyURL: value}))
		if err == nil {
			t.Fatalf("%s=%q was accepted", EnvHTTPProxyURL, value)
		}
		if !strings.Contains(err.Error(), EnvHTTPProxyURL) {
			t.Fatalf("the error does not name the variable: %v", err)
		}
	}
}

// TestTheProxyDecisionIsMadeOnceAtConstruction pins WHERE the choice is made,
// which is what keeps it meaningful: net/http's environment proxy function
// reads its six variables at the first request and caches the answer for the
// life of the process, so a proxy chosen later decides nothing.
func TestTheProxyDecisionIsMadeOnceAtConstruction(t *testing.T) {
	cases := []struct {
		name        string
		env         map[string]string
		wantProxy   bool
		wantFromEnv bool
	}{
		{"no proxy configured", nil, false, false},
		{"an explicit proxy URL", map[string]string{EnvHTTPProxyURL: "http://proxy:3128"}, true, false},
		{"the environment proxy", map[string]string{EnvEnvProxy: "true"}, true, true},
		{"an explicit URL wins over the environment flag",
			map[string]string{EnvHTTPProxyURL: "http://proxy:3128", EnvEnvProxy: "true"}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport, err := buildTransport(mustSettings(t, tc.env))
			if err != nil {
				t.Fatalf("buildTransport: %v", err)
			}
			if (transport.Proxy != nil) != tc.wantProxy {
				t.Fatalf("a proxy function was %spresent, want the opposite", map[bool]string{true: "", false: "not "}[transport.Proxy != nil])
			}
			if !tc.wantProxy {
				return
			}
			// An explicit URL returns that URL for every request; the
			// environment function is the standard library's own and returns
			// whatever the process environment says, which here is nothing.
			req, _ := http.NewRequest(http.MethodGet, "http://loki.example:3100/x", nil)
			got, err := transport.Proxy(req)
			if err != nil {
				t.Fatalf("the proxy function errored: %v", err)
			}
			if tc.wantFromEnv {
				return // its answer depends on the process environment, which this test does not set.
			}
			if got == nil || got.Host != "proxy:3128" {
				t.Fatalf("the proxy function returned %v, want the configured proxy", got)
			}
		})
	}
}

// TestCompressionDecidesTheTransportSetting pins the flag onto the transport,
// so "the header was sent" and "the transport was configured" are separate
// observations.
func TestCompressionDecidesTheTransportSetting(t *testing.T) {
	off, err := buildTransport(mustSettings(t, nil))
	if err != nil {
		t.Fatalf("buildTransport: %v", err)
	}
	if !off.DisableCompression {
		t.Fatal("compression is negotiated by the transport with the flag unset")
	}
	on, err := buildTransport(mustSettings(t, map[string]string{EnvHTTPCompression: "true"}))
	if err != nil {
		t.Fatalf("buildTransport: %v", err)
	}
	if on.DisableCompression {
		t.Fatal("compression is disabled with the flag set")
	}
}

// TestTLSSkipVerifyReachesTheTransport is the one assertion this suite makes
// about the TLS configuration: that the operator's own explicit choice is what
// sets it. Whether a handshake then succeeds is the standard library's, and
// asserting it would need a private certificate authority to dial.
func TestTLSSkipVerifyReachesTheTransport(t *testing.T) {
	off, err := buildTransport(mustSettings(t, nil))
	if err != nil {
		t.Fatalf("buildTransport: %v", err)
	}
	if off.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("certificate verification is skipped with the flag unset")
	}
	on, err := buildTransport(mustSettings(t, map[string]string{EnvTLSSkipVerify: "true"}))
	if err != nil {
		t.Fatalf("buildTransport: %v", err)
	}
	if !on.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("the operator's explicit skip-verify did not reach the transport")
	}
	if off.TLSClientConfig.MinVersion != 0x0303 {
		t.Fatalf("the minimum TLS version is %#x, want TLS 1.2", off.TLSClientConfig.MinVersion)
	}
}

// TestTheClientHasAnExplicitTimeout. http.DefaultClient has none and would hang
// on a Loki that accepts a connection and never answers.
func TestTheClientHasAnExplicitTimeout(t *testing.T) {
	c, err := NewClient("http://loki.example:3100", mustSettings(t, nil))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.http.Timeout == 0 {
		t.Fatal("the client has no timeout")
	}
	if c.http == http.DefaultClient {
		t.Fatal("the client is http.DefaultClient")
	}
	if c.http.Transport == nil {
		t.Fatal("the client uses the default transport rather than the one built from the environment")
	}
}
