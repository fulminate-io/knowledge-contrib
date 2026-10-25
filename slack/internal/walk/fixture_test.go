// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/slack/internal/slackapi"
	"github.com/fulminate-io/knowledge-contrib/slack/internal/walk"
)

// fixtureToken is the token every fixture walk runs with.
const fixtureToken = "xoxb-1234-5678-fixtureSECRETvalue/+="

// Recorded Slack Web API answers. The shapes are transcribed from the method
// reference pages (conversations.history, conversations.replies,
// conversations.info, users.info, search.messages) and the bot_message event
// page; ids and texts are this corpus's own.
//
// Channel C0123GENERAL: two history pages; its first page carries a thread
// parent with three replies over two replies pages, and its second a bot
// message. Channel C0123RANDOM: one history page. Search "deploy": two pages,
// one match in C0123RANDOM and one in C0123OPS, which no channels target names.
const (
	generalInfo = `{"ok": true, "channel": {"id": "C0123GENERAL", "name": "general", "is_channel": true,
		"is_group": false, "is_im": false, "is_mpim": false, "is_private": false, "is_archived": false,
		"created": 1449252889, "topic": {"value": "Company-wide announcements", "creator": "U0123ALICE", "last_set": 1449709364},
		"purpose": {"value": "This channel is for team-wide communication.", "creator": "U0123ALICE", "last_set": 1449709334}}}`
	randomInfo = `{"ok": true, "channel": {"id": "C0123RANDOM", "name": "random", "is_channel": true,
		"is_private": false, "is_archived": false, "created": 1449252889,
		"topic": {"value": "", "creator": "", "last_set": 0}, "purpose": {"value": "", "creator": "", "last_set": 0}}}`

	generalHistory1 = `{"ok": true, "messages": [
		{"type": "message", "user": "U0123ALICE", "text": "I find you punny and would like to smell your nose letter", "ts": "1512085950.000216"},
		{"type": "message", "user": "U0123BOB", "text": "What, you want to smell my shoes better?", "ts": "1512104434.000490",
		 "thread_ts": "1512104434.000490", "reply_count": 3, "reply_users_count": 2, "latest_reply": "1512104460.000620"}
		], "has_more": true, "pin_count": 0, "response_metadata": {"next_cursor": "bmV4dF90czoxNTEyMDg1ODYx"}}`
	generalHistory2 = `{"ok": true, "messages": [
		{"type": "message", "subtype": "bot_message", "ts": "1358877455.000010", "text": "Pushing is the answer",
		 "bot_id": "BB12033", "username": "github", "icons": {}}
		], "has_more": false, "pin_count": 0, "response_metadata": {"next_cursor": ""}}`
	generalReplies1 = `{"ok": true, "messages": [
		{"type": "message", "user": "U0123BOB", "text": "What, you want to smell my shoes better?", "ts": "1512104434.000490",
		 "thread_ts": "1512104434.000490", "reply_count": 3},
		{"type": "message", "user": "U0123ALICE", "text": "island", "ts": "1512104440.000500", "thread_ts": "1512104434.000490", "parent_user_id": "U0123BOB"}
		], "has_more": true, "response_metadata": {"next_cursor": "dXNlcjpVMEc5V0ZYTlo="}}`
	generalReplies2 = `{"ok": true, "messages": [
		{"type": "message", "user": "U0123BOB", "text": "one island", "ts": "1512104450.000600", "thread_ts": "1512104434.000490", "parent_user_id": "U0123BOB"},
		{"type": "message", "user": "U0123ALICE", "text": "two islands", "ts": "1512104460.000620", "thread_ts": "1512104434.000490", "parent_user_id": "U0123BOB"}
		], "has_more": false, "response_metadata": {"next_cursor": ""}}`
	randomHistory = `{"ok": true, "messages": [
		{"type": "message", "user": "U0123ALICE", "text": "lunch?", "ts": "1512200000.000100"}
		], "has_more": false, "pin_count": 0}`

	search1 = `{"ok": true, "query": "deploy", "messages": {"total": 2, "matches": [
		{"channel": {"id": "C0123RANDOM", "is_ext_shared": false, "is_mpim": false, "is_org_shared": false, "is_pending_ext_shared": false,
		 "is_private": false, "is_shared": false, "name": "random", "pending_shared": []},
		 "iid": "cb64bdaa-c1e8-4631-8a91-0f78080113e9", "permalink": "https://example.slack.com/archives/C0123RANDOM/p1512300000000100",
		 "team": "T0123TEAM", "text": "deploy at noon", "ts": "1512300000.000100", "type": "message", "user": "U0123BOB", "username": "bob"}
		], "pagination": {"first": 1, "last": 1, "page": 1, "page_count": 2, "per_page": 100, "total_count": 2},
		"paging": {"count": 100, "page": 1, "pages": 2, "total": 2}},
		"response_metadata": {"next_cursor": "c2VhcmNoOnBhZ2Uy"}}`
	search2 = `{"ok": true, "query": "deploy", "messages": {"total": 2, "matches": [
		{"channel": {"id": "C0123OPS", "is_private": false, "name": "ops"},
		 "iid": "2d1b9c1e-3a4e-4c7c-9a47-6b2b2c0d0f11", "permalink": "https://example.slack.com/archives/C0123OPS/p1512300100000200",
		 "team": "T0123TEAM", "text": "deploy done", "ts": "1512300100.000200", "type": "message", "user": "U0123ALICE", "username": "alice"}
		], "paging": {"count": 100, "page": 2, "pages": 2, "total": 2}},
		"response_metadata": {"next_cursor": ""}}`

	aliceInfo = `{"ok": true, "user": {"id": "U0123ALICE", "team_id": "T0123TEAM", "name": "alice", "deleted": false,
		"real_name": "Alice Example", "tz": "America/Los_Angeles", "is_bot": false,
		"profile": {"title": "Engineer", "phone": "", "real_name": "Alice Example", "display_name": "alice", "email": "alice@example.com"}}}`
	bobInfo = `{"ok": true, "user": {"id": "U0123BOB", "team_id": "T0123TEAM", "name": "bob", "deleted": false,
		"real_name": "Bob Example", "tz": "America/New_York", "is_bot": false,
		"profile": {"title": "", "real_name": "Bob Example", "display_name": "bobby", "email": "bob@example.com"}}}`
)

