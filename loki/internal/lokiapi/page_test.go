// SPDX-License-Identifier: Apache-2.0

package lokiapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// page_test.go — the backward time-narrowing walk, its two completeness arms
// and its error arms, against a fake Loki.
//
// THE FAKE IS A REAL HTTP SERVER speaking the real response shape over the real
// client, so what is under test is the module's own transport and paging rather
// than a stubbed-out call.

// fakeLoki serves query_range from a handler the test supplies.
type fakeLoki struct {
	*httptest.Server
	requests []map[string]string
}

func newFakeLoki(t *testing.T, handle func(q map[string]string) (any, int)) *fakeLoki {
	t.Helper()
	f := &fakeLoki{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := map[string]string{}
		for k, v := range r.URL.Query() {
			q[k] = v[0]
		}
		for k, v := range r.Header {
			q["header:"+k] = v[0]
		}
		f.requests = append(f.requests, q)
		body, status := handle(q)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if raw, ok := body.(string); ok {
			_, _ = w.Write([]byte(raw))
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(f.Close)
	return f
}

// successBody builds one query_range response holding the supplied entries at
// consecutive nanoseconds ending at endNanos.
func successBody(labels map[string]string, endNanos int64, count int) map[string]any {
	values := make([][]string, 0, count)
	for i := range count {
		values = append(values, []string{
			strconv.FormatInt(endNanos-int64(i), 10),
			fmt.Sprintf("worker restarted attempt %d", i%3),
		})
	}
	return map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "streams",
			"result":     []map[string]any{{"stream": labels, "values": values}},
		},
	}
}

func testClient(t *testing.T, address string) *Client {
	t.Helper()
	c, err := NewClient(address, mustSettings(t, nil))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func mustSettings(t *testing.T, env map[string]string) Settings {
	t.Helper()
	s, err := LoadSettings(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	return s
}

// mapLookup reads an environment a test supplies, WITHOUT touching the
// process's own. These tests run alongside everything else in the package, and
// os.Setenv is a process-global mutation that a parallel test would observe.
func mapLookup(env map[string]string) LookupFunc {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

var (
	windowStartAt = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	windowEndAt   = time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)
)

// TestASingleShortPageCompletesTheWalk is the ordinary case.
func TestASingleShortPageCompletesTheWalk(t *testing.T) {
	f := newFakeLoki(t, func(map[string]string) (any, int) {
		return successBody(map[string]string{"app": "checkout"}, windowEndAt.UnixNano(), 3), http.StatusOK
	})
	got, err := testClient(t, f.URL).Walk(context.Background(), Query{}, windowStartAt, windowEndAt)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(got.Entries))
	}
	if !got.Complete {
		t.Fatalf("the walk asserted incomplete: %s", got.Incomplete)
	}
	if len(f.requests) != 1 {
		t.Fatalf("requests = %d, want 1; a short page ends the walk", len(f.requests))
	}
	req := f.requests[0]
	if req["direction"] != "backward" {
		t.Fatalf("direction = %q, want backward", req["direction"])
	}
	if req["limit"] != strconv.Itoa(pageLimit) {
		t.Fatalf("limit = %q, want %d", req["limit"], pageLimit)
	}
	if req["start"] != strconv.FormatInt(windowStartAt.UnixNano(), 10) {
		t.Fatalf("start = %q, want the window start in nanoseconds", req["start"])
	}
}

