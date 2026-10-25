// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/slack/internal/walk"
)

func sortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }

// TestAFullFixtureWalkEmitsTheWholeCorpusAndIsComplete: every channel page,
// every reply page and every search page lands as a message node, the walk
// asserts completeness, and the emitted vocabulary is the declared one.
func TestAFullFixtureWalkEmitsTheWholeCorpusAndIsComplete(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, fullParams())

	if !res.Complete.IsComplete() {
		t.Fatalf("a clean fixture walk reported incomplete: %s", res.Complete.Reason())
	}
	want := fixtureMessageIDs(t)
	got := nodesOfType(res, walk.NodeMessage)
	if !slices.Equal(sortedKeys(got), sortedKeys(want)) {
		t.Errorf("message nodes:\n got %v\nwant %v", sortedKeys(got), sortedKeys(want))
	}
	wantChannels := []string{"slack:channel:C0123GENERAL", "slack:channel:C0123OPS", "slack:channel:C0123RANDOM"}
	if got := sortedKeys(nodesOfType(res, walk.NodeChannel)); !slices.Equal(got, wantChannels) {
		t.Errorf("channel nodes: got %v want %v", got, wantChannels)
	}
	wantUsers := []string{"slack:user:U0123ALICE", "slack:user:U0123BOB"}
	if got := sortedKeys(nodesOfType(res, walk.NodeUser)); !slices.Equal(got, wantUsers) {
		t.Errorf("user nodes: got %v want %v", got, wantUsers)
	}

	decl := walk.Collector{}.Describe()
	emittedNodes, emittedEdges := map[string]bool{}, map[string]bool{}
	for _, n := range res.Nodes {
		emittedNodes[n.Type] = true
	}
	for _, e := range res.Edges {
		emittedEdges[e.Type] = true
	}
	if got := sortedKeys(emittedNodes); !slices.Equal(got, slices.Sorted(slices.Values(decl.NodeTypes))) {
		t.Errorf("emitted node types %v differ from the declared %v", got, decl.NodeTypes)
	}
	if got := sortedKeys(emittedEdges); !slices.Equal(got, slices.Sorted(slices.Values(decl.EdgeTypes))) {
		t.Errorf("emitted edge types %v differ from the declared %v", got, decl.EdgeTypes)
	}
}

// TestEveryPageIsRequestedOnceWithTheCursorSentBackVerbatim: history, replies
// and search each serve two pages; each page is requested exactly once and the
// second request carries the first answer's next_cursor, URL-encoded.
func TestEveryPageIsRequestedOnceWithTheCursorSentBackVerbatim(t *testing.T) {
	f := newFixture(t, corpus())
	f.mustWalk(t, fullParams())
	for _, key := range []string{
		historyKey("C0123GENERAL", ""), historyKey("C0123GENERAL", "bmV4dF90czoxNTEyMDg1ODYx"),
		threadRepliesKey(""), threadRepliesKey("dXNlcjpVMEc5V0ZYTlo="),
		searchKey("deploy", "*"), searchKey("deploy", "c2VhcmNoOnBhZ2Uy"),
	} {
		if n := f.count(key); n != 1 {
			t.Errorf("%s was requested %d times, want 1", key, n)
		}
	}
	var sawEncoded bool
	for _, r := range f.requestsTo("conversations.replies") {
		if strings.Contains(r.rawQuery, "cursor=dXNlcjpVMEc5V0ZYTlo%3D") {
			sawEncoded = true
		}
	}
	if !sawEncoded {
		t.Errorf("the replies cursor ending in '=' was not sent back URL-encoded as %%3D")
	}
}

// TestOnlyThreadParentsAreReadForRepliesAndEachReplyPointsAtItsParent.
func TestOnlyThreadParentsAreReadForRepliesAndEachReplyPointsAtItsParent(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, fullParams())
	for _, r := range f.requestsTo("conversations.replies") {
		if r.query.Get("ts") != "1512104434.000490" {
			t.Errorf("conversations.replies was called for %s, which has no replies", r.query.Get("ts"))
		}
	}
	parent := "slack:message:C0123GENERAL:1512104434.000490"
	want := map[string]bool{}
	for _, ts := range []string{"1512104440.000500", "1512104450.000600", "1512104460.000620"} {
		want["slack:message:C0123GENERAL:"+ts+" -> "+parent] = true
	}
	if got := edgesOfType(res, walk.EdgeRepliesTo); !maps.Equal(got, want) {
		t.Errorf("REPLIES_TO edges:\n got %v\nwant %v", sortedKeys(got), sortedKeys(want))
	}
	parents := 0
	for _, n := range res.Nodes {
		if n.ID == parent {
			parents++
		}
	}
	if parents != 1 {
		t.Errorf("the thread parent was emitted %d times, want 1", parents)
	}
}

