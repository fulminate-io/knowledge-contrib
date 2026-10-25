// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/walk"
)

func limited(policy string, headers map[string]string) answer {
	return answer{status: http.StatusTooManyRequests, headers: headers, body: rateLimited(policy)}
}

var secondly = map[string]string{"X-HubSpot-RateLimit-Interval-Milliseconds": "10000", "X-HubSpot-RateLimit-Max": "190", "X-HubSpot-RateLimit-Remaining": "0"}

// TestARateLimitIsRetriedAfterTheWaitHubSpotNames: a secondly 429 waits the
// interval header, a search 429 waits one second; each retries once and the
// walk completes.
func TestARateLimitIsRetriedAfterTheWaitHubSpotNames(t *testing.T) {
	for _, c := range []struct {
		name  string
		key   string
		first answer
		want  []time.Duration
	}{
		{name: "secondly", key: readKey("deals", "301"), first: limited("TEN_SECONDLY_ROLLING", secondly), want: []time.Duration{10 * time.Second}},
		{name: "search", key: searchKey(""), first: limited("TEN_SECONDLY_ROLLING", nil), want: []time.Duration{time.Second}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, withRoute(c.key, c.first, corpus()[c.key][0]))
			res := f.mustWalk(t, fullParams(t))
			if !slices.Equal(f.waits, c.want) {
				t.Errorf("recorded waits %v, want %v", f.waits, c.want)
			}
			if n := f.count(c.key); n != 2 {
				t.Errorf("the limited read was requested %d times, want 2", n)
			}
			if !res.Complete.IsComplete() {
				t.Errorf("a walk whose rate limit cleared reported incomplete: %s", res.Complete.Reason())
			}
		})
	}
}

// TestRateLimitOutcomesThatEndARead: each of these ends the read without a
// guessed wait, marks the walk incomplete naming why, and keeps the rest.
func TestRateLimitOutcomesThatEndARead(t *testing.T) {
	k := readKey("deals", "301")
	for _, c := range []struct {
		name      string
		answer    answer
		reason    string
		wantWaits []time.Duration
		wantCount int
	}{
		{name: "DAILY", answer: limited("DAILY", secondly), reason: "daily request limit", wantCount: 1},
		{name: "secondly without the interval header", answer: limited("TEN_SECONDLY_ROLLING", nil),
			reason: "X-HubSpot-RateLimit-Interval-Milliseconds", wantCount: 1},
		{name: "unknown policy", answer: limited("HOURLY", secondly), reason: `policyName "HOURLY"`, wantCount: 1},
		{name: "persistent secondly", answer: limited("TEN_SECONDLY_ROLLING", secondly), reason: "after 3 attempts",
			wantWaits: []time.Duration{10 * time.Second, 10 * time.Second}, wantCount: 3},
		{name: "interval beyond the deadline", answer: limited("TEN_SECONDLY_ROLLING",
			map[string]string{"X-HubSpot-RateLimit-Interval-Milliseconds": "60000"}), reason: "1m0s exceeds", wantCount: 1},
		{name: "migration", answer: answer{status: 477, headers: map[string]string{"Retry-After": "86400"},
			body: `{"status": "error", "message": "Migration in progress"}`}, reason: "24h0m0s exceeds", wantCount: 1},
		{name: "migration with no Retry-After", answer: answer{status: 477, body: `{"status": "error", "message": "Migration in progress"}`},
			reason: "being migrated (477)", wantCount: 1},
		{name: "negative interval", answer: limited("TEN_SECONDLY_ROLLING",
			map[string]string{"X-HubSpot-RateLimit-Interval-Milliseconds": "-5"}), reason: "is negative", wantCount: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, withRoute(k, c.answer))
			res := f.mustWalk(t, fullParams(t))
			if res.Complete.IsComplete() || !strings.Contains(res.Complete.Reason(), c.reason) || !strings.Contains(res.Complete.Reason(), "/deals/batch/read") {
				t.Fatalf("got complete=%v reason %q, want incomplete naming %q and the path", res.Complete.IsComplete(), res.Complete.Reason(), c.reason)
			}
			if !slices.Equal(f.waits, c.wantWaits) {
				t.Errorf("recorded waits %v, want %v", f.waits, c.wantWaits)
			}
			if n := f.count(k); n != c.wantCount {
				t.Errorf("the limited read was requested %d times, want %d", n, c.wantCount)
			}
			if _, kept := nodesOfType(res, "hubspot-company")["hubspot:companies:101"]; !kept {
				t.Errorf("the earlier reads were not kept")
			}
		})
	}
}

// TestAnExhaustedWindowWaitsBeforeTheNextRequest: a 2xx whose
// X-HubSpot-RateLimit-Remaining is 0 makes the next request wait the interval.
func TestAnExhaustedWindowWaitsBeforeTheNextRequest(t *testing.T) {
	k := readKey("companies", "101")
	exhausted := corpus()[k][0]
	exhausted.headers = map[string]string{"X-HubSpot-RateLimit-Interval-Milliseconds": "10000", "X-HubSpot-RateLimit-Remaining": "0"}
	f := newFixture(t, withRoute(k, exhausted))
	f.mustWalk(t, walk.Params{Companies: []string{"101"}})
	if !slices.Equal(f.waits, []time.Duration{10 * time.Second}) {
		t.Errorf("recorded waits %v, want one 10s wait after the exhausted window", f.waits)
	}
}

// TestACancellationDuringAWaitEndsTheWalkIncomplete.
func TestACancellationDuringAWaitEndsTheWalkIncomplete(t *testing.T) {
	f := newFixture(t, withRoute(readKey("deals", "301"), limited("TEN_SECONDLY_ROLLING", secondly)))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.onWait = cancel
	res, err := f.walkWith(ctx, t, fullParams(t))
	if err != nil {
		t.Fatalf("got %v, want an incomplete walk", err)
	}
	if res.Complete.IsComplete() || !strings.Contains(res.Complete.Reason(), "the walk was cancelled") {
		t.Fatalf("got complete=%v reason %q, want incomplete naming the cancellation", res.Complete.IsComplete(), res.Complete.Reason())
	}
	if n := len(f.requestsWith("POST /crm/associations/")); n != 0 {
		t.Errorf("the cancelled walk made %d association reads after the cancellation", n)
	}
}