// TestTheWalkNarrowsTheWindowBackwards covers the multi-page walk and the
// narrowing arithmetic: each request's end is ONE NANOSECOND before the oldest
// entry of the previous raw page.
func TestTheWalkNarrowsTheWindowBackwards(t *testing.T) {
	page := 0
	oldestOfFirst := windowEndAt.UnixNano() - int64(pageLimit) + 1
	f := newFakeLoki(t, func(map[string]string) (any, int) {
		page++
		if page == 1 {
			return successBody(map[string]string{"app": "checkout"}, windowEndAt.UnixNano(), pageLimit), http.StatusOK
		}
		return successBody(map[string]string{"app": "checkout"}, oldestOfFirst-1, 2), http.StatusOK
	})

	got, err := testClient(t, f.URL).Walk(context.Background(), Query{}, windowStartAt, windowEndAt)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !got.Complete {
		t.Fatalf("the walk asserted incomplete: %s", got.Incomplete)
	}
	if len(got.Entries) != pageLimit+2 {
		t.Fatalf("entries = %d, want %d", len(got.Entries), pageLimit+2)
	}
	if len(f.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(f.requests))
	}
	wantEnd := strconv.FormatInt(oldestOfFirst-1, 10)
	if f.requests[1]["end"] != wantEnd {
		t.Fatalf("the second request's end = %q, want %q (one nanosecond before the first page's oldest)",
			f.requests[1]["end"], wantEnd)
	}
}

// TestTerminationIsOnTheRawPageCountNeverTheFilteredOne is the truncation cell.
// The severity filter drops the whole first page to nothing, and the walk must
// still page on, because Loki counted 5000 entries in that window whatever this
// side kept.
func TestTerminationIsOnTheRawPageCountNeverTheFilteredOne(t *testing.T) {
	page := 0
	oldestOfFirst := windowEndAt.UnixNano() - int64(pageLimit) + 1
	f := newFakeLoki(t, func(map[string]string) (any, int) {
		page++
		if page == 1 {
			// A full page of INFO entries, every one of which the filter drops.
			return successBody(map[string]string{"app": "checkout", "level": "info"}, windowEndAt.UnixNano(), pageLimit), http.StatusOK
		}
		// A short page of ERROR entries, which the filter keeps.
		return successBody(map[string]string{"app": "checkout", "level": "error"}, oldestOfFirst-1, 4), http.StatusOK
	})

	q := Query{SeverityMin: "ERROR"}
	got, err := testClient(t, f.URL).Walk(context.Background(), q, windowStartAt, windowEndAt)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(f.requests) != 2 {
		t.Fatalf("requests = %d, want 2; the walk terminated on the FILTERED count of the first page", len(f.requests))
	}
	if len(got.Entries) != 4 {
		t.Fatalf("entries = %d, want 4 (the second page only)", len(got.Entries))
	}
	if !got.Complete {
		t.Fatalf("the walk asserted incomplete: %s", got.Incomplete)
	}
}

// TestAWindowThatCannotNarrowAssertsIncomplete is the completeness FALSE arm,
// and it is the one place this collector deliberately diverges from the
// built-in adapter, which returns success here.
//
// The condition: a full raw page whose oldest entry is at or after the current
// end bound, so `end = oldest - 1` cannot move the cursor. Loki's query_range
// has no cursor, so the remainder of the window is unreachable.
func TestAWindowThatCannotNarrowAssertsIncomplete(t *testing.T) {
	// Every entry at ONE instant, at the window's end.
	f := newFakeLoki(t, func(map[string]string) (any, int) {
		values := make([][]string, 0, pageLimit)
		for range pageLimit {
			values = append(values, []string{strconv.FormatInt(windowEndAt.UnixNano(), 10), "same instant"})
		}
		return map[string]any{
			"status": "success",
			"data": map[string]any{"resultType": "streams", "result": []map[string]any{
				{"stream": map[string]string{"app": "checkout"}, "values": values},
			}},
		}, http.StatusOK
	})

	got, err := testClient(t, f.URL).Walk(context.Background(), Query{}, windowStartAt, windowEndAt)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if got.Complete {
		t.Fatal("the walk asserted COMPLETE over a window it could not finish reading")
	}
	if got.Incomplete == "" {
		t.Fatal("an incomplete walk carries no reason")
	}
	for _, want := range []string{"all carry the timestamp", strconv.Itoa(pageLimit)} {
		if !strings.Contains(got.Incomplete, want) {
			t.Fatalf("the reason does not name %q: %q", want, got.Incomplete)
		}
	}
	if len(got.Entries) != pageLimit {
		t.Fatalf("entries = %d, want the page it did read (%d)", len(got.Entries), pageLimit)
	}
	if len(f.requests) != 1 {
		t.Fatalf("requests = %d, want 1; the walk must stop rather than loop", len(f.requests))
	}
}

