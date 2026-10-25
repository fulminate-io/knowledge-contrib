// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/hubapi"
	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/walk"
)

// fixtureToken is the token every fixture walk runs with.
const fixtureToken = "pat-na1-fixture/SECRET+value=0123"

// The response shapes below are transcribed from HubSpot's API reference for
// batch read, associations batch read, lists, list memberships and CRM search,
// and the usage guidelines' 429 body. Ids and values are this corpus's own.

func record(id string, props map[string]string) string {
	p := map[string]string{"hs_object_id": id, "createdate": "2024-01-01T00:00:00.000Z"}
	maps.Copy(p, props)
	raw, err := json.Marshal(map[string]any{
		"id": id, "properties": p, "createdAt": "2024-01-01T00:00:00.000Z", "updatedAt": "2024-02-01T00:00:00.000Z", "archived": false,
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func batch(records ...string) string {
	return `{"status": "COMPLETE", "results": [` + strings.Join(records, ",") +
		`], "startedAt": "2024-03-01T00:00:00.000Z", "completedAt": "2024-03-01T00:00:01.000Z"}`
}

// assocType is the association type the corpus's associations carry.
const assocType = `{"category": "HUBSPOT_DEFINED", "typeId": 280, "label": "Primary"}`

// assocRow is one associations batch read result: from to each id, with the
// next page's cursor when after is not empty.
func assocRow(from, after string, to ...string) string {
	tos := make([]string, 0, len(to))
	for _, id := range to {
		tos = append(tos, `{"toObjectId": `+id+`, "associationTypes": [`+assocType+`]}`)
	}
	row := `{"from": {"id": "` + from + `"}, "to": [` + strings.Join(tos, ",") + `]`
	if after != "" {
		row += `, "paging": {"next": {"after": "` + after + `", "link": "https://api.hubapi.com/crm/associations/2026-09/x/y/batch/read?after=` + after + `"}}`
	}
	return row + `}`
}

func assocs(rows ...string) string { return batch(rows...) }

func memberships(after string, ids ...string) string {
	results := make([]string, 0, len(ids))
	for _, id := range ids {
		results = append(results, `{"recordId": "`+id+`", "membershipTimestamp": "2024-01-02T00:00:00.000Z"}`)
	}
	body := `{"results": [` + strings.Join(results, ",") + `]`
	if after != "" {
		body += `, "paging": {"next": {"after": "` + after + `", "link": "https://api.hubapi.com/crm/lists/2026-09/7/memberships?after=` + after + `"}}`
	}
	return body + `, "total": 2}`
}

func searchPage(total int, after string, records ...string) string {
	body := fmt.Sprintf(`{"total": %d, "results": [%s]`, total, strings.Join(records, ","))
	if after != "" {
		body += `, "paging": {"next": {"after": "` + after + `", "link": "https://api.hubapi.com/crm/objects/2026-09/contacts/search?after=` + after + `"}}`
	}
	return body + `}`
}

func rateLimited(policy string) string {
	return `{"status": "error", "message": "You have reached your ` + strings.ToLower(policy) + ` limit.", "errorType": "RATE_LIMIT",
		"correlationId": "c033cdaa-2c40-4a64-ae48-b4cec88dad24", "policyName": "` + policy + `", "groupName": "api"}`
}

func errorBody(category, message string) string {
	return `{"status": "error", "message": "` + message + `", "correlationId": "a43683b0-5717-4ceb-80b4-104d02915d8c", "category": "` + category + `"}`
}

// Keys: the method, the path, and what selects the page: the input ids (and
// their after) of a batch read, the after of a GET or a search.
func readKey(objectType string, ids ...string) string {
	return "POST /crm/objects/2026-09/" + objectType + "/batch/read " + strings.Join(ids, ",")
}

func assocKey(from, to string, inputs ...string) string {
	return "POST /crm/associations/2026-09/" + from + "/" + to + "/batch/read " + strings.Join(inputs, ",")
}

func listKey(id string) string { return "GET /crm/lists/2026-09/" + id + " " }

// membersKey is the key of list 7's memberships page at after.
func membersKey(after string) string { return "GET /crm/lists/2026-09/7/memberships " + after }

// searchKey is the key of the contacts search page at after.
func searchKey(after string) string { return "POST /crm/objects/2026-09/contacts/search " + after }

// The corpus's search: contacts whose lifecyclestage is customer.
const customerFilter = `[{"filters": [{"propertyName": "lifecyclestage", "operator": "EQ", "value": "customer"}]}]`

// answer is one recorded response.
type answer struct {
	status  int
	headers map[string]string
	body    string
}

func ok(body string) answer { return answer{status: http.StatusOK, body: body} }

// corpus returns the recorded answers keyed by key. A named company 101 is
// associated with one record of each of the nine types, its contacts over two
// pages; a named contact 201 and deal 301; list 7 holds contacts 102 (whose id
// a company also has) and 203 over two membership pages; the search returns
// contacts 204 and 205 over two pages. An associations read the corpus does
// not carry answers no associations.
func corpus() map[string][]answer {
	return map[string][]answer{
		readKey("companies", "101"): {ok(batch(record("101", map[string]string{"name": "Acme", "domain": "acme.example"})))},
		readKey("contacts", "201"):  {ok(batch(record("201", map[string]string{"firstname": "Ada", "lastname": "Lovelace", "email": "ada@acme.example"})))},
		readKey("deals", "301"):     {ok(batch(record("301", map[string]string{"dealname": "Big Deal", "amount": "1000"})))},

		listKey("7"):      {ok(`{"list": {"listId": "7", "name": "Customers", "objectTypeId": "0-1", "processingType": "MANUAL", "size": 2}}`)},
		membersKey(""):    {ok(memberships("CAE", "102"))},
		membersKey("CAE"): {ok(memberships("", "203"))},
		readKey("contacts", "102", "203"): {ok(batch(
			record("102", map[string]string{"firstname": "Grace", "lastname": "Hopper"}),
			record("203", map[string]string{"firstname": "Alan", "lastname": "Turing"})))},

		searchKey(""):  {ok(searchPage(2, "1", record("204", map[string]string{"firstname": "Barbara", "lifecyclestage": "customer"})))},
		searchKey("1"): {ok(searchPage(2, "", record("205", map[string]string{"firstname": "Edsger", "lifecyclestage": "customer"})))},

		assocKey("companies", "contacts", "101"):      {ok(assocs(assocRow("101", "MjAy", "201")))},
		assocKey("companies", "contacts", "101@MjAy"): {ok(assocs(assocRow("101", "", "202")))},
		assocKey("companies", "companies", "101"):     {ok(assocs(assocRow("101", "", "102")))},
		assocKey("companies", "deals", "101"):         {ok(assocs(assocRow("101", "", "301")))},
		assocKey("companies", "tickets", "101"):       {ok(assocs(assocRow("101", "", "401")))},
		assocKey("companies", "calls", "101"):         {ok(assocs(assocRow("101", "", "501")))},
		assocKey("companies", "emails", "101"):        {ok(assocs(assocRow("101", "", "601")))},
		assocKey("companies", "meetings", "101"):      {ok(assocs(assocRow("101", "", "701")))},
		assocKey("companies", "notes", "101"):         {ok(assocs(assocRow("101", "", "801")))},
		assocKey("companies", "tasks", "101"):         {ok(assocs(assocRow("101", "", "901")))},
		assocKey("deals", "contacts", "301"):          {ok(assocs(assocRow("301", "", "201")))},

		readKey("contacts", "202"):  {ok(batch(record("202", map[string]string{"firstname": "Linus"})))},
		readKey("companies", "102"): {ok(batch(record("102", map[string]string{"name": "Initech"})))},
		readKey("tickets", "401"):   {ok(batch(record("401", map[string]string{"subject": "Login fails", "content": "Cannot sign in"})))},
		readKey("calls", "501"):     {ok(batch(record("501", map[string]string{"hs_call_title": "Kickoff", "hs_call_body": "Discussed scope"})))},
		readKey("emails", "601"):    {ok(batch(record("601", map[string]string{"hs_email_subject": "Proposal", "hs_email_text": "Attached"})))},
		readKey("meetings", "701"):  {ok(batch(record("701", map[string]string{"hs_meeting_title": "Review", "hs_meeting_body": "Quarterly review"})))},
		readKey("notes", "801"):     {ok(batch(record("801", map[string]string{"hs_note_body": "Prefers email"})))},
		readKey("tasks", "901"):     {ok(batch(record("901", map[string]string{"hs_task_subject": "Follow up", "hs_task_body": "Call back"})))},
	}
}

// fullParams names every target of the corpus.
func fullParams(t *testing.T) walk.Params {
	t.Helper()
	var groups []map[string]any
	if err := json.Unmarshal([]byte(customerFilter), &groups); err != nil {
		t.Fatal(err)
	}
	return walk.Params{
		Companies: []string{"101"}, Contacts: []string{"201"}, Deals: []string{"301"}, Lists: []string{"7"},
		Searches: []walk.Search{{ObjectType: "contacts", FilterGroups: groups}},
	}
}

// recorded is one request the fixture served.
type recorded struct {
	key   string
	auth  string
	query string
	body  map[string]any
}

// fixture is a real HTTP server standing in for the HubSpot API. Each key
// serves its answers in order and repeats the last one; an associations read
// it does not carry answers no associations, any other key 404.
type fixture struct {
	server   *httptest.Server
	mu       sync.Mutex
	routes   map[string][]answer
	served   map[string]int
	requests []recorded
	waits    []time.Duration
	paces    []time.Duration
	onWait   func()
}

func newFixture(t *testing.T, routes map[string][]answer) *fixture {
	t.Helper()
	f := &fixture{routes: routes, served: map[string]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

// selectorOf is what selects a request's page: batch inputs, or after.
func selectorOf(r *http.Request, body map[string]any) string {
	if inputs, ok := body["inputs"].([]any); ok {
		ids := make([]string, 0, len(inputs))
		for _, in := range inputs {
			m, _ := in.(map[string]any)
			id, _ := m["id"].(string)
			if after, _ := m["after"].(string); after != "" {
				id += "@" + after
			}
			ids = append(ids, id)
		}
		return strings.Join(ids, ",")
	}
	if after, ok := body["after"].(string); ok {
		return after
	}
	return r.URL.Query().Get("after")
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body) //nolint:errcheck // a body that does not read is recorded empty
	var body map[string]any
	_ = json.Unmarshal(raw, &body) //nolint:errcheck // a GET has no body
	k := r.Method + " " + r.URL.Path + " " + selectorOf(r, body)
	f.mu.Lock()
	f.requests = append(f.requests, recorded{key: k, auth: r.Header.Get("Authorization"), query: r.URL.RawQuery, body: body})
	answers, found := f.routes[k]
	n := f.served[k]
	f.served[k] = n + 1
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	if !found {
		if strings.HasPrefix(r.URL.Path, "/crm/associations/") {
			fmt.Fprint(w, assocs())
			return
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, errorBody("OBJECT_NOT_FOUND", "resource not found"))
		return
	}
	a := answers[min(n, len(answers)-1)]
	for h, v := range a.headers {
		w.Header().Set(h, v)
	}
	w.WriteHeader(a.status)
	fmt.Fprint(w, a.body)
}

func (f *fixture) count(k string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.served[k]
}

// requestsWith is every recorded request whose key starts with prefix.
func (f *fixture) requestsWith(prefix string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.requests {
		if strings.HasPrefix(r.key, prefix) {
			out = append(out, r)
		}
	}
	return out
}

func (f *fixture) collector() walk.Collector {
	frozen := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	return walk.Collector{Client: func(token string) *hubapi.Client {
		c := hubapi.NewAt(f.server.URL, f.server.Client(), token)
		c.Now = func() time.Time { return frozen }
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
		c.Pace = func(ctx context.Context, d time.Duration) error {
			f.mu.Lock()
			f.paces = append(f.paces, d)
			f.mu.Unlock()
			return ctx.Err()
		}
		return c
	}}
}

func (f *fixture) walkWith(ctx context.Context, t *testing.T, p walk.Params) (framework.Result, error) {
	t.Helper()
	t.Setenv(walk.TokenVariable, fixtureToken)
	return f.collector().Walk(ctx, "acme", p, framework.ForeignContext{})
}

func (f *fixture) mustWalk(t *testing.T, p walk.Params) framework.Result {
	t.Helper()
	res, err := f.walkWith(t.Context(), t, p)
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}
	return res
}

func withRoute(k string, answers ...answer) map[string][]answer {
	routes := corpus()
	routes[k] = answers
	return routes
}

func nodesOfType(res framework.Result, kind string) map[string]framework.Node {
	out := map[string]framework.Node{}
	for _, n := range res.Nodes {
		if n.Type == kind {
			out[n.ID] = n
		}
	}
	return out
}

func edges(res framework.Result, kind string) map[string]framework.Edge {
	out := map[string]framework.Edge{}
	for _, e := range res.Edges {
		if e.Type == kind {
			out[e.FromID+" -> "+e.ToID] = e
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
