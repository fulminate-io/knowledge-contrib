// SPDX-License-Identifier: Apache-2.0

package bbclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
)

// client_test.go — the pagination, the retry budget, the Retry-After parse, the
// cancellation arm and the timeout, driven against a REAL HTTP server.
//
// BOTH SIDES OF THE SEAM ARE REAL. This collector has no provider SDK: its seam
// is a base URL and an *http.Client, so a double on the far side of it would be
// a double of net/http rather than of the provider. A test server IS the
// Bitbucket API here.

// serve stands a recorded API up and returns a client pointed at it.
func serve(t *testing.T, handler http.HandlerFunc) *bbclient.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return bbclient.NewAt(server.URL, server.Client(), "user", "app-password")
}

// collectPages drains a paginated endpoint into one slice of raw pages.
func collectPages(t *testing.T, client *bbclient.Client, path string) []string {
	t.Helper()
	var pages []string
	err := client.GetPaginated(context.Background(), path, func(raw json.RawMessage) error {
		pages = append(pages, string(raw))
		return nil
	})
	if err != nil {
		t.Fatalf("paginating %q: %v", path, err)
	}
	return pages
}

// TestPagelenIsAppendedWhenTheCallerNamesNone.
func TestPagelenIsAppendedWhenTheCallerNamesNone(t *testing.T) {
	var seen string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RawQuery
		fmt.Fprint(w, `{"values": [1], "next": ""}`)
	})

	collectPages(t, client, "repositories/acme")
	if seen != "pagelen=100" {
		t.Errorf("the request carried the query %q, want %q", seen, "pagelen=100")
	}
}

// TestAnExplicitPagelenIsLeftAlone, which is what lets the capped enumeration
// ask for fewer than the provider's maximum.
func TestAnExplicitPagelenIsLeftAlone(t *testing.T) {
	var seen string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RawQuery
		fmt.Fprint(w, `{"values": [1], "next": ""}`)
	})

	collectPages(t, client, "repositories/acme/api/pipelines?sort=-created_on&pagelen=7")
	if strings.Contains(seen, "pagelen=100") {
		t.Errorf("an explicit pagelen was overridden: %q", seen)
	}
	if !strings.Contains(seen, "pagelen=7") {
		t.Errorf("the caller's own pagelen was lost: %q", seen)
	}
}

// TestTheNextURLIsFollowedToExhaustionAndVerbatim.
//
// THE SECOND AND THIRD PAGES ARE ABSOLUTE URLS, which is the shape the provider
// really answers with, and they are followed VERBATIM rather than re-joined to
// the base — including the query the provider chose to put on them.
func TestTheNextURLIsFollowedToExhaustionAndVerbatim(t *testing.T) {
	var base string
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.RequestURI())
		switch r.URL.Query().Get("cursor") {
		case "":
			fmt.Fprintf(w, `{"values": ["a"], "next": %q}`, base+"/things?cursor=two")
		case "two":
			fmt.Fprintf(w, `{"values": ["b"], "next": %q}`, base+"/things?cursor=three")
		default:
			fmt.Fprint(w, `{"values": ["c"], "next": ""}`)
		}
	}))
	t.Cleanup(server.Close)
	base = server.URL
	client := bbclient.NewAt(server.URL, server.Client(), "user", "app-password")

	pages := collectPages(t, client, "things")
	if len(pages) != 3 {
		t.Fatalf("the walk read %d pages, want 3: %v", len(pages), pages)
	}
	if pages[0] != `["a"]` || pages[2] != `["c"]` {
		t.Errorf("the pages arrived as %v", pages)
	}
	// THE FOLLOWED URLS ARE THE PROVIDER'S OWN, unmodified: no second pagelen is
	// appended to a URL the provider built.
	for _, uri := range requested[1:] {
		if strings.Contains(uri, "pagelen") {
			t.Errorf("a provider-supplied next URL was rewritten: %q", uri)
		}
	}
}

