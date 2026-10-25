// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/notion/internal/notionapi"
	"github.com/fulminate-io/knowledge-contrib/notion/internal/walk"
)

// fixtureToken is the token every fixture walk runs with.
const fixtureToken = "ntn_fixture/SECRET+value=0123456789"

// The corpus's ids. The shapes of every body below are transcribed from the
// Notion API reference pages for retrieve a page, retrieve block children,
// retrieve a database, query a data source, search and list comments.
const (
	pageRoadmap   = "59833787-2cf9-4fdf-8782-e53db20768a5"
	pageSub       = "4e2f1a2b-1111-4c3d-9e8f-0123456789ab"
	pageHit       = "7a1d3c55-2222-4b6e-8d9f-abcdefabcdef"
	blockKale     = "c02fc1d3-db8b-45c5-a222-27595b15aea7"
	blockPlan     = "a1b2c3d4-0001-4000-8000-000000000001"
	blockForm     = "a1b2c3d4-0002-4000-8000-000000000002"
	blockImage    = "a1b2c3d4-0003-4000-8000-000000000003"
	blockMark     = "a1b2c3d4-0004-4000-8000-000000000004"
	blockTodo     = "a1b2c3d4-0005-4000-8000-000000000005"
	blockPhoto    = "a1b2c3d4-0006-4000-8000-000000000006"
	blockNested1  = "a1b2c3d4-0007-4000-8000-000000000007"
	blockNested2  = "a1b2c3d4-0008-4000-8000-000000000008"
	blockSubText  = "a1b2c3d4-0009-4000-8000-000000000009"
	blockHitText  = "a1b2c3d4-000a-4000-8000-00000000000a"
	databaseTasks = "bc1211ca-e3f1-4939-ae34-5260b16f627c"
	sourceTasks   = "248104cd-477e-80af-bc30-000bd28de8f9"
	sourceArchive = "248104cd-477e-80af-bc30-000bd28de8fa"
	sourceHit     = "248104cd-477e-80af-bc30-000bd28de8fb"
	rowOne        = "e1000000-0000-4000-8000-000000000001"
	rowTwo        = "e1000000-0000-4000-8000-000000000002"
	rowThree      = "e1000000-0000-4000-8000-000000000003"
	rowFour       = "e1000000-0000-4000-8000-000000000004"
	commentOne    = "94cc56ab-9f02-409d-9f99-1037e9fe502f"
	commentTwo    = "94cc56ab-9f02-409d-9f99-1037e9fe5030"
	commentNested = "94cc56ab-9f02-409d-9f99-1037e9fe5031"
	signedURL     = "https://prod-files-secure.s3.us-west-2.amazonaws.com/abc/diagram.png?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=deadbeef"
	externalPhoto = "https://example.com/photo.png"
	externalSpec  = "https://example.com/spec"
)

func rich(text string) string {
	return fmt.Sprintf(`[{"type": "text", "text": {"content": %q, "link": null}, "annotations": {"bold": false, "italic": false,
		"strikethrough": false, "underline": false, "code": false, "color": "default"}, "plain_text": %q, "href": null}]`, text, text)
}

func block(id, kind string, hasChildren bool, content string) string {
	return fmt.Sprintf(`{"object": "block", "id": %q, "parent": {"type": "page_id", "page_id": %q},
		"created_time": "2022-03-01T19:05:00.000Z", "last_edited_time": "2022-03-01T19:05:00.000Z",
		"created_by": {"object": "user", "id": "ee5f0f84-409a-440f-983a-a5315961c6e4"},
		"has_children": %t, "archived": false, "in_trash": false, "type": %q, %q: %s}`,
		id, pageRoadmap, hasChildren, kind, kind, content)
}

func paragraph(id, text string, hasChildren bool) string {
	return block(id, "paragraph", hasChildren, `{"rich_text": `+rich(text)+`, "color": "default"}`)
}

func blockList(next string, blocks ...string) string {
	return listOf("block", next, blocks...)
}

func listOf(kind, next string, results ...string) string {
	cursor := "null"
	if next != "" {
		cursor = fmt.Sprintf("%q", next)
	}
	return fmt.Sprintf(`{"object": "list", "results": [%s], "next_cursor": %s, "has_more": %t, "type": %q, %q: {}}`,
		strings.Join(results, ","), cursor, next != "", kind, kind)
}

