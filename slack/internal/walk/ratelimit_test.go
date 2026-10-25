// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/slack/internal/slackapi"
	"github.com/fulminate-io/knowledge-contrib/slack/internal/walk"
)

// TestA429IsRetriedAfterTheRetryAfterItNames.
func TestA429IsRetriedAfterTheRetryAfterItNames(t *testing.T) {
	key := threadRepliesKey("")
	f := newFixture(t, withRoute(key, answer{status: http.StatusTooManyRequests, retryAfter: "7", body: `{"ok":false,"error":"ratelimited"}`}, ok(generalReplies1)))
	res := f.mustWalk(t, fullParams())
	if !slices.Equal(f.waits, []time.Duration{7 * time.Second}) {
		t.Errorf("recorded waits %v, want exactly [7s]", f.waits)
	}
	if n := f.count(key); n != 2 {
		t.Errorf("the rate-limited page was requested %d times, want 2", n)
	}
	if !res.Complete.IsComplete() {
		t.Errorf("a walk whose 429 cleared reported incomplete: %s", res.Complete.Reason())
	}
}

// TestRateLimitOutcomesThatEndARead: a persistent 429, a 429 without
// Retry-After, and a Retry-After beyond the retry deadline each end the
// replies read as incomplete, keep what was already read, and record only the
// waits the rule allows.
func TestRateLimitOutcomesThatEndARead(t *testing.T) {
	key := threadRepliesKey("")
	limited := func(retryAfter string) answer {
		return answer{status: http.StatusTooManyRequests, retryAfter: retryAfter, body: `{"ok":false,"error":"ratelimited"}`}
	}
	for _, c := range []struct {
		name       string
		answers    []answer
		wantWaits  []time.Duration
		wantReason []string
		wantCount  int
	}{
		{name: "persistent 429", answers: []answer{limited("1")}, wantWaits: []time.Duration{time.Second, time.Second},
			wantReason: []string{"conversations.replies", "429", "3 attempts"}, wantCount: slackapi.MaxAttempts},
		{name: "429 without Retry-After", answers: []answer{limited("")},
			wantReason: []string{"conversations.replies", "429", "no Retry-After"}, wantCount: 1},
		{name: "Retry-After beyond the deadline", answers: []answer{limited("600")},
			wantReason: []string{"conversations.replies", "429", "600"}, wantCount: 1},
		{name: "Retry-After not a number", answers: []answer{limited("soon")},
			wantReason: []string{"conversations.replies", "429", "not a whole number"}, wantCount: 1},
		{name: "negative Retry-After", answers: []answer{limited("-3")},
			wantReason: []string{"conversations.replies", "429", "not a whole number"}, wantCount: 1},
		{name: "second wait beyond what remains", answers: []answer{limited("70")}, wantWaits: []time.Duration{70 * time.Second},
			wantReason: []string{"conversations.replies", "70s exceeds"}, wantCount: 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, withRoute(key, c.answers...))
			res := f.mustWalk(t, fullParams())
			if res.Complete.IsComplete() {
				t.Fatalf("the walk reported complete although a replies read ended rate-limited")
			}
			for _, want := range c.wantReason {
				if !strings.Contains(res.Complete.Reason(), want) {
					t.Errorf("the reason does not name %q: %s", want, res.Complete.Reason())
				}
			}
			if !slices.Equal(f.waits, c.wantWaits) {
				t.Errorf("recorded waits %v, want %v", f.waits, c.wantWaits)
			}
			if n := f.count(key); n != c.wantCount {
				t.Errorf("the rate-limited page was requested %d times, want %d", n, c.wantCount)
			}
			if _, kept := nodesOfType(res, walk.NodeMessage)["slack:message:C0123GENERAL:1512085950.000216"]; !kept {
				t.Errorf("the history read before the rate limit was not kept")
			}
		})
	}
}

// TestACancellationDuringAWaitEndsTheWalkIncomplete: the context is cancelled
// while the client waits on a Retry-After; the walk returns what it read,
// marked incomplete because it was cancelled, and makes no further request.
func TestACancellationDuringAWaitEndsTheWalkIncomplete(t *testing.T) {
	key := threadRepliesKey("")
	f := newFixture(t, withRoute(key, answer{status: http.StatusTooManyRequests, retryAfter: "1", body: `{"ok":false}`}))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.onWait = cancel
	res, err := f.walkWith(ctx, t, fullParams())
	if err != nil {
		t.Fatalf("the walk failed instead of reporting incomplete: %v", err)
	}
	if res.Complete.IsComplete() || !strings.Contains(res.Complete.Reason(), "the walk was cancelled") {
		t.Fatalf("got complete=%v reason %q, want an incomplete walk naming the cancellation", res.Complete.IsComplete(), res.Complete.Reason())
	}
	if n := len(f.requestsTo("search.messages")) + len(f.requestsTo("users.info")); n != 0 {
		t.Errorf("the cancelled walk made %d requests after the cancellation", n)
	}
	if _, kept := nodesOfType(res, walk.NodeMessage)["slack:message:C0123GENERAL:1512085950.000216"]; !kept {
		t.Errorf("the history read before the cancellation was not kept")
	}
}