// TestTheCompleteArmIsReachableInTheSameShape is the control for the row above:
// the same page count, one nanosecond apart instead of at one instant, walks on
// and completes. Without it, "incomplete" would not be distinguishable from
// "this fixture always says incomplete".
func TestTheCompleteArmIsReachableInTheSameShape(t *testing.T) {
	page := 0
	f := newFakeLoki(t, func(map[string]string) (any, int) {
		page++
		if page == 1 {
			return successBody(map[string]string{"app": "checkout"}, windowEndAt.UnixNano(), pageLimit), http.StatusOK
		}
		return successBody(map[string]string{"app": "checkout"}, windowStartAt.UnixNano()+10, 1), http.StatusOK
	})
	got, err := testClient(t, f.URL).Walk(context.Background(), Query{}, windowStartAt, windowEndAt)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !got.Complete {
		t.Fatalf("the walk asserted incomplete: %s", got.Incomplete)
	}
}

// TestAPageFailureFailsTheWalkRatherThanTruncatingIt. The built-in adapter
// returns success after a mid-walk failure when earlier pages produced entries,
// which reports a complete walk over a truncated window and lets the deletion
// phase treat every unreached entry as gone. This collector refuses.
func TestAPageFailureFailsTheWalkRatherThanTruncatingIt(t *testing.T) {
	page := 0
	f := newFakeLoki(t, func(map[string]string) (any, int) {
		page++
		if page == 1 {
			return successBody(map[string]string{"app": "checkout"}, windowEndAt.UnixNano(), pageLimit), http.StatusOK
		}
		return "upstream exploded", http.StatusInternalServerError
	})
	_, err := testClient(t, f.URL).Walk(context.Background(), Query{}, windowStartAt, windowEndAt)
	if err == nil {
		t.Fatal("a failure on the second page returned success with the first page's entries")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("the error does not name the status: %v", err)
	}
}

// TestErrorArms covers each refusal the walk owes, one case each.
func TestErrorArms(t *testing.T) {
	cases := []struct {
		name   string
		body   any
		status int
		want   string
	}{
		{"a 4xx names the status", "no such tenant", http.StatusNotFound, "404"},
		{"a 5xx names the status", "boom", http.StatusInternalServerError, "500"},
		{"a body that is not JSON says so", "<html>not json</html>", http.StatusOK, "not the expected JSON"},
		{"a JSON body that is not the envelope says so", map[string]any{"status": "error"}, http.StatusOK, `status "error"`},
		{"a value that is not a timestamp-line pair says so", map[string]any{
			"status": "success",
			"data": map[string]any{"result": []map[string]any{
				{"stream": map[string]string{"app": "c"}, "values": [][]string{{"only-one-field"}}},
			}},
		}, http.StatusOK, "[timestamp, line] pair"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeLoki(t, func(map[string]string) (any, int) { return tc.body, tc.status })
			_, err := testClient(t, f.URL).Walk(context.Background(), Query{}, windowStartAt, windowEndAt)
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error does not name %q: %v", tc.want, err)
			}
		})
	}
}

// TestAnUnreachableEndpointErrors covers the transport arm.
func TestAnUnreachableEndpointErrors(t *testing.T) {
	f := newFakeLoki(t, func(map[string]string) (any, int) { return "", http.StatusOK })
	url := f.URL
	f.Close() // The listener is gone; the address is not.
	_, err := testClient(t, url).Walk(context.Background(), Query{}, windowStartAt, windowEndAt)
	if err == nil {
		t.Fatal("a walk against a closed listener returned no error")
	}
	if !strings.Contains(err.Error(), url) {
		t.Fatalf("the error does not name the endpoint: %v", err)
	}
}