// TestATokenSlackRefusesFailsTheCollectNamingTheCodeAndTheVariable.
func TestATokenSlackRefusesFailsTheCollectNamingTheCodeAndTheVariable(t *testing.T) {
	for _, code := range []string{"invalid_auth", "not_authed", "token_revoked", "account_inactive"} {
		t.Run(code, func(t *testing.T) {
			f := newFixture(t, withRoute(infoKey("C0123GENERAL"), ok(`{"ok": false, "error": "`+code+`"}`)))
			res, err := f.walkWith(t.Context(), t, fullParams())
			if err == nil {
				t.Fatalf("a refused token produced a result with %d nodes and no error", len(res.Nodes))
			}
			for _, want := range []string{code, walk.TokenVariable} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the error does not name %q: %v", want, err)
				}
			}
		})
	}
}

// TestAMissingScopeOrWrongTokenTypeFailsNamingWhatIsNeeded.
func TestAMissingScopeOrWrongTokenTypeFailsNamingWhatIsNeeded(t *testing.T) {
	for _, c := range []struct {
		name  string
		key   string
		body  string
		wants []string
	}{
		{name: "missing_scope on history", key: historyKey("C0123GENERAL", ""),
			body:  `{"ok": false, "error": "missing_scope", "needed": "channels:history", "provided": "users:read"}`,
			wants: []string{"missing_scope", "channels:history, groups:history, im:history, mpim:history", walk.TokenVariable}},
		{name: "bot token on search", key: searchKey("deploy", "*"),
			body:  `{"ok": false, "error": "not_allowed_token_type"}`,
			wants: []string{"not_allowed_token_type", "user (xoxp) token", "search:read"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, withRoute(c.key, ok(c.body)))
			_, err := f.walkWith(t.Context(), t, fullParams())
			if err == nil {
				t.Fatalf("the walk succeeded")
			}
			for _, want := range c.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the error does not name %q: %v", want, err)
				}
			}
		})
	}
}

// TestWalkCompleteIsTruthful: each of these makes the walk incomplete with a
// reason, and each empty read is complete.
func TestWalkCompleteIsTruthful(t *testing.T) {
	limitedHistory := strings.Replace(randomHistory, `"has_more": false`, `"has_more": false, "is_limited": true`, 1)
	shortSearch := strings.Replace(search2, `"total": 2}`, `"total": 3}`, 1)
	for _, c := range []struct {
		name       string
		routes     map[string][]answer
		cancel     bool
		wantReason string
		keptID     string
	}{
		{name: "is_limited", routes: withRoute(historyKey("C0123RANDOM", ""), ok(limitedHistory)),
			wantReason: "free-plan", keptID: "slack:message:C0123RANDOM:1512200000.000100"},
		{name: "search total above what was read", routes: withRoute(searchKey("deploy", "c2VhcmNoOnBhZ2Uy"), ok(shortSearch)),
			wantReason: "reported 3 matches and 2 were read", keptID: "slack:message:C0123OPS:1512300100.000200"},
		{name: "5xx in mid-pagination", routes: withRoute(historyKey("C0123GENERAL", "bmV4dF90czoxNTEyMDg1ODYx"),
			answer{status: http.StatusInternalServerError, body: "upstream unavailable"}),
			wantReason: "HTTP 500", keptID: "slack:message:C0123GENERAL:1512085950.000216"},
		{name: "cancelled context", routes: corpus(), cancel: true, wantReason: "cancelled"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, c.routes)
			ctx := t.Context()
			if c.cancel {
				cctx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cctx
			}
			res, err := f.walkWith(ctx, t, fullParams())
			if err != nil {
				t.Fatalf("the walk failed instead of reporting incomplete: %v", err)
			}
			if res.Complete.IsComplete() {
				t.Fatalf("the walk reported complete")
			}
			if !strings.Contains(res.Complete.Reason(), c.wantReason) {
				t.Errorf("the reason does not name %q: %s", c.wantReason, res.Complete.Reason())
			}
			if c.keptID != "" {
				if _, kept := nodesOfType(res, walk.NodeMessage)[c.keptID]; !kept {
					t.Errorf("%s, read before the incomplete read, was not kept", c.keptID)
				}
			}
		})
	}

	t.Run("EmptyChannelAndEmptySearchAreComplete", func(t *testing.T) {
		routes := corpus()
		routes[historyKey("C0123RANDOM", "")] = []answer{ok(`{"ok": true, "messages": [], "has_more": false, "pin_count": 0, "response_metadata": {"next_cursor": ""}}`)}
		routes[searchKey("nothing", "*")] = []answer{ok(`{"ok": true, "query": "nothing", "messages": {"total": 0, "matches": [], "paging": {"count": 100, "page": 1, "pages": 0, "total": 0}}}`)}
		f := newFixture(t, routes)
		res := f.mustWalk(t, walk.Params{Channels: []string{"C0123RANDOM"}, Search: []string{"nothing"}})
		if !res.Complete.IsComplete() {
			t.Fatalf("empty reads reported incomplete: %s", res.Complete.Reason())
		}
		if n := len(nodesOfType(res, walk.NodeMessage)); n != 0 {
			t.Errorf("empty reads emitted %d message nodes", n)
		}
	})
}