// TestASinglePageEndsTheWalk. The known positive for the row above: a `next` of
// the empty string stops, so the three-page walk is following a value rather
// than looping a fixed number of times.
func TestASinglePageEndsTheWalk(t *testing.T) {
	var requests atomic.Int64
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"values": ["only"], "next": ""}`)
	})

	if pages := collectPages(t, client, "things"); len(pages) != 1 {
		t.Errorf("a single-page endpoint produced %d pages", len(pages))
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("a single-page endpoint was fetched %d times, want 1", got)
	}
}

// TestAPersistentRateLimitIsFetchedFourTimes is the retry budget, and FOUR is
// the number rather than three.
//
// THE LOOP RUNS THREE ATTEMPTS AND IS FOLLOWED BY ONE UNCONDITIONAL FINAL
// REQUEST. That is the source provider's shape, so a test asserting three
// attempts would be asserting a budget this collector does not have — and the
// provider would see one more request per rate-limited URL than the test claimed.
func TestAPersistentRateLimitIsFetchedFourTimes(t *testing.T) {
	var requests atomic.Int64
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"slow down"}`)
	})

	err := client.GetPaginated(context.Background(), "things",
		func(json.RawMessage) error { return nil })
	if err == nil {
		t.Fatal("a persistently rate-limited URL produced no error")
	}
	var apiErr *bbclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("the failure is not an APIError: %v", err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("the final answer's status is %d, want 429", apiErr.StatusCode)
	}
	if got := requests.Load(); got != 4 {
		t.Errorf("the URL was fetched %d times, want 4 — %d retries and one unconditional final "+
			"request", got, bbclient.MaxRetries)
	}
}

// TestARateLimitThatClearsReturnsTheAnswer. The other arm: the retry exists to
// succeed, and a 429 followed by a 200 returns the 200 and stops retrying.
func TestARateLimitThatClearsReturnsTheAnswer(t *testing.T) {
	var requests atomic.Int64
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"values": ["answered"], "next": ""}`)
	})

	pages := collectPages(t, client, "things")
	if len(pages) != 1 || pages[0] != `["answered"]` {
		t.Errorf("the cleared rate limit produced %v", pages)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("the URL was fetched %d times, want 2 — one refused and one answered", got)
	}
}

// TestACancelledContextDoesNotWaitOutTheRetry. The sleep between attempts is a
// select on the context, so a cancelled walk returns at once rather than
// spending the whole backoff on a result it will discard.
func TestACancelledContextDoesNotWaitOutTheRetry(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		// A LONG Retry-After. If the sleep were unconditional the walk would sit
		// here for a minute; the bound below is what turns that into a failure
		// rather than a hang.
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.GetPaginated(ctx, "things", func(json.RawMessage) error { return nil })
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the cancelled retry returned %v, want the context error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled walk waited out a 60-second Retry-After")
	}
}

// TestACancelledWalkReturnsTheCancellationAndNotAFailedRequest is the per-page
// context check's own observation, and finding the RIGHT observable took a
// measurement.
//
// THE OBVIOUS ONE DOES NOT WORK, and saying so is the point. Counting requests
// passes with the check DELETED: net/http declines a request built on a
// cancelled context before it reaches the wire, so the server counts zero either
// way and the count is the transport's doing rather than the guard's.
//
// WHAT THE GUARD ALONE PRODUCES IS THE ERROR SHAPE. With it, a cancelled walk
// returns the context error plainly. Without it, the same cancellation comes
// back wrapped as a failed HTTP GET naming a URL — a transport failure in an
// operator's diagnostics for something that was not a transport failure, and one
// that carries a request URL into a log line for a request never made.
func TestACancelledWalkReturnsTheCancellationAndNotAFailedRequest(t *testing.T) {
	var requests atomic.Int64
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"values": ["page"], "next": ""}`)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := client.GetPaginated(ctx, "things", func(json.RawMessage) error {
		t.Error("a page handler ran on a cancelled walk")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the walk returned %v, want the context error", err)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("a walk cancelled before it started made %d request(s), want 0", got)
	}
	// THE SHAPE, which is what the check produces and the transport does not.
	if strings.Contains(err.Error(), "http GET") {
		t.Errorf("the cancellation came back as a failed HTTP GET: %v. The check at the top of "+
			"the page loop is what returns it plainly; without it a cancelled walk reports a "+
			"transport failure and carries a request URL for a request that was never sent", err)
	}
	if err.Error() != context.Canceled.Error() {
		t.Errorf("the cancelled walk returned %q, want the bare context error %q",
			err.Error(), context.Canceled.Error())
	}

	// THE SAME-RUN KNOWN POSITIVE: the identical client and server answer an
	// uncancelled walk, so the zero above is a guard and not a broken fixture.
	if pages := collectPages(t, client, "things"); len(pages) != 1 {
		t.Fatalf("the same client read %d pages on an uncancelled walk", len(pages))
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("the uncancelled walk made %d requests, want 1", got)
	}
}

// TestTheContextIsCheckedBetweenPages. A cancelled walk stops following `next`
// rather than draining an enumeration whose result is discarded.
func TestTheContextIsCheckedBetweenPages(t *testing.T) {
	var base string
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprintf(w, `{"values": ["page"], "next": %q}`, base+"/things?cursor=next")
	}))
	t.Cleanup(server.Close)
	base = server.URL
	client := bbclient.NewAt(server.URL, server.Client(), "user", "app-password")

	ctx, cancel := context.WithCancel(context.Background())
	err := client.GetPaginated(ctx, "things", func(json.RawMessage) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the walk returned %v, want the context error", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("the cancelled walk made %d requests, want 1 — a `next` that never ends would "+
			"otherwise page forever", got)
	}
}