func page(id, title, parent, created string) string {
	return fmt.Sprintf(`{"object": "page", "id": %q, "created_time": %q, "last_edited_time": "2022-03-02T00:00:00.000Z",
		"created_by": {"object": "user", "id": "ee5f0f84-409a-440f-983a-a5315961c6e4"},
		"last_edited_by": {"object": "user", "id": "ee5f0f84-409a-440f-983a-a5315961c6e4"},
		"cover": null, "icon": null, "parent": %s, "archived": false, "in_trash": false,
		"properties": {"Name": {"id": "title", "type": "title", "title": %s}},
		"url": "https://www.notion.so/%s", "public_url": null}`,
		id, created, parent, rich(title), strings.ReplaceAll(id, "-", ""))
}

func row(id, title, source, created string) string {
	return fmt.Sprintf(`{"object": "page", "id": %q, "created_time": %q, "last_edited_time": "2022-03-02T00:00:00.000Z",
		"created_by": {"object": "user", "id": "ee5f0f84-409a-440f-983a-a5315961c6e4"},
		"parent": {"type": "data_source_id", "data_source_id": %q, "database_id": %q}, "archived": false, "in_trash": false,
		"properties": {
			"Name": {"id": "title", "type": "title", "title": %s},
			"Status": {"id": "%%3AVhm", "type": "select", "select": {"id": "c3", "name": "Done", "color": "green"}},
			"Attachment": {"id": "files", "type": "files", "files": [
				{"name": "spec.pdf", "type": "file", "file": {"url": %q, "expiry_time": "2022-03-02T01:00:00.000Z"}},
				{"name": "link", "type": "external", "external": {"url": %q}}]}},
		"url": "https://www.notion.so/%s"}`,
		id, created, source, databaseTasks, rich(title), signedURL, externalSpec, strings.ReplaceAll(id, "-", ""))
}

func comment(id, parentType, parentID, text string) string {
	return fmt.Sprintf(`{"object": "comment", "id": %q, "parent": {"type": %q, %q: %q},
		"discussion_id": "f1407351-36f5-4c49-a13c-49f8ba11776d", "created_time": "2022-07-15T16:52:00.000Z",
		"last_edited_time": "2022-07-15T19:16:00.000Z", "created_by": {"object": "user", "id": "9b15170a-9941-4297-8ee6-83fa7649a87a"},
		"rich_text": %s}`, id, parentType, parentType, parentID, rich(text))
}

func pageParent(id string) string { return fmt.Sprintf(`{"type": "page_id", "page_id": %q}`, id) }

func errorBody(status int, code, message string) string {
	return fmt.Sprintf(`{"object": "error", "status": %d, "code": %q, "message": %q, "request_id": "d1b7c1f2"}`, status, code, message)
}

// Keys: the method, the path and the cursor.
func key(method, path, cursor string) string { return method + " " + path + " cursor=" + cursor }

func pageKey(id string) string              { return key("GET", "/pages/"+id, "") }
func blocksKey(id, cursor string) string    { return key("GET", "/blocks/"+id+"/children", cursor) }
func commentsKey(id, cursor string) string  { return key("GET", "/comments block_id="+id, cursor) }
func databaseKey(id string) string          { return key("GET", "/databases/"+id, "") }
func dataSourceKey(id string) string        { return key("GET", "/data_sources/"+id, "") }
func queryKey(id, cursor string) string     { return key("POST", "/data_sources/"+id+"/query", cursor) }
func searchKey(query, cursor string) string { return key("POST", "/search query="+query, cursor) }

// filteredKey is the key of a POST under k whose body's filter encodes as
// filterJSON. A filtered POST is served from it when the routes carry it, and
// from k otherwise.
func filteredKey(k, filterJSON string) string { return k + " filter=" + filterJSON }

// answer is one recorded response.
type answer struct {
	status     int
	retryAfter string
	body       string
}

func ok(body string) answer { return answer{status: http.StatusOK, body: body} }

func emptyPage(id string, routes map[string][]answer) {
	routes[blocksKey(id, "")] = []answer{ok(blockList(""))}
	routes[commentsKey(id, "")] = []answer{ok(listOf("comment", ""))}
}

