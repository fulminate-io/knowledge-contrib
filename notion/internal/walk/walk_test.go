// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/notion/internal/walk"
)

func sortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }

// roadmapText is the Roadmap page's block text in document order, assembled
// here from the corpus's blocks: nested blocks follow their parent, a
// Notion-hosted image keeps only its caption, an external one its caption and
// URL.
var roadmapText = strings.Join([]string{
	"Lacinato kale", "nested level 1", "nested level 2", "Plan", "Subpage", "Diagram", externalSpec,
	"ship it", "Photo " + externalPhoto,
}, "\n")

// TestAFullFixtureWalkEmitsTheWholeCorpusAndIsComplete.
func TestAFullFixtureWalkEmitsTheWholeCorpusAndIsComplete(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, fullParams())
	if !res.Complete.IsComplete() {
		t.Fatalf("a clean fixture walk reported incomplete: %s", res.Complete.Reason())
	}

	pages := nodesOfType(res, walk.NodePage)
	var wantPages []string
	for _, id := range []string{pageRoadmap, pageSub, pageHit, rowOne, rowTwo, rowThree, rowFour} {
		wantPages = append(wantPages, "notion:page:"+id)
	}
	slices.Sort(wantPages)
	if got := sortedKeys(pages); !slices.Equal(got, wantPages) {
		t.Errorf("page nodes:\n got %v\nwant %v", got, wantPages)
	}
	if got := pages["notion:page:"+pageRoadmap].Content; got != roadmapText {
		t.Errorf("the Roadmap page's content:\n got %q\nwant %q", got, roadmapText)
	}
	if got := pages["notion:page:"+pageRoadmap].Metadata["unsupported_blocks"]; got != "form" {
		t.Errorf("unsupported_blocks is %q, want form", got)
	}
	wantComments := []string{"notion:comment:" + commentOne, "notion:comment:" + commentTwo, "notion:comment:" + commentNested}
	slices.Sort(wantComments)
	if got := sortedKeys(nodesOfType(res, walk.NodeComment)); !slices.Equal(got, wantComments) {
		t.Errorf("comment nodes: got %v, want %v", got, wantComments)
	}
	wantSources := []string{"notion:data-source:" + sourceTasks, "notion:data-source:" + sourceArchive, "notion:data-source:" + sourceHit}
	slices.Sort(wantSources)
	if got := sortedKeys(nodesOfType(res, walk.NodeDataSource)); !slices.Equal(got, wantSources) {
		t.Errorf("data source nodes: got %v, want %v", got, wantSources)
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

// TestEveryPageOfEveryListIsRequestedOnceWithItsCursor: block children and
// comments carry start_cursor in the query string, the query and search in
// the JSON body, and each page is requested exactly once.
func TestEveryPageOfEveryListIsRequestedOnceWithItsCursor(t *testing.T) {
	f := newFixture(t, corpus())
	f.mustWalk(t, fullParams())
	for _, k := range []string{
		blocksKey(pageRoadmap, ""), blocksKey(pageRoadmap, "c2Vjb25kLXBhZ2U"),
		commentsKey(pageRoadmap, ""), commentsKey(pageRoadmap, "Y29tbWVudHMtMg"),
		queryKey(sourceTasks, ""), queryKey(sourceTasks, "cXVlcnktMg"),
		searchKey("roadmap", ""), searchKey("roadmap", "c2VhcmNoLTI"), commentsKey(blockPlan, ""),
	} {
		if n := f.count(k); n != 1 {
			t.Errorf("%s was requested %d times, want 1", k, n)
		}
	}
	if n := f.count(commentsKey(blockPlan, "c3RhbGU")); n != 0 {
		t.Errorf("a next_cursor beside has_more false was followed %d times", n)
	}
	for _, r := range f.requestsWith("GET /blocks/" + pageRoadmap) {
		if strings.Contains(r.key, "c2Vjb25kLXBhZ2U") && !strings.Contains(r.query, "start_cursor=c2Vjb25kLXBhZ2U") {
			t.Errorf("the second block page's cursor did not ride the query string: %s", r.query)
		}
	}
	for _, r := range f.requestsWith("POST /data_sources/" + sourceTasks) {
		if r.query != "" {
			t.Errorf("a query POST carried a query string %q", r.query)
		}
		if size, _ := r.body["page_size"].(float64); size != 100 {
			t.Errorf("a query POST carried page_size %v, want 100", r.body["page_size"])
		}
	}
}

// TestDescendantsDecidesWhetherChildPagesAreRead.
func TestDescendantsDecidesWhetherChildPagesAreRead(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, walk.Params{Pages: []string{pageRoadmap}})
	if n := len(f.requestsWith("GET /pages/"+pageSub)) + len(f.requestsWith("GET /blocks/"+pageSub)); n != 0 {
		t.Errorf("descendants false made %d requests for the child page", n)
	}
	if _, read := nodesOfType(res, walk.NodePage)["notion:page:"+pageSub]; read {
		t.Errorf("descendants false emitted the child page")
	}

	f2 := newFixture(t, corpus())
	res2 := f2.mustWalk(t, walk.Params{Pages: []string{pageRoadmap}, Descendants: true})
	if !edgesOfType(res2, walk.EdgeChildOf)["notion:page:"+pageSub+" -> notion:page:"+pageRoadmap] {
		t.Errorf("descendants true emitted no CHILD_OF from the child page: %v", sortedKeys(edgesOfType(res2, walk.EdgeChildOf)))
	}
	if got := nodesOfType(res2, walk.NodePage)["notion:page:"+pageSub].Content; got != "sub text" {
		t.Errorf("the child page's content is %q, want %q", got, "sub text")
	}
}