// answer is one recorded response.
type answer struct {
	status     int
	retryAfter string
	body       string
}

func ok(body string) answer { return answer{status: http.StatusOK, body: body} }

// selector is the request key: the method and the query arguments that
// select what is read.
func selector(method string, q url.Values) string {
	keep := url.Values{}
	for _, k := range []string{"channel", "ts", "query", "user", "cursor"} {
		if v := q.Get(k); v != "" {
			keep.Set(k, v)
		}
	}
	return method + " " + keep.Encode()
}

func historyKey(channel, cursor string) string {
	return selector("conversations.history", url.Values{"channel": {channel}, "cursor": {cursor}})
}

// threadTS is the parent ts of the corpus's one thread, in C0123GENERAL.
const threadTS = "1512104434.000490"

// threadRepliesKey is the replies selector of the corpus's thread at cursor.
func threadRepliesKey(cursor string) string {
	return selector("conversations.replies", url.Values{"channel": {"C0123GENERAL"}, "ts": {threadTS}, "cursor": {cursor}})
}

func infoKey(channel string) string {
	return selector("conversations.info", url.Values{"channel": {channel}})
}

func userKey(user string) string { return selector("users.info", url.Values{"user": {user}}) }

func searchKey(query, cursor string) string {
	return selector("search.messages", url.Values{"query": {query}, "cursor": {cursor}})
}

// corpus returns the recorded answers keyed by selector.
func corpus() map[string][]answer {
	return map[string][]answer{
		infoKey("C0123GENERAL"):                                {ok(generalInfo)},
		infoKey("C0123RANDOM"):                                 {ok(randomInfo)},
		historyKey("C0123GENERAL", ""):                         {ok(generalHistory1)},
		historyKey("C0123GENERAL", "bmV4dF90czoxNTEyMDg1ODYx"): {ok(generalHistory2)},
		historyKey("C0123RANDOM", ""):                          {ok(randomHistory)},
		threadRepliesKey(""):                                   {ok(generalReplies1)},
		threadRepliesKey("dXNlcjpVMEc5V0ZYTlo="):               {ok(generalReplies2)},
		searchKey("deploy", "*"):                               {ok(search1)},
		searchKey("deploy", "c2VhcmNoOnBhZ2Uy"):                {ok(search2)},
		userKey("U0123ALICE"):                                  {ok(aliceInfo)},
		userKey("U0123BOB"):                                    {ok(bobInfo)},
	}
}

// recorded is one request the fixture served.
type recorded struct {
	key      string
	rawQuery string
	auth     string
	query    url.Values
}

// fixture is a real HTTP server standing in for the Slack Web API. Each
// selector serves its answers in order and repeats the last one.
type fixture struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	routes   map[string][]answer
	served   map[string]int
	requests []recorded
	waits    []time.Duration
	// onWait, when set, runs inside the wait seam before it returns.
	onWait func()
}