// TestACancelledContextStopsTheWalkAndIsNotRetried. A cancelled context is the
// caller's decision, so it must reach the caller as an error rather than be
// retried as a transient failure.
func TestACancelledContextStopsTheWalkAndIsNotRetried(t *testing.T) {
	requests := 0
	f := newFakeLoki(t, func(map[string]string) (any, int) {
		requests++
		return successBody(map[string]string{"app": "c"}, windowEndAt.UnixNano(), pageLimit), http.StatusOK
	})

	c, err := NewClient(f.URL, mustSettings(t, map[string]string{EnvClientRetries: "3", EnvClientMinBackoff: "1ms"}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Walk(ctx, Query{}, windowStartAt, windowEndAt); err == nil {
		t.Fatal("a walk under a cancelled context returned no error")
	}
	if requests > 1 {
		t.Fatalf("the cancelled request was retried %d times", requests)
	}
}

// TestAnInvertedWindowIsRefusedBeforeAnyRequest is the bad-input arm on the
// window, and its assertion is that NOTHING WAS SENT.
func TestAnInvertedWindowIsRefusedBeforeAnyRequest(t *testing.T) {
	f := newFakeLoki(t, func(map[string]string) (any, int) {
		return successBody(nil, windowEndAt.UnixNano(), 1), http.StatusOK
	})
	c := testClient(t, f.URL)
	for _, tc := range []struct {
		name       string
		start, end time.Time
	}{
		{"end before start", windowEndAt, windowStartAt},
		{"an instantaneous window", windowStartAt, windowStartAt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Walk(context.Background(), Query{}, tc.start, tc.end); err == nil {
				t.Fatal("the window was accepted")
			}
		})
	}
	if len(f.requests) != 0 {
		t.Fatalf("%d requests were sent for a window that was refused", len(f.requests))
	}
}

// TestTheTextFilterIsBothSentAndApplied covers the double application: Loki's
// line filter is case-sensitive, so the case-insensitive match this collector
// promises is applied on this side as well as sent.
func TestTheTextFilterIsBothSentAndApplied(t *testing.T) {
	f := newFakeLoki(t, func(map[string]string) (any, int) {
		return map[string]any{
			"status": "success",
			"data": map[string]any{"result": []map[string]any{{
				"stream": map[string]string{"app": "c"},
				"values": [][]string{
					{strconv.FormatInt(windowEndAt.UnixNano(), 10), "upstream TIMEOUT reached"},
					{strconv.FormatInt(windowEndAt.UnixNano()-1, 10), "everything is fine"},
				},
			}}},
		}, http.StatusOK
	})
	got, err := testClient(t, f.URL).Walk(context.Background(), Query{TextFilter: "timeout"}, windowStartAt, windowEndAt)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, want 1; the filter is applied on this side, case-insensitively", len(got.Entries))
	}
	if !strings.Contains(f.requests[0]["query"], `|= "timeout"`) {
		t.Fatalf("the filter was not sent to Loki: query = %q", f.requests[0]["query"])
	}
}

// TestEntriesComeBackNewestFirst pins the per-page ordering.
func TestEntriesComeBackNewestFirst(t *testing.T) {
	f := newFakeLoki(t, func(map[string]string) (any, int) {
		return map[string]any{
			"status": "success",
			"data": map[string]any{"result": []map[string]any{{
				"stream": map[string]string{"app": "c"},
				"values": [][]string{
					{strconv.FormatInt(windowStartAt.UnixNano(), 10), "older"},
					{strconv.FormatInt(windowEndAt.UnixNano(), 10), "newer"},
				},
			}}},
		}, http.StatusOK
	})
	got, err := testClient(t, f.URL).Walk(context.Background(), Query{}, windowStartAt, windowEndAt)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(got.Entries) != 2 || got.Entries[0].Message != "newer" {
		t.Fatalf("entries came back %v, want the newest first", got.Entries)
	}
}
