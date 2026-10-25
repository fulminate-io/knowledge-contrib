// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/notion/internal/notionapi"
	"github.com/fulminate-io/knowledge-contrib/notion/internal/walk"
)

func limited(status int, retryAfter string) answer {
	return answer{status: status, retryAfter: retryAfter, body: errorBody(status, "rate_limited", "You have been rate limited. Please try again in a few minutes.")}
}

// TestA429Or529IsRetriedAfterTheRetryAfterItNames.
func TestA429Or529IsRetriedAfterTheRetryAfterItNames(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
	}{{name: "429", status: http.StatusTooManyRequests}, {name: "529", status: notionapi.StatusServiceOverload}} {
		t.Run(c.name, func(t *testing.T) {
			k := blocksKey(blockKale, "")
			f := newFixture(t, withRoute(k, limited(c.status, "5"), corpus()[k][0]))
			res := f.mustWalk(t, fullParams())
			if !slices.Equal(f.waits, []time.Duration{5 * time.Second}) {
				t.Errorf("recorded waits %v, want exactly [5s]", f.waits)
			}
			if n := f.count(k); n != 2 {
				t.Errorf("the limited read was requested %d times, want 2", n)
			}
			if !res.Complete.IsComplete() {
				t.Errorf("a walk whose rate limit cleared reported incomplete: %s", res.Complete.Reason())
			}
		})
	}
}

// TestRateLimitOutcomesThatEndARead: a persistent 429, a 429 without
// Retry-After and a Retry-After beyond the retry deadline each end the read as
// incomplete, keep the rest of the walk, and record only the waits the rule
// allows.
func TestRateLimitOutcomesThatEndARead(t *testing.T) {
	k := blocksKey(blockKale, "")
	for _, c := range []struct {
		name       string
		answer     answer
		wantWaits  []time.Duration
		wantReason []string
		wantCount  int
	}{
		{name: "persistent 429", answer: limited(http.StatusTooManyRequests, "1"), wantWaits: []time.Duration{time.Second, time.Second},
			wantReason: []string{"/blocks/" + blockKale + "/children", "429", "3 attempts"}, wantCount: notionapi.MaxAttempts},
		{name: "429 without Retry-After", answer: limited(http.StatusTooManyRequests, ""),
			wantReason: []string{"/blocks/" + blockKale + "/children", "429", "no Retry-After"}, wantCount: 1},
		{name: "Retry-After beyond the deadline", answer: limited(http.StatusTooManyRequests, "180"),
			wantReason: []string{"/blocks/" + blockKale + "/children", "429", "180"}, wantCount: 1},
		{name: "Retry-After not a number", answer: limited(http.StatusTooManyRequests, "soon"),
			wantReason: []string{"/blocks/" + blockKale + "/children", "429", "not a whole number"}, wantCount: 1},
		{name: "negative Retry-After", answer: limited(http.StatusTooManyRequests, "-3"),
			wantReason: []string{"/blocks/" + blockKale + "/children", "429", "negative"}, wantCount: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, withRoute(k, c.answer))
			res := f.mustWalk(t, fullParams())
			if res.Complete.IsComplete() {
				t.Fatalf("the walk reported complete although a read ended rate-limited")
			}
			for _, want := range c.wantReason {
				if !strings.Contains(res.Complete.Reason(), want) {
					t.Errorf("the reason does not name %q: %s", want, res.Complete.Reason())
				}
			}
			if !slices.Equal(f.waits, c.wantWaits) {
				t.Errorf("recorded waits %v, want %v", f.waits, c.wantWaits)
			}
			if n := f.count(k); n != c.wantCount {
				t.Errorf("the limited read was requested %d times, want %d", n, c.wantCount)
			}
			if _, kept := nodesOfType(res, walk.NodePage)["notion:page:"+rowOne]; !kept {
				t.Errorf("the rest of the walk was not kept")
			}
		})
	}
}

// TestRefusalsFailTheCollectNamingTheCause.
func TestRefusalsFailTheCollectNamingTheCause(t *testing.T) {
	for _, c := range []struct {
		name      string
		key       string
		answer    answer
		wants     []string
		wantCount int
	}{
		{name: "401", key: pageKey(pageRoadmap),
			answer: answer{status: http.StatusUnauthorized, body: errorBody(401, "unauthorized", "API token is invalid.")},
			wants:  []string{"unauthorized", walk.TokenVariable}, wantCount: 1},
		{name: "403 on comments", key: commentsKey(pageRoadmap, ""),
			answer: answer{status: http.StatusForbidden, body: errorBody(403, "restricted_resource", "Insufficient permissions for this endpoint.")},
			wants:  []string{"/comments", "restricted_resource", "Insufficient permissions for this endpoint."}, wantCount: 1},
		{name: "blocked connection", key: pageKey(pageRoadmap),
			answer: answer{status: http.StatusTooManyRequests, retryAfter: "1", body: `{"object": "error", "status": 429, "code": "rate_limited",
				"message": "This connection's API access has been restricted.", "additional_data": {"rate_limit_reason": "public_api_request_blocked"}}`},
			wants: []string{notionapi.BlockedReason}, wantCount: 1},
		{name: "named page not found", key: pageKey(pageRoadmap),
			answer: answer{status: http.StatusNotFound, body: errorBody(404, "object_not_found", "Could not find page.")},
			wants:  []string{pageRoadmap, "not shared with or visible to"}, wantCount: 1},
		{name: "unknown 4xx", key: pageKey(pageRoadmap),
			answer: answer{status: http.StatusConflict, body: errorBody(409, "conflict_error", "Transaction could not be completed.")},
			wants:  []string{"409", "conflict_error"}, wantCount: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, withRoute(c.key, c.answer))
			res, err := f.walkWith(t.Context(), t, fullParams())
			if err == nil {
				t.Fatalf("got a result with %d nodes and no error, want the collect to fail", len(res.Nodes))
			}
			for _, want := range c.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the error does not name %q: %v", want, err)
				}
			}
			if n := f.count(c.key); n != c.wantCount {
				t.Errorf("%s was requested %d times, want %d", c.key, n, c.wantCount)
			}
			if len(f.waits) != 0 {
				t.Errorf("a refusal recorded waits %v", f.waits)
			}
		})
	}
}