func newFixture(t *testing.T, routes map[string][]answer) *fixture {
	t.Helper()
	f := &fixture{t: t, routes: routes, served: map[string]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	method := strings.TrimPrefix(r.URL.Path, "/")
	key := selector(method, r.URL.Query())
	f.mu.Lock()
	f.requests = append(f.requests, recorded{key: key, rawQuery: r.URL.RawQuery, auth: r.Header.Get("Authorization"), query: r.URL.Query()})
	answers, found := f.routes[key]
	n := f.served[key]
	f.served[key] = n + 1
	f.mu.Unlock()
	if !found {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok": false, "error": "fixture_has_no_answer"}`)
		return
	}
	a := answers[min(n, len(answers)-1)]
	if a.retryAfter != "" {
		w.Header().Set("Retry-After", a.retryAfter)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(a.status)
	fmt.Fprint(w, a.body)
}

// count is how many times key was requested.
func (f *fixture) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.served[key]
}

// requestsTo is every recorded request whose method is method.
func (f *fixture) requestsTo(method string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.requests {
		if strings.HasPrefix(r.key, method+" ") {
			out = append(out, r)
		}
	}
	return out
}

// collector is the walk pointed at this fixture, with a wait seam that records
// every requested wait instead of sleeping.
func (f *fixture) collector() walk.Collector {
	return walk.Collector{Client: func(token string) *slackapi.Client {
		c := slackapi.NewAt(f.server.URL, f.server.Client(), token)
		c.Wait = func(ctx context.Context, d time.Duration) error {
			f.mu.Lock()
			f.waits = append(f.waits, d)
			hook := f.onWait
			f.mu.Unlock()
			if hook != nil {
				hook()
			}
			return ctx.Err()
		}
		return c
	}}
}

// walkWith runs one walk against the fixture with the fixture token set.
func (f *fixture) walkWith(ctx context.Context, t *testing.T, p walk.Params) (framework.Result, error) {
	t.Helper()
	t.Setenv(walk.TokenVariable, fixtureToken)
	return f.collector().Walk(ctx, "acme", p, framework.ForeignContext{})
}

// mustWalk runs one walk and fails the test on an error.
func (f *fixture) mustWalk(t *testing.T, p walk.Params) framework.Result {
	t.Helper()
	res, err := f.walkWith(t.Context(), t, p)
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}
	return res
}

// fullParams names every target of the corpus.
func fullParams() walk.Params {
	return walk.Params{Channels: []string{"C0123GENERAL", "C0123RANDOM"}, Search: []string{"deploy"}}
}

// fixtureMessages decodes the ts of every message a recorded body carries,
// with the channel the walk reads it under, as node ids computed in this test.
func fixtureMessageIDs(t *testing.T) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	add := func(channel, body string) {
		var page struct {
			Messages []struct {
				TS string `json:"ts"`
			} `json:"messages"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("decoding a fixture body: %v", err)
		}
		for _, m := range page.Messages {
			ids["slack:message:"+channel+":"+m.TS] = true
		}
	}
	add("C0123GENERAL", generalHistory1)
	add("C0123GENERAL", generalHistory2)
	add("C0123GENERAL", generalReplies1)
	add("C0123GENERAL", generalReplies2)
	add("C0123RANDOM", randomHistory)
	for _, body := range []string{search1, search2} {
		var page struct {
			Messages struct {
				Matches []struct {
					TS      string `json:"ts"`
					Channel struct {
						ID string `json:"id"`
					} `json:"channel"`
				} `json:"matches"`
			} `json:"messages"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("decoding a fixture search body: %v", err)
		}
		for _, m := range page.Messages.Matches {
			ids["slack:message:"+m.Channel.ID+":"+m.TS] = true
		}
	}
	return ids
}

// nodesOfType is the ids of every node of type kind.
func nodesOfType(res framework.Result, kind string) map[string]framework.Node {
	out := map[string]framework.Node{}
	for _, n := range res.Nodes {
		if n.Type == kind {
			out[n.ID] = n
		}
	}
	return out
}

// edgesOfType is every edge of type kind, as "from -> to".
func edgesOfType(res framework.Result, kind string) map[string]bool {
	out := map[string]bool{}
	for _, e := range res.Edges {
		if e.Type == kind {
			out[e.FromID+" -> "+e.ToID] = true
		}
	}
	return out
}

// withRoute returns the corpus with key's answers replaced.
func withRoute(key string, answers ...answer) map[string][]answer {
	routes := corpus()
	routes[key] = answers
	return routes
}
