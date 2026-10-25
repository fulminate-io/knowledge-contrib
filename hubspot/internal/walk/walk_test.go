// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/walk"
)

// TestAFullFixtureWalkEmitsTheWholeCorpusAndIsComplete: every named record,
// list member and search hit, and every record associated with them, lands as
// its own node; each association carries the association types the fixture
// sent; the vocabulary is the declared one.
func TestAFullFixtureWalkEmitsTheWholeCorpusAndIsComplete(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, fullParams(t))
	if !res.Complete.IsComplete() {
		t.Fatalf("a clean fixture walk reported incomplete: %s", res.Complete.Reason())
	}
	want := map[string][]string{
		"hubspot-company": {"102", "101"}, "hubspot-contact": {"102", "201", "202", "203", "204", "205"},
		"hubspot-deal": {"301"}, "hubspot-ticket": {"401"}, "hubspot-call": {"501"}, "hubspot-email": {"601"},
		"hubspot-meeting": {"701"}, "hubspot-note": {"801"}, "hubspot-task": {"901"},
	}
	objectOf := map[string]string{
		"hubspot-company": "companies", "hubspot-contact": "contacts", "hubspot-deal": "deals", "hubspot-ticket": "tickets",
		"hubspot-call": "calls", "hubspot-email": "emails", "hubspot-meeting": "meetings", "hubspot-note": "notes", "hubspot-task": "tasks",
	}
	for kind, ids := range want {
		var wantIDs []string
		for _, id := range ids {
			wantIDs = append(wantIDs, "hubspot:"+objectOf[kind]+":"+id)
		}
		slices.Sort(wantIDs)
		if got := sortedKeys(nodesOfType(res, kind)); !slices.Equal(got, wantIDs) {
			t.Errorf("%s nodes: got %v, want %v", kind, got, wantIDs)
		}
	}
	if got := sortedKeys(nodesOfType(res, walk.NodeList)); !slices.Equal(got, []string{"hubspot:list:7"}) {
		t.Errorf("list nodes: got %v", got)
	}

	var wantTypes any
	if err := json.Unmarshal([]byte("["+assocType+"]"), &wantTypes); err != nil {
		t.Fatal(err)
	}
	assoc := edges(res, walk.EdgeAssociatedWith)
	for _, to := range []string{"contacts:201", "contacts:202", "companies:102", "deals:301", "tickets:401", "calls:501",
		"emails:601", "meetings:701", "notes:801", "tasks:901"} {
		e, found := assoc["hubspot:companies:101 -> hubspot:"+to]
		if !found {
			t.Errorf("no ASSOCIATED_WITH from company 101 to %s", to)
			continue
		}
		var got any
		if err := json.Unmarshal([]byte(e.Evidence), &got); err != nil {
			t.Errorf("the association to %s carries evidence %q, which is not JSON: %v", to, e.Evidence, err)
			continue
		}
		if !reflect.DeepEqual(got, wantTypes) {
			t.Errorf("the association to %s carries evidence %q, want the fixture's association types", to, e.Evidence)
		}
	}
	members := edges(res, walk.EdgeHasMember)
	for _, m := range []string{"102", "203"} {
		if _, found := members["hubspot:list:7 -> hubspot:contacts:"+m]; !found {
			t.Errorf("no HAS_MEMBER from list 7 to contact %s", m)
		}
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

// TestTheWalkIsOneHopAndFollowsEveryPage: associations are read only for
// named records, list members and search hits; membership and search pages
// are each read once with their after; an association result's paging is
// followed for that input alone.
func TestTheWalkIsOneHopAndFollowsEveryPage(t *testing.T) {
	f := newFixture(t, corpus())
	f.mustWalk(t, fullParams(t))
	targets := map[string]bool{"101": true, "201": true, "102": true, "203": true, "204": true, "205": true, "301": true}
	for _, r := range f.requestsWith("POST /crm/associations/") {
		inputs, _ := r.body["inputs"].([]any)
		for _, in := range inputs {
			id, _ := in.(map[string]any)["id"].(string)
			if !targets[id] {
				t.Errorf("%s read the associations of %s, which is only an associated record", r.key, id)
			}
		}
	}
	for _, k := range []string{membersKey(""), membersKey("CAE"), searchKey(""), searchKey("1"),
		assocKey("companies", "contacts", "101"), assocKey("companies", "contacts", "101@MjAy")} {
		if n := f.count(k); n != 1 {
			t.Errorf("%s was requested %d times, want 1", k, n)
		}
	}
	for _, r := range f.requestsWith(membersKey("CAE")) {
		if !strings.Contains(r.query, "after=CAE") {
			t.Errorf("the second membership page's after did not ride the query string: %s", r.query)
		}
	}
}

// TestNamedRecordsAreBatchedByHundred: 150 named contacts are read in two
// batches of 100 and 50, and their associations in one request per type.
func TestNamedRecordsAreBatchedByHundred(t *testing.T) {
	routes := map[string][]answer{}
	var ids, first, second []string
	for i := 1; i <= 150; i++ {
		id := strconv.Itoa(1000 + i)
		ids = append(ids, id)
		if i <= 100 {
			first = append(first, id)
		} else {
			second = append(second, id)
		}
	}
	for _, chunk := range [][]string{first, second} {
		records := make([]string, 0, len(chunk))
		for _, id := range chunk {
			records = append(records, record(id, map[string]string{"firstname": "n" + id}))
		}
		routes[readKey("contacts", chunk...)] = []answer{ok(batch(records...))}
	}
	f := newFixture(t, routes)
	res := f.mustWalk(t, walk.Params{Contacts: ids})
	if !res.Complete.IsComplete() {
		t.Fatalf("got incomplete: %s", res.Complete.Reason())
	}
	reads := f.requestsWith("POST /crm/objects/2026-09/contacts/batch/read")
	if len(reads) != 2 || len(reads[0].body["inputs"].([]any)) != 100 || len(reads[1].body["inputs"].([]any)) != 50 {
		t.Fatalf("got %d batch reads, want 2 of 100 and 50", len(reads))
	}
	for _, to := range []string{"contacts", "companies", "deals", "tickets", "calls", "emails", "meetings", "notes", "tasks"} {
		if n := len(f.requestsWith("POST /crm/associations/2026-09/contacts/" + to + "/")); n != 1 {
			t.Errorf("the contacts-to-%s associations took %d requests, want 1", to, n)
		}
	}
}

// TestPropertiesAreRequestedOnlyWhenNamed.
func TestPropertiesAreRequestedOnlyWhenNamed(t *testing.T) {
	f := newFixture(t, corpus())
	f.mustWalk(t, walk.Params{Companies: []string{"101"}})
	for _, r := range f.requestsWith(readKey("companies", "101")) {
		if _, sent := r.body["properties"]; sent {
			t.Errorf("a read with no properties param sent properties %v", r.body["properties"])
		}
	}
	f2 := newFixture(t, corpus())
	f2.mustWalk(t, walk.Params{Companies: []string{"101"}, Properties: map[string][]string{"companies": {"industry", "numberofemployees"}}})
	reads := f2.requestsWith(readKey("companies", "101"))
	if len(reads) != 1 || fmt.Sprint(reads[0].body["properties"]) != "[industry numberofemployees]" {
		t.Errorf("the companies read did not carry the named properties: %v", reads)
	}

	p := fullParams(t)
	p.Properties = map[string][]string{"contacts": {"jobtitle"}}
	f3 := newFixture(t, corpus())
	f3.mustWalk(t, p)
	for _, s := range f3.requestsWith("POST /crm/objects/2026-09/contacts/search") {
		if fmt.Sprint(s.body["properties"]) != "[jobtitle]" {
			t.Errorf("a contacts search carried properties %v, want [jobtitle]", s.body["properties"])
		}
	}
}

// TestTheSearchCarriesTheFilterAsWrittenAndTheIDSort.
func TestTheSearchCarriesTheFilterAsWrittenAndTheIDSort(t *testing.T) {
	f := newFixture(t, corpus())
	f.mustWalk(t, fullParams(t))
	searches := f.requestsWith("POST /crm/objects/2026-09/contacts/search")
	if len(searches) != 2 {
		t.Fatalf("got %d search requests, want 2", len(searches))
	}
	var groups, sorts any
	if err := json.Unmarshal([]byte(customerFilter), &groups); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`[{"propertyName": "hs_object_id", "direction": "ASCENDING"}]`), &sorts); err != nil {
		t.Fatal(err)
	}
	for i, s := range searches {
		if !reflect.DeepEqual(s.body["filterGroups"], groups) || !reflect.DeepEqual(s.body["sorts"], sorts) {
			t.Errorf("search %d carried filterGroups %v and sorts %v", i, s.body["filterGroups"], s.body["sorts"])
		}
		if limit, _ := s.body["limit"].(float64); limit != 200 {
			t.Errorf("search %d carried limit %v, want 200", i, s.body["limit"])
		}
	}
	if after, _ := searches[1].body["after"].(string); after != "1" {
		t.Errorf("the second search page carried after %q, want 1", after)
	}
	if !slices.Equal(f.paces, []time.Duration{200 * time.Millisecond}) {
		t.Errorf("search pacing recorded %v, want one 200ms wait before the second request", f.paces)
	}
}