// corpus returns the recorded answers keyed by key.
func corpus() map[string][]answer {
	routes := map[string][]answer{
		pageKey(pageRoadmap): {ok(page(pageRoadmap, "Roadmap", `{"type": "workspace", "workspace": true}`, "2022-03-01T19:05:00.000Z"))},
		blocksKey(pageRoadmap, ""): {ok(blockList("c2Vjb25kLXBhZ2U",
			paragraph(blockKale, "Lacinato kale", true),
			block(blockPlan, "heading_1", false, `{"rich_text": `+rich("Plan")+`, "is_toggleable": false, "color": "default"}`),
			fmt.Sprintf(`{"object": "block", "id": %q, "has_children": false, "type": "unsupported", "unsupported": {"block_type": "form"}}`, blockForm),
			block(pageSub, "child_page", true, `{"title": "Subpage"}`),
			block(blockImage, "image", false, `{"caption": `+rich("Diagram")+`, "type": "file", "file": {"url": "`+signedURL+`", "expiry_time": "2022-03-02T01:00:00.000Z"}}`),
			block(blockMark, "bookmark", false, `{"caption": [], "url": "`+externalSpec+`"}`),
		))},
		blocksKey(pageRoadmap, "c2Vjb25kLXBhZ2U"): {ok(blockList("",
			block(blockTodo, "to_do", false, `{"rich_text": `+rich("ship it")+`, "checked": false, "color": "default"}`),
			block(blockPhoto, "image", false, `{"caption": `+rich("Photo")+`, "type": "external", "external": {"url": "`+externalPhoto+`"}}`),
		))},
		blocksKey(blockKale, ""):    {ok(blockList("", paragraph(blockNested1, "nested level 1", true)))},
		blocksKey(blockNested1, ""): {ok(blockList("", paragraph(blockNested2, "nested level 2", false)))},
		commentsKey(pageRoadmap, ""): {ok(listOf("comment", "Y29tbWVudHMtMg",
			comment(commentOne, "page_id", pageRoadmap, "Looks good")))},
		commentsKey(pageRoadmap, "Y29tbWVudHMtMg"): {ok(listOf("comment", "",
			comment(commentTwo, "page_id", pageRoadmap, "Ship it Friday")))},
		commentsKey(blockNested1, ""): {ok(listOf("comment", "",
			comment(commentNested, "block_id", blockNested1, "nested comment")))},

		pageKey(pageSub):       {ok(page(pageSub, "Subpage", pageParent(pageRoadmap), "2022-03-01T19:06:00.000Z"))},
		blocksKey(pageSub, ""): {ok(blockList("", paragraph(blockSubText, "sub text", false)))},
		blocksKey(pageHit, ""): {ok(blockList("", paragraph(blockHitText, "search hit", false)))},

		databaseKey(databaseTasks): {ok(fmt.Sprintf(`{"object": "database", "id": %q, "title": %s,
			"parent": {"type": "page_id", "page_id": %q}, "created_time": "2022-03-01T19:05:00.000Z",
			"data_sources": [{"id": %q, "name": "Tasks"}, {"id": %q, "name": "Archive"}],
			"url": "https://www.notion.so/Tasks-DB"}`, databaseTasks, rich("Tasks DB"), pageRoadmap, sourceTasks, sourceArchive))},
		queryKey(sourceTasks, ""): {ok(listOf("page_or_data_source", "cXVlcnktMg",
			row(rowOne, "Write spec", sourceTasks, "2022-03-01T10:00:00.000Z"),
			row(rowTwo, "Review spec", sourceTasks, "2022-03-01T11:00:00.000Z")))},
		queryKey(sourceTasks, "cXVlcnktMg"): {ok(listOf("page_or_data_source", "",
			row(rowThree, "Ship", sourceTasks, "2022-03-01T12:00:00.000Z")))},
		queryKey(sourceArchive, ""): {ok(listOf("page_or_data_source", "",
			row(rowFour, "Old task", sourceArchive, "2021-01-01T00:00:00.000Z")))},
		dataSourceKey(sourceArchive): {ok(fmt.Sprintf(`{"object": "data_source", "id": %q, "title": %s,
			"parent": {"type": "database_id", "database_id": %q}}`, sourceArchive, rich("Archive"), databaseTasks))},

		searchKey("roadmap", ""): {ok(listOf("page_or_data_source", "c2VhcmNoLTI",
			page(pageHit, "Roadmap notes", `{"type": "workspace", "workspace": true}`, "2022-04-01T00:00:00.000Z")))},
		searchKey("roadmap", "c2VhcmNoLTI"): {ok(listOf("page_or_data_source", "",
			fmt.Sprintf(`{"object": "data_source", "id": %q, "title": %s, "parent": {"type": "database_id", "database_id": %q},
				"database_parent": {"type": "page_id", "page_id": %q}}`, sourceHit, rich("Roadmap tracker"), databaseTasks, pageRoadmap)))},
		queryKey(sourceHit, ""): {ok(listOf("page_or_data_source", ""))},
	}
	// A list whose has_more is false ends there, whatever next_cursor says.
	routes[commentsKey(blockPlan, "")] = []answer{ok(strings.Replace(listOf("comment", ""), `"next_cursor": null`, `"next_cursor": "c3RhbGU"`, 1))}
	for _, id := range []string{blockPlan, blockForm, blockImage, blockMark, blockTodo, blockPhoto, blockKale, blockNested2,
		blockSubText, blockHitText, pageSub, pageHit} {
		if _, present := routes[commentsKey(id, "")]; !present {
			routes[commentsKey(id, "")] = []answer{ok(listOf("comment", ""))}
		}
	}
	for _, id := range []string{rowOne, rowTwo, rowThree, rowFour} {
		emptyPage(id, routes)
	}
	return routes
}