// TestReadsThatEndWithoutARefusalAreMarkedIncomplete: an answer that loses
// part of one read without refusing the token or the target marks the walk
// incomplete, names the read, and keeps the rest of the walk.
func TestReadsThatEndWithoutARefusalAreMarkedIncomplete(t *testing.T) {
	for _, c := range []struct {
		name, key, body string
		status          int
		want            string
		check           func(t *testing.T, res framework.Result)
	}{
		{name: "invalid_cursor", key: historyKey("C0123GENERAL", "bmV4dF90czoxNTEyMDg1ODYx"),
			body: `{"ok": false, "error": "invalid_cursor"}`, want: "channel C0123GENERAL: conversations.history answered invalid_cursor"},
		{name: "ratelimited on HTTP 200", key: searchKey("deploy", "*"),
			body: `{"ok": false, "error": "ratelimited"}`, want: `search "deploy": search.messages answered ratelimited`},
		{name: "user_not_found", key: userKey("U0123BOB"), body: `{"ok": false, "error": "user_not_found"}`,
			want: "user U0123BOB: users.info answered user_not_found",
			check: func(t *testing.T, res framework.Result) {
				stub, ok := nodesOfType(res, walk.NodeUser)["slack:user:U0123BOB"]
				if !ok || stub.Metadata["user_id"] != "U0123BOB" {
					t.Errorf("the unread poster has no user node to end its POSTED_BY edges: %+v", stub)
				}
			}},
		{name: "conversations.info 5xx", key: infoKey("C0123GENERAL"), status: http.StatusBadGateway, body: "bad gateway",
			want: "channel C0123GENERAL: slack conversations.info answered HTTP 502",
			check: func(t *testing.T, res framework.Result) {
				if _, ok := nodesOfType(res, walk.NodeChannel)["slack:channel:C0123GENERAL"]; !ok {
					t.Errorf("the channel whose info read failed has no node to end its BELONGS_TO edges")
				}
				if _, ok := nodesOfType(res, walk.NodeMessage)["slack:message:C0123GENERAL:1512085950.000216"]; !ok {
					t.Errorf("the channel's history was not read after its info read failed")
				}
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			status := c.status
			if status == 0 {
				status = http.StatusOK
			}
			f := newFixture(t, withRoute(c.key, answer{status: status, body: c.body}))
			res, err := f.walkWith(t.Context(), t, fullParams())
			if err != nil {
				t.Fatalf("the walk failed instead of reporting incomplete: %v", err)
			}
			if res.Complete.IsComplete() || !strings.Contains(res.Complete.Reason(), c.want) {
				t.Fatalf("got complete=%v reason %q, want an incomplete walk naming %q", res.Complete.IsComplete(), res.Complete.Reason(), c.want)
			}
			if c.check != nil {
				c.check(t, res)
			}
		})
	}
}