// TestParseRetryAfterCoversEveryArm.
func TestParseRetryAfterCoversEveryArm(t *testing.T) {
	for _, arm := range []struct {
		header string
		want   time.Duration
		why    string
	}{
		{"", 2 * time.Second, "an absent header takes the default"},
		{"not-a-number", 2 * time.Second, "an unparseable header takes the default"},
		{"0", 1 * time.Second, "a zero is FLOORED rather than taken literally, or the retry " +
			"becomes a hot loop against a provider that just asked for less traffic"},
		{"-5", 1 * time.Second, "a negative is floored on the same terms"},
		{"7", 7 * time.Second, "a positive integer is honored"},
	} {
		if got := bbclient.ParseRetryAfter(arm.header); got != arm.want {
			t.Errorf("Retry-After %q parsed to %s, want %s — %s",
				arm.header, got, arm.want, arm.why)
		}
	}
}

// TestTheCredentialRidesBasicAuthAndNeverTheURL. The client logs the request URL
// on a rate limit, so a credential built into the URL would land in an
// operator's log.
func TestTheCredentialRidesBasicAuthAndNeverTheURL(t *testing.T) {
	var authorization, uri string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		uri = r.URL.RequestURI()
		fmt.Fprint(w, `{"values": [], "next": ""}`)
	})

	collectPages(t, client, "things")
	user, password, ok := (&http.Request{Header: http.Header{"Authorization": {authorization}}}).BasicAuth()
	if !ok {
		t.Fatalf("the request carried no HTTP Basic credential: %q", authorization)
	}
	if user != "user" || password != "app-password" {
		t.Errorf("the credential arrived as %q/%q", user, password)
	}
	if strings.Contains(uri, "app-password") || strings.Contains(uri, "@") {
		t.Errorf("the request URI carries the credential: %q", uri)
	}
}

// TestANonSuccessStatusBecomesAnAPIErrorCarryingIt, which is what every
// classification downstream is made on.
func TestANonSuccessStatusBecomesAnAPIErrorCarryingIt(t *testing.T) {
	for _, status := range []int{
		http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusInternalServerError,
	} {
		client := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"error":{"message":"status %d"}}`, status)
		})
		err := client.GetPaginated(context.Background(), "things",
			func(json.RawMessage) error { return nil })

		var apiErr *bbclient.APIError
		if !errors.As(err, &apiErr) {
			t.Errorf("a %d produced %v, which is not an APIError", status, err)
			continue
		}
		if apiErr.StatusCode != status {
			t.Errorf("the APIError carries %d, want %d", apiErr.StatusCode, status)
		}
		if !strings.Contains(apiErr.Body, fmt.Sprintf("status %d", status)) {
			t.Errorf("the APIError carries the body %q, which is not the provider's", apiErr.Body)
		}
	}
}

// TestAnErrorBodyIsBounded. An error body is a diagnostic rather than data, and
// a provider answering an unbounded one on a failure would otherwise be read
// into memory in full and carried into a log line.
func TestAnErrorBodyIsBounded(t *testing.T) {
	const oversized = 40000
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, strings.Repeat("x", oversized))
	})

	err := client.GetPaginated(context.Background(), "things",
		func(json.RawMessage) error { return nil })
	var apiErr *bbclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("the failure is not an APIError: %v", err)
	}
	if len(apiErr.Body) >= oversized {
		t.Errorf("the error carries %d bytes of body; it is bounded", len(apiErr.Body))
	}
	if len(apiErr.Body) == 0 {
		t.Error("the error carries no body at all, so the bound above is not measuring a limit")
	}
}

// TestGetRawReturnsTheWholeBodyAndClassifiesItsFailures. The pipeline definition
// is not a pagination envelope, so it is read through the other entry point —
// which needs its own arms for the same reason.
func TestGetRawReturnsTheWholeBodyAndClassifiesItsFailures(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "pipelines:\n  default: []\n")
	})
	body, err := client.GetRaw(context.Background(), "repositories/acme/api/src/main/file.yml")
	if err != nil {
		t.Fatalf("reading a raw endpoint: %v", err)
	}
	if !strings.HasPrefix(string(body), "pipelines:") {
		t.Errorf("the raw read returned %q", body)
	}

	missing := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"not found"}`)
	})
	_, err = missing.GetRaw(context.Background(), "repositories/acme/api/src/main/file.yml")
	var apiErr *bbclient.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("a 404 on a raw read produced %v, want an APIError carrying 404", err)
	}
}