// TestADatabaseYieldsItsDataSourcesAndEveryEntry: every data source belongs
// to the database and every entry to its data source.
func TestADatabaseYieldsItsDataSourcesAndEveryEntry(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, walk.Params{Databases: []string{databaseTasks}})
	belongs := edgesOfType(res, walk.EdgeBelongsTo)
	for _, want := range []string{
		"notion:data-source:" + sourceTasks + " -> notion:database:" + databaseTasks,
		"notion:data-source:" + sourceArchive + " -> notion:database:" + databaseTasks,
		"notion:page:" + rowOne + " -> notion:data-source:" + sourceTasks,
		"notion:page:" + rowThree + " -> notion:data-source:" + sourceTasks,
		"notion:page:" + rowFour + " -> notion:data-source:" + sourceArchive,
	} {
		if !belongs[want] {
			t.Errorf("no BELONGS_TO %s", want)
		}
	}
	entry := nodesOfType(res, walk.NodePage)["notion:page:"+rowOne]
	if entry.SymbolName != "Write spec" || entry.Metadata["property:Status"] != "Done" {
		t.Errorf("the entry carries title %q and Status %q, want Write spec and Done", entry.SymbolName, entry.Metadata["property:Status"])
	}
}

// TestATitleSearchReadsPageHitsAndQueriesDataSourceHits.
func TestATitleSearchReadsPageHitsAndQueriesDataSourceHits(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, walk.Params{TitleSearch: []string{"roadmap"}})
	if got := nodesOfType(res, walk.NodePage)["notion:page:"+pageHit].Content; got != "search hit" {
		t.Errorf("the page hit's content is %q, want %q", got, "search hit")
	}
	if n := f.count(queryKey(sourceHit, "")); n != 1 {
		t.Errorf("the data source hit was queried %d times, want 1", n)
	}
}

// TestEveryRequestCarriesTheNotionVersion.
func TestEveryRequestCarriesTheNotionVersion(t *testing.T) {
	f := newFixture(t, corpus())
	f.mustWalk(t, fullParams())
	for _, r := range f.requestsWith("") {
		if r.version != "2026-03-11" {
			t.Errorf("%s carried Notion-Version %q", r.key, r.version)
		}
	}
}