// recorded is one request the fixture served.
type recorded struct {
	key     string
	method  string
	auth    string
	version string
	query   string
	body    map[string]any
}

// fixture is a real HTTP server standing in for the Notion API. Each key
// serves its answers in order and repeats the last one; a key the corpus does
// not carry answers 404 object_not_found.
type fixture struct {
	server   *httptest.Server
	mu       sync.Mutex
	routes   map[string][]answer
	served   map[string]int
	requests []recorded
	waits    []time.Duration
	onWait   func()
	// onRequest, when set, runs after each request is recorded with the
	// number of requests recorded so far.
	onRequest func(n int)
}

func newFixture(t *testing.T, routes map[string][]answer) *fixture {
	t.Helper()
	f := &fixture{routes: routes, served: map[string]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body) //nolint:errcheck // a body that does not read is recorded empty
	var body map[string]any
	_ = json.Unmarshal(raw, &body) //nolint:errcheck // a GET has no body
	q := r.URL.Query()
	cursor := q.Get("start_cursor")
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost:
		cursor, _ = body["start_cursor"].(string)
		if path == "/search" {
			query, _ := body["query"].(string)
			path += " query=" + query
		}
	case path == "/comments":
		path += " block_id=" + q.Get("block_id")
	}
	k := key(r.Method, path, cursor)
	if filter, filtered := body["filter"]; filtered && r.Method == http.MethodPost {
		encoded, err := json.Marshal(filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		f.mu.Lock()
		_, routed := f.routes[filteredKey(k, string(encoded))]
		f.mu.Unlock()
		if routed {
			k = filteredKey(k, string(encoded))
		}
	}
	f.mu.Lock()
	f.requests = append(f.requests, recorded{key: k, method: r.Method, auth: r.Header.Get("Authorization"),
		version: r.Header.Get("Notion-Version"), query: r.URL.RawQuery, body: body})
	answers, found := f.routes[k]
	n := f.served[k]
	f.served[k] = n + 1
	hook, recordedCount := f.onRequest, len(f.requests)
	f.mu.Unlock()
	if hook != nil {
		hook(recordedCount)
	}
	w.Header().Set("Content-Type", "application/json")
	if !found {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, errorBody(http.StatusNotFound, "object_not_found", "Could not find the object."))
		return
	}
	a := answers[min(n, len(answers)-1)]
	if a.retryAfter != "" {
		w.Header().Set("Retry-After", a.retryAfter)
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
	return walk.Collector{Client: func(token string) *notionapi.Client {
		c := notionapi.NewAt(f.server.URL, f.server.Client(), token)
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

// fullParams names every target of the corpus.
func fullParams() walk.Params {
	return walk.Params{Pages: []string{pageRoadmap}, Descendants: true, Databases: []string{databaseTasks}, TitleSearch: []string{"roadmap"}}
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

func edgesOfType(res framework.Result, kind string) map[string]bool {
	out := map[string]bool{}
	for _, e := range res.Edges {
		if e.Type == kind {
			out[e.FromID+" -> "+e.ToID] = true
		}
	}
	return out
}
