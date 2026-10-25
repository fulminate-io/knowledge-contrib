// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/notion/internal/walk"
)

const (
	sourceCapped = "3c1e0000-0000-4000-8000-0000000000c1"
	cappedA      = "3c1e0000-0000-4000-8000-0000000000a1"
	cappedB      = "3c1e0000-0000-4000-8000-0000000000a2"
	cappedC      = "3c1e0000-0000-4000-8000-0000000000a3"
	cappedD      = "3c1e0000-0000-4000-8000-0000000000a4"
)

// userFilter is the filter the collect names, as JSON so the test compares
// what the wire carries against what the param said.
const userFilter = `{"property": "Status", "select": {"equals": "Done"}}`

func cappedList(next string, capped bool, rows ...string) string {
	body := listOf("page_or_data_source", next, rows...)
	if !capped {
		return body
	}
	return strings.TrimSuffix(body, "}") + `, "request_status": {"type": "incomplete", "incomplete_reason": "query_result_limit_reached"}}`
}

func cappedRoutes(answers ...answer) map[string][]answer {
	routes := corpus()
	routes[dataSourceKey(sourceCapped)] = []answer{ok(fmt.Sprintf(`{"object": "data_source", "id": %q, "title": %s,
		"parent": {"type": "database_id", "database_id": %q}}`, sourceCapped, rich("Capped"), databaseTasks))}
	routes[queryKey(sourceCapped, "")] = answers
	for _, id := range []string{cappedA, cappedB, cappedC, cappedD} {
		emptyPage(id, routes)
	}
	return routes
}

func cappedParams(t *testing.T) walk.Params {
	t.Helper()
	var filter map[string]any
	if err := json.Unmarshal([]byte(userFilter), &filter); err != nil {
		t.Fatal(err)
	}
	return walk.Params{DataSources: []walk.DataSourceQuery{{ID: sourceCapped, Filter: filter}}}
}

func decoded(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return v
}

// TestACappedQueryIsWindowedByCreatedTimeUntilEveryEntryIsRead: every query
// sorts by created_time ascending; the first carries the collect's filter
// alone; the cap signal on a page before the last starts a second window that
// and-combines the filter with created_time on or after the last entry read;
// every entry is read once and the walk is complete.
func TestACappedQueryIsWindowedByCreatedTimeUntilEveryEntryIsRead(t *testing.T) {
	routes := cappedRoutes(
		ok(cappedList("d2luZG93LTE", true,
			row(cappedA, "A", sourceCapped, "2023-01-01T00:00:00.000Z"),
			row(cappedB, "B", sourceCapped, "2023-01-02T00:00:00.000Z"))),
		ok(cappedList("", false,
			row(cappedC, "C", sourceCapped, "2023-01-03T00:00:00.000Z"),
			row(cappedD, "D", sourceCapped, "2023-01-04T00:00:00.000Z"))),
	)
	routes[queryKey(sourceCapped, "d2luZG93LTE")] = []answer{ok(cappedList("", false,
		row(cappedC, "C", sourceCapped, "2023-01-03T00:00:00.000Z")))}
	f := newFixture(t, routes)
	res := f.mustWalk(t, cappedParams(t))
	if !res.Complete.IsComplete() {
		t.Fatalf("a windowed walk past the cap reported incomplete: %s", res.Complete.Reason())
	}
	queries := f.requestsWith("POST /data_sources/" + sourceCapped)
	if len(queries) != 3 {
		t.Fatalf("got %d queries, want 3 (two pages of the first window, one of the second)", len(queries))
	}
	sorts := decoded(t, `[{"timestamp": "created_time", "direction": "ascending"}]`)
	for i, q := range queries {
		if !reflect.DeepEqual(q.body["sorts"], sorts) {
			t.Errorf("query %d sorts %v, want %v", i, q.body["sorts"], sorts)
		}
	}
	if want := decoded(t, userFilter); !reflect.DeepEqual(queries[0].body["filter"], want) {
		t.Errorf("the first query's filter is %v, want the collect's filter %v unchanged", queries[0].body["filter"], want)
	}
	window := decoded(t, `{"and": [`+userFilter+`, {"timestamp": "created_time", "created_time": {"on_or_after": "2023-01-03T00:00:00.000Z"}}]}`)
	if !reflect.DeepEqual(queries[2].body["filter"], window) {
		t.Errorf("the second window's filter is %v, want %v", queries[2].body["filter"], window)
	}
	for _, id := range []string{cappedA, cappedB, cappedC, cappedD} {
		if n := f.count(blocksKey(id, "")); n != 1 {
			t.Errorf("entry %s was read %d times, want 1", id, n)
		}
	}
}

// TestACapNoWindowGetsPastMarksTheWalkIncomplete: every remaining entry
// shares one created_time, so the next window returns the same entries.
func TestACapNoWindowGetsPastMarksTheWalkIncomplete(t *testing.T) {
	same := func() answer {
		return ok(cappedList("", true,
			row(cappedA, "A", sourceCapped, "2023-01-01T00:00:00.000Z"),
			row(cappedB, "B", sourceCapped, "2023-01-01T00:00:00.000Z")))
	}
	f := newFixture(t, cappedRoutes(same(), same()))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.onRequest = func(n int) {
		if n >= 50 {
			cancel()
		}
	}
	res, err := f.walkWith(ctx, t, cappedParams(t))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(f.requestsWith("POST /data_sources/" + sourceCapped)); n != 2 {
		t.Errorf("got %d queries, want 2", n)
	}
	if res.Complete.IsComplete() || !strings.Contains(res.Complete.Reason(), "query_result_limit_reached") {
		t.Fatalf("got complete=%v reason %q, want incomplete naming the cap", res.Complete.IsComplete(), res.Complete.Reason())
	}
}

// TestAFilterNotionRefusesFailsTheCollectNamingTheParam.
func TestAFilterNotionRefusesFailsTheCollectNamingTheParam(t *testing.T) {
	f := newFixture(t, cappedRoutes(answer{status: http.StatusBadRequest,
		body: errorBody(400, "validation_error", "Could not find property with name or id: Status.")}))
	_, err := f.walkWith(t.Context(), t, cappedParams(t))
	if err == nil {
		t.Fatalf("got no error, want the collect to fail on the filter")
	}
	for _, want := range []string{"params.data_sources[0].filter", "validation_error"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q: %v", want, err)
		}
	}
}