// TestTheWindowReachesTheWireAndFiltersSearch.
func TestTheWindowReachesTheWireAndFiltersSearch(t *testing.T) {
	start, end := "2017-12-01T00:00:00Z", "2017-12-03T10:30:00.5Z"
	startT, _ := time.Parse(time.RFC3339, start)
	endT, _ := time.Parse(time.RFC3339, end)
	wantOldest := fmt.Sprintf("%d.000000", startT.Unix())
	wantLatest := fmt.Sprintf("%d.500000", endT.Unix())

	f := newFixture(t, corpus())
	res := f.mustWalk(t, walk.Params{Channels: []string{"C0123RANDOM"}, Search: []string{"deploy"}, StartTime: start, EndTime: end})
	history := f.requestsTo("conversations.history")
	if len(history) == 0 {
		t.Fatalf("no history request was made")
	}
	for _, r := range history {
		if r.query.Get("oldest") != wantOldest || r.query.Get("latest") != wantLatest || r.query.Get("inclusive") != "true" {
			t.Errorf("history window oldest=%q latest=%q inclusive=%q, want %q %q true",
				r.query.Get("oldest"), r.query.Get("latest"), r.query.Get("inclusive"), wantOldest, wantLatest)
		}
	}
	// 1512300000 is 2017-12-03T11:20:00Z (outside); move one match inside.
	msgs := nodesOfType(res, walk.NodeMessage)
	if _, in := msgs["slack:message:C0123RANDOM:1512300000.000100"]; in {
		t.Errorf("a search match outside the window was emitted")
	}
	if !res.Complete.IsComplete() {
		t.Errorf("filtering search matches by the window made the walk incomplete: %s", res.Complete.Reason())
	}

	inside := strings.Replace(search1, `"ts": "1512300000.000100"`, `"ts": "1512290000.000100"`, 1)
	f2 := newFixture(t, withRoute(searchKey("deploy", "*"), ok(inside)))
	res2 := f2.mustWalk(t, walk.Params{Search: []string{"deploy"}, StartTime: start, EndTime: end})
	msgs2 := nodesOfType(res2, walk.NodeMessage)
	if _, in := msgs2["slack:message:C0123RANDOM:1512290000.000100"]; !in {
		t.Errorf("a search match inside the window was not emitted: %v", sortedKeys(msgs2))
	}
	if _, in := msgs2["slack:message:C0123OPS:1512300100.000200"]; in {
		t.Errorf("a search match outside the window was emitted")
	}
	if !res2.Complete.IsComplete() {
		t.Errorf("the windowed search reported incomplete: %s", res2.Complete.Reason())
	}
}

// TestParamsAreRefusedNamingTheParam: each refusal names the param path and
// makes no request.
func TestParamsAreRefusedNamingTheParam(t *testing.T) {
	for _, c := range []struct {
		name   string
		params walk.Params
		want   string
	}{
		{name: "no target", params: walk.Params{}, want: "params.channels"},
		{name: "empty channel", params: walk.Params{Channels: []string{"C1", " "}}, want: "params.channels[1]"},
		{name: "thread with empty channel", params: walk.Params{Threads: []walk.Thread{{Channel: "", TS: "1512104434.000490"}}}, want: "params.threads[0].channel"},
		{name: "thread with malformed ts", params: walk.Params{Threads: []walk.Thread{{Channel: "C1", TS: "yesterday"}}}, want: "params.threads[0].ts"},
		{name: "empty search", params: walk.Params{Search: []string{""}}, want: "params.search[0]"},
		{name: "reversed window", params: walk.Params{Channels: []string{"C1"}, StartTime: "2024-02-01T00:00:00Z", EndTime: "2024-01-01T00:00:00Z"}, want: "params.end_time"},
		{name: "start not RFC 3339", params: walk.Params{Channels: []string{"C1"}, StartTime: "2024-01-01"}, want: "params.start_time"},
		{name: "end not RFC 3339", params: walk.Params{Channels: []string{"C1"}, EndTime: "last week"}, want: "params.end_time"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, corpus())
			_, err := f.walkWith(t.Context(), t, c.params)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want a refusal naming %s", err, c.want)
			}
			if n := len(f.requests); n != 0 {
				t.Errorf("a refused collect made %d requests", n)
			}
		})
	}
}