// TestADescendantNotFoundMarksTheWalkIncomplete: a child page the token
// cannot read loses that page only.
func TestADescendantNotFoundMarksTheWalkIncomplete(t *testing.T) {
	f := newFixture(t, withRoute(pageKey(pageSub), answer{status: http.StatusNotFound, body: errorBody(404, "object_not_found", "Could not find page.")}))
	res, err := f.walkWith(t.Context(), t, fullParams())
	if err != nil {
		t.Fatalf("got %v, want an incomplete walk", err)
	}
	if res.Complete.IsComplete() || !strings.Contains(res.Complete.Reason(), "page "+pageSub) {
		t.Fatalf("got complete=%v reason %q, want an incomplete walk naming page %s", res.Complete.IsComplete(), res.Complete.Reason(), pageSub)
	}
	if _, kept := nodesOfType(res, walk.NodePage)["notion:page:"+pageRoadmap]; !kept {
		t.Errorf("the parent page was not kept")
	}
}

// TestWalkCompleteIsTruthful: a 5xx in mid-pagination and a cancel each make
// the walk incomplete and keep what was read; empty reads are complete.
func TestWalkCompleteIsTruthful(t *testing.T) {
	t.Run("MidPagination5xx", func(t *testing.T) {
		f := newFixture(t, withRoute(blocksKey(pageRoadmap, "c2Vjb25kLXBhZ2U"), answer{status: http.StatusBadGateway, body: "bad gateway"}))
		res := f.mustWalk(t, fullParams())
		if res.Complete.IsComplete() || !strings.Contains(res.Complete.Reason(), "502") {
			t.Fatalf("got complete=%v reason %q, want incomplete naming 502", res.Complete.IsComplete(), res.Complete.Reason())
		}
		if got := nodesOfType(res, walk.NodePage)["notion:page:"+pageRoadmap].Content; !strings.Contains(got, "Lacinato kale") {
			t.Errorf("the first block page was not kept: %q", got)
		}
	})
	t.Run("CancelDuringAWait", func(t *testing.T) {
		k := blocksKey(blockKale, "")
		f := newFixture(t, withRoute(k, limited(http.StatusTooManyRequests, "1")))
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		f.onWait = cancel
		res, err := f.walkWith(ctx, t, fullParams())
		if err != nil {
			t.Fatalf("got %v, want an incomplete walk", err)
		}
		if res.Complete.IsComplete() || !strings.Contains(res.Complete.Reason(), "the walk was cancelled") {
			t.Fatalf("got complete=%v reason %q, want incomplete naming the cancellation", res.Complete.IsComplete(), res.Complete.Reason())
		}
		if n := len(f.requestsWith("POST ")); n != 0 {
			t.Errorf("the cancelled walk made %d requests after the cancellation", n)
		}
	})
	t.Run("EmptyReadsAreComplete", func(t *testing.T) {
		routes := corpus()
		routes[blocksKey(pageRoadmap, "")] = []answer{ok(blockList(""))}
		routes[queryKey(sourceTasks, "")] = []answer{ok(listOf("page_or_data_source", ""))}
		routes[searchKey("nothing", "")] = []answer{ok(listOf("page_or_data_source", ""))}
		f := newFixture(t, routes)
		res := f.mustWalk(t, walk.Params{Pages: []string{pageRoadmap}, Databases: []string{databaseTasks}, TitleSearch: []string{"nothing"}})
		if !res.Complete.IsComplete() {
			t.Fatalf("empty reads reported incomplete: %s", res.Complete.Reason())
		}
		if got := nodesOfType(res, walk.NodePage)["notion:page:"+pageRoadmap].Content; got != "" {
			t.Errorf("a page with no blocks has content %q", got)
		}
	})
}

// TestParamsAreRefusedNamingTheParam: each refusal names the param path and
// makes no request.
func TestParamsAreRefusedNamingTheParam(t *testing.T) {
	for _, c := range []struct {
		name   string
		params walk.Params
		want   string
	}{
		{name: "no target", params: walk.Params{}, want: "params.pages"},
		{name: "malformed page id", params: walk.Params{Pages: []string{"not-an-id"}}, want: "params.pages[0]"},
		{name: "empty database id", params: walk.Params{Databases: []string{""}}, want: "params.databases[0]"},
		{name: "malformed data source id", params: walk.Params{DataSources: []walk.DataSourceQuery{{ID: "x"}}}, want: "params.data_sources[0].id"},
		{name: "empty title search", params: walk.Params{TitleSearch: []string{" "}}, want: "params.title_search[0]"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, corpus())
			_, err := f.walkWith(t.Context(), t, c.params)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want a refusal naming %s", err, c.want)
			}
			if n := len(f.requestsWith("")); n != 0 {
				t.Errorf("a refused collect made %d requests", n)
			}
		})
	}
}
