// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/notion/internal/walk"
)

const (
	filterDone  = `{"property": "Status", "select": {"equals": "Done"}}`
	filterDoing = `{"property": "Status", "select": {"equals": "Doing"}}`
)

// filterOf decodes raw into the filter a param carries and returns it with
// its JSON encoding, which is what the fixture routes a filtered POST by.
func filterOf(t *testing.T, raw string) (map[string]any, string) {
	t.Helper()
	var filter map[string]any
	if err := json.Unmarshal([]byte(raw), &filter); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	return filter, string(encoded)
}

// hitSourceRoutes serves data source sourceHit: retrieving it, and a query of
// it that answers rowOne under filterDone, rowTwo under filterDoing, and both
// unfiltered.
func hitSourceRoutes(t *testing.T) map[string][]answer {
	t.Helper()
	routes := corpus()
	_, done := filterOf(t, filterDone)
	_, doing := filterOf(t, filterDoing)
	routes[dataSourceKey(sourceHit)] = []answer{ok(fmt.Sprintf(`{"object": "data_source", "id": %q, "title": %s,
		"parent": {"type": "database_id", "database_id": %q}}`, sourceHit, rich("Roadmap tracker"), databaseTasks))}
	routes[filteredKey(queryKey(sourceHit, ""), done)] = []answer{ok(listOf("page_or_data_source", "",
		row(rowOne, "Done task", sourceHit, "2022-03-01T10:00:00.000Z")))}
	routes[filteredKey(queryKey(sourceHit, ""), doing)] = []answer{ok(listOf("page_or_data_source", "",
		row(rowTwo, "Doing task", sourceHit, "2022-03-01T11:00:00.000Z")))}
	routes[queryKey(sourceHit, "")] = []answer{ok(listOf("page_or_data_source", "",
		row(rowOne, "Done task", sourceHit, "2022-03-01T10:00:00.000Z"),
		row(rowTwo, "Doing task", sourceHit, "2022-03-01T11:00:00.000Z")))}
	return routes
}

func wantEntries(t *testing.T, f *fixture, p walk.Params, wantQueries int) {
	t.Helper()
	res := f.mustWalk(t, p)
	if !res.Complete.IsComplete() {
		t.Fatalf("the walk reported incomplete: %s", res.Complete.Reason())
	}
	pages := nodesOfType(res, walk.NodePage)
	for _, id := range []string{rowOne, rowTwo} {
		if _, read := pages[walk.PageID(id)]; !read {
			t.Errorf("entry %s was not emitted; emitted %v", id, sortedKeys(pages))
		}
	}
	if n := len(f.requestsWith("POST /data_sources/" + sourceHit)); n != wantQueries {
		t.Errorf("data source %s was queried %d times, want %d", sourceHit, n, wantQueries)
	}
}

// TestTheSameDataSourceUnderTwoFiltersReadsBoth: each filter's query runs and
// the entries either selects are emitted.
func TestTheSameDataSourceUnderTwoFiltersReadsBoth(t *testing.T) {
	done, _ := filterOf(t, filterDone)
	doing, _ := filterOf(t, filterDoing)
	f := newFixture(t, hitSourceRoutes(t))
	wantEntries(t, f, walk.Params{DataSources: []walk.DataSourceQuery{{ID: sourceHit, Filter: done}, {ID: sourceHit, Filter: doing}}}, 2)
}

// TestAFilteredDataSourceReachedAgainBySearchIsReadUnfiltered: a title search
// hit on a data source the collect queried with a filter queries it again
// with no filter, and every entry is emitted.
func TestAFilteredDataSourceReachedAgainBySearchIsReadUnfiltered(t *testing.T) {
	done, _ := filterOf(t, filterDone)
	f := newFixture(t, hitSourceRoutes(t))
	wantEntries(t, f, walk.Params{DataSources: []walk.DataSourceQuery{{ID: sourceHit, Filter: done}}, TitleSearch: []string{"roadmap"}}, 2)
	unfiltered := 0
	for _, r := range f.requestsWith("POST /data_sources/" + sourceHit) {
		if _, filtered := r.body["filter"]; !filtered {
			unfiltered++
		}
	}
	if unfiltered != 1 {
		t.Errorf("got %d unfiltered queries of the search hit, want 1", unfiltered)
	}
}

// TestASelectorRepeatedOrCoveredByAnUnfilteredReadIsNotQueriedAgain: the same
// filter twice is one query, and a filtered selector after the data source was
// read unfiltered makes no query.
func TestASelectorRepeatedOrCoveredByAnUnfilteredReadIsNotQueriedAgain(t *testing.T) {
	done, _ := filterOf(t, filterDone)
	again, _ := filterOf(t, filterDone)
	f := newFixture(t, hitSourceRoutes(t))
	f.mustWalk(t, walk.Params{DataSources: []walk.DataSourceQuery{{ID: sourceHit, Filter: done}, {ID: sourceHit, Filter: again}}})
	if n := len(f.requestsWith("POST /data_sources/" + sourceHit)); n != 1 {
		t.Errorf("the same filter twice made %d queries, want 1", n)
	}

	f2 := newFixture(t, hitSourceRoutes(t))
	wantEntries(t, f2, walk.Params{TitleSearch: []string{"roadmap"}, DataSources: []walk.DataSourceQuery{{ID: sourceArchive, Filter: done}},
		Databases: []string{databaseTasks}}, 1)
	if n := len(f2.requestsWith("POST /data_sources/" + sourceArchive)); n != 1 {
		t.Errorf("a filtered selector after the unfiltered database read made %d queries of the data source in all, want 1", n)
	}
}

// TestAFilterThatDoesNotEncodeIsRefusedNamingItsParam.
func TestAFilterThatDoesNotEncodeIsRefusedNamingItsParam(t *testing.T) {
	f := newFixture(t, hitSourceRoutes(t))
	_, err := f.walkWith(t.Context(), t, walk.Params{DataSources: []walk.DataSourceQuery{{ID: sourceHit, Filter: map[string]any{"number": math.Inf(1)}}}})
	if err == nil || !strings.Contains(err.Error(), "params.data_sources[0].filter") || !strings.Contains(err.Error(), "does not encode as JSON") {
		t.Fatalf("got %v, want an error naming params.data_sources[0].filter", err)
	}
	if n := len(f.requestsWith("POST /data_sources/" + sourceHit)); n != 0 {
		t.Errorf("an unencodable filter made %d queries", n)
	}
}