// TestAThreadTargetReadsRepliesOnly.
func TestAThreadTargetReadsRepliesOnly(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, walk.Params{Threads: []walk.Thread{{Channel: "C0123GENERAL", TS: "1512104434.000490"}}})
	if n := len(f.requestsTo("conversations.history")); n != 0 {
		t.Errorf("a thread target read conversations.history %d times", n)
	}
	if n := len(nodesOfType(res, walk.NodeMessage)); n != 4 {
		t.Errorf("the thread target emitted %d messages, want the parent and three replies", n)
	}
	if n := len(edgesOfType(res, walk.EdgeRepliesTo)); n != 3 {
		t.Errorf("the thread target emitted %d REPLIES_TO edges, want 3", n)
	}
}

// TestSearchMatchesCarryTheirChannelAndPoster.
func TestSearchMatchesCarryTheirChannelAndPoster(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, walk.Params{Search: []string{"deploy"}})
	belongs, posted := edgesOfType(res, walk.EdgeBelongsTo), edgesOfType(res, walk.EdgePostedBy)
	for _, want := range []struct{ msg, channel, user string }{
		{msg: "slack:message:C0123RANDOM:1512300000.000100", channel: "slack:channel:C0123RANDOM", user: "slack:user:U0123BOB"},
		{msg: "slack:message:C0123OPS:1512300100.000200", channel: "slack:channel:C0123OPS", user: "slack:user:U0123ALICE"},
	} {
		if !belongs[want.msg+" -> "+want.channel] {
			t.Errorf("%s carries no BELONGS_TO %s", want.msg, want.channel)
		}
		if !posted[want.msg+" -> "+want.user] {
			t.Errorf("%s carries no POSTED_BY %s", want.msg, want.user)
		}
	}
	if ops := nodesOfType(res, walk.NodeChannel)["slack:channel:C0123OPS"]; ops.Metadata["name"] != "ops" {
		t.Errorf("the search-only channel node does not carry the match's channel name: %+v", ops)
	}
}

// TestANamedTargetSlackCannotReadFailsTheCollectNamingIt.
func TestANamedTargetSlackCannotReadFailsTheCollectNamingIt(t *testing.T) {
	for _, c := range []struct {
		key, code string
	}{
		{key: infoKey("C0123RANDOM"), code: "channel_not_found"},
		{key: historyKey("C0123RANDOM", ""), code: "not_in_channel"},
	} {
		t.Run(c.code, func(t *testing.T) {
			f := newFixture(t, withRoute(c.key, ok(`{"ok": false, "error": "`+c.code+`"}`)))
			_, err := f.walkWith(t.Context(), t, fullParams())
			if err == nil || !strings.Contains(err.Error(), c.code) || !strings.Contains(err.Error(), "C0123RANDOM") {
				t.Fatalf("got %v, want a failure naming %s and C0123RANDOM", err, c.code)
			}
		})
	}
	t.Run("UnknownOkFalseCode", func(t *testing.T) {
		f := newFixture(t, withRoute(historyKey("C0123RANDOM", ""), ok(`{"ok": false, "error": "some_new_code"}`)))
		_, err := f.walkWith(t.Context(), t, fullParams())
		if err == nil || !strings.Contains(err.Error(), "some_new_code") {
			t.Fatalf("got %v, want a failure naming some_new_code", err)
		}
	})
}

// TestEachPosterIsReadOnceAndABotMessageHasNoPoster.
func TestEachPosterIsReadOnceAndABotMessageHasNoPoster(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, fullParams())
	for _, u := range []string{"U0123ALICE", "U0123BOB"} {
		if n := f.count(userKey(u)); n != 1 {
			t.Errorf("users.info for %s was requested %d times, want 1", u, n)
		}
	}
	bot := "slack:message:C0123GENERAL:1358877455.000010"
	for edge := range edgesOfType(res, walk.EdgePostedBy) {
		if strings.HasPrefix(edge, bot+" ") {
			t.Errorf("the bot message carries a POSTED_BY edge: %s", edge)
		}
	}
	if got := nodesOfType(res, walk.NodeMessage)[bot].Metadata["bot_id"]; got != "BB12033" {
		t.Errorf("the bot message's bot_id metadata is %q, want BB12033", got)
	}
}
