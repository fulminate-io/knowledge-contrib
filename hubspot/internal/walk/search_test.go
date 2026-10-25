// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/walk"
)

// capRoutes serves a search of 10,001 contacts: 50 pages of 200 ids 1 to
// 10,000 under the first query, then one page holding id 10,001 under the
// window query, which a key cannot tell apart from the first query's first
// page, so that key answers both in order.
func capRoutes() map[string][]answer {
	routes := map[string][]answer{}
	var firstPages []answer
	for page := range 50 {
		records := make([]string, 0, 200)
		for i := 1; i <= 200; i++ {
			records = append(records, record(strconv.Itoa(page*200+i), nil))
		}
		next := ""
		if page < 49 {
			next = strconv.Itoa((page + 1) * 200)
		}
		a := ok(searchPage(10001, next, records...))
		if page == 0 {
			firstPages = append(firstPages, a)
			continue
		}
		routes[searchKey(strconv.Itoa(page*200))] = []answer{a}
	}
	firstPages = append(firstPages, ok(searchPage(1, "", record("10001", nil))))
	routes[searchKey("")] = firstPages
	return routes
}

func capSearch(t *testing.T, groupsJSON string) walk.Params {
	t.Helper()
	var groups []map[string]any
	if err := json.Unmarshal([]byte(groupsJSON), &groups); err != nil {
		t.Fatal(err)
	}
	return walk.Params{Searches: []walk.Search{{ObjectType: "contacts", FilterGroups: groups}}}
}

// TestASearchPastTheCapIsWindowedByObjectID: every request sorts by
// hs_object_id ascending; after 10,000 results the next query adds
// hs_object_id GT the last id to every group, and every hit is read.
func TestASearchPastTheCapIsWindowedByObjectID(t *testing.T) {
	f := newFixture(t, capRoutes())
	res := f.mustWalk(t, capSearch(t, customerFilter))
	if !res.Complete.IsComplete() {
		t.Fatalf("a windowed search past the cap reported incomplete: %s", res.Complete.Reason())
	}
	if n := len(nodesOfType(res, "hubspot-contact")); n != 10001 {
		t.Errorf("got %d contacts, want 10001", n)
	}
	searches := f.requestsWith("POST /crm/objects/2026-09/contacts/search")
	if len(searches) != 51 {
		t.Fatalf("got %d search requests, want 51", len(searches))
	}
	var sorts any
	if err := json.Unmarshal([]byte(`[{"propertyName": "hs_object_id", "direction": "ASCENDING"}]`), &sorts); err != nil {
		t.Fatal(err)
	}
	for i, s := range searches {
		if !reflect.DeepEqual(s.body["sorts"], sorts) {
			t.Fatalf("search %d carried sorts %v", i, s.body["sorts"])
		}
	}
	var window any
	if err := json.Unmarshal([]byte(`[{"filters": [{"propertyName": "lifecyclestage", "operator": "EQ", "value": "customer"},
		{"propertyName": "hs_object_id", "operator": "GT", "value": "10000"}]}]`), &window); err != nil {
		t.Fatal(err)
	}
	if got := searches[50].body["filterGroups"]; !reflect.DeepEqual(got, window) {
		t.Errorf("the window query carried filterGroups %v, want %v", got, window)
	}
	if _, after := searches[50].body["after"]; after {
		t.Errorf("the window query carried an after")
	}
}

// TestACapTheWindowCannotPassMarksTheWalkIncomplete: a group already at six
// filters cannot take the window filter.
func TestACapTheWindowCannotPassMarksTheWalkIncomplete(t *testing.T) {
	six := `[{"filters": [` + strings.TrimSuffix(strings.Repeat(`{"propertyName": "lifecyclestage", "operator": "EQ", "value": "customer"},`, 6), ",") + `]}]`
	f := newFixture(t, capRoutes())
	res := f.mustWalk(t, capSearch(t, six))
	if res.Complete.IsComplete() || !strings.Contains(res.Complete.Reason(), "already carries HubSpot's 6 filters") {
		t.Fatalf("got complete=%v reason %q, want incomplete naming the filter cap", res.Complete.IsComplete(), res.Complete.Reason())
	}
	if n := len(f.requestsWith("POST /crm/objects/2026-09/contacts/search")); n != 50 {
		t.Errorf("got %d search requests, want the first query's 50", n)
	}
}
