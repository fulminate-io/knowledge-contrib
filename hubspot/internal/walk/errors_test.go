// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/walk"
)

// TestRefusalsFailTheCollectNamingTheCause.
func TestRefusalsFailTheCollectNamingTheCause(t *testing.T) {
	missing := answer{status: 207, body: `{"status": "COMPLETE", "results": [], "numErrors": 1, "errors": [{"status": "error",
		"category": "OBJECT_NOT_FOUND", "message": "Could not get some CONTACT objects, they may be deleted or not exist.",
		"context": {"ids": ["201"]}}]}`}
	for _, c := range []struct {
		name   string
		key    string
		answer answer
		wants  []string
	}{
		{name: "401", key: readKey("companies", "101"), answer: answer{status: http.StatusUnauthorized,
			body: errorBody("INVALID_AUTHENTICATION", "Authentication credentials not found.")}, wants: []string{"401", walk.TokenVariable}},
		{name: "403 on deals", key: readKey("deals", "301"), answer: answer{status: http.StatusForbidden,
			body: `{"status": "error", "message": "This app hasn't been granted all required scopes to make this call."}`},
			wants: []string{"403", "crm.objects.deals.read", walk.TokenVariable}},
		{name: "named record not returned", key: readKey("contacts", "201"), answer: missing, wants: []string{"201", "OBJECT_NOT_FOUND"}},
		{name: "named list not found", key: listKey("7"), answer: answer{status: http.StatusNotFound,
			body: errorBody("OBJECT_NOT_FOUND", "List does not exist")}, wants: []string{"404", "list 7"}},
		{name: "search filter refused", key: searchKey(""), answer: answer{status: http.StatusBadRequest,
			body: errorBody("VALIDATION_ERROR", "There was a problem with the request.")}, wants: []string{"params.searches[0].filter_groups", "400"}},
		{name: "unknown 4xx", key: readKey("companies", "101"), answer: answer{status: http.StatusConflict,
			body: errorBody("CONFLICT", "conflict")}, wants: []string{"409", "CONFLICT"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, withRoute(c.key, c.answer))
			res, err := f.walkWith(t.Context(), t, fullParams(t))
			if err == nil {
				t.Fatalf("got a result with %d nodes and no error, want the collect to fail", len(res.Nodes))
			}
			for _, want := range c.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the error does not name %q: %v", want, err)
				}
			}
		})
	}
}

// TestReadsThatLosePartOfTheirScopeMarkTheWalkIncomplete: an associated
// record HubSpot does not return, an associations batch answered 207 with
// errors, a 5xx in mid-pagination and a list of an object type this collector
// does not read each keep what was read and name the read.
func TestReadsThatLosePartOfTheirScopeMarkTheWalkIncomplete(t *testing.T) {
	for _, c := range []struct {
		name   string
		key    string
		answer answer
		want   string
		kept   string
		extra  map[string]answer
	}{
		{name: "associated record not returned", key: readKey("tickets", "401"), answer: ok(batch()),
			want: "tickets 401: the batch read did not return them", kept: "hubspot:companies:101"},
		{name: "associations 207", key: assocKey("companies", "deals", "101"), answer: answer{status: 207,
			body: `{"status": "COMPLETE", "results": [], "numErrors": 1, "errors": [{"status": "error", "category": "OBJECT_NOT_FOUND",
				"message": "No company is associated with this ID", "context": {"fromObjectId": ["101"]}}]}`},
			want: "OBJECT_NOT_FOUND 101", kept: "hubspot:calls:501"},
		{name: "5xx in mid-pagination", key: membersKey("CAE"), answer: answer{status: http.StatusBadGateway, body: "bad gateway"},
			want: "502", kept: "hubspot:contacts:102",
			extra: map[string]answer{readKey("contacts", "102"): ok(batch(record("102", map[string]string{"firstname": "Grace"})))}},
		{name: "search total above what was read", key: searchKey("1"),
			answer: ok(searchPage(3, "", record("205", map[string]string{"firstname": "Edsger"}))),
			want:   "HubSpot reported 3 results and 2 were read", kept: "hubspot:contacts:204"},
		{name: "list of another object type", key: listKey("7"), answer: ok(`{"list": {"listId": "7", "name": "Products", "objectTypeId": "0-7"}}`),
			want: `objectTypeId "0-7"`, kept: "hubspot:contacts:204"},
	} {
		t.Run(c.name, func(t *testing.T) {
			routes := withRoute(c.key, c.answer)
			for k, a := range c.extra {
				routes[k] = []answer{a}
			}
			f := newFixture(t, routes)
			res, err := f.walkWith(t.Context(), t, fullParams(t))
			if err != nil {
				t.Fatalf("got %v, want an incomplete walk", err)
			}
			if res.Complete.IsComplete() || !strings.Contains(res.Complete.Reason(), c.want) {
				t.Fatalf("got complete=%v reason %q, want incomplete naming %q", res.Complete.IsComplete(), res.Complete.Reason(), c.want)
			}
			found := false
			for _, n := range res.Nodes {
				found = found || n.ID == c.kept
			}
			if !found {
				t.Errorf("%s, read before the incomplete read, was not kept", c.kept)
			}
		})
	}
}

// TestEmptyReadsAreComplete: a list with no members, a search with no hits and
// a named record with no associations leave the walk complete.
func TestEmptyReadsAreComplete(t *testing.T) {
	routes := corpus()
	routes[membersKey("")] = []answer{ok(`{"results": [], "total": 0}`)}
	routes[searchKey("")] = []answer{ok(searchPage(0, ""))}
	f := newFixture(t, routes)
	p := fullParams(t)
	p.Companies, p.Deals = nil, nil
	res := f.mustWalk(t, p)
	if !res.Complete.IsComplete() {
		t.Fatalf("empty reads reported incomplete: %s", res.Complete.Reason())
	}
	if n := len(nodesOfType(res, "hubspot-contact")); n != 1 {
		t.Errorf("got %d contact nodes, want the named contact alone", n)
	}
}

// TestSearchesAreRefusedLocallyAtHubSpotsCaps: each refusal names the param
// and HubSpot's cap, and no request is made.
func TestSearchesAreRefusedLocallyAtHubSpotsCaps(t *testing.T) {
	filter := func(i int) map[string]any {
		return map[string]any{"propertyName": "hs_object_id", "operator": "GT", "value": fmt.Sprint(i)}
	}
	group := func(n int) map[string]any {
		filters := make([]any, 0, n)
		for i := range n {
			filters = append(filters, filter(i))
		}
		return map[string]any{"filters": filters}
	}
	groups := func(sizes ...int) []map[string]any {
		out := make([]map[string]any, 0, len(sizes))
		for _, n := range sizes {
			out = append(out, group(n))
		}
		return out
	}
	long := []map[string]any{{"filters": []any{map[string]any{"propertyName": "name", "operator": "EQ", "value": strings.Repeat("x", 3000)}}}}
	for _, c := range []struct {
		name   string
		search walk.Search
		want   string
	}{
		{name: "six groups", search: walk.Search{ObjectType: "contacts", FilterGroups: groups(1, 1, 1, 1, 1, 1)}, want: "at most 5"},
		{name: "seven filters in a group", search: walk.Search{ObjectType: "contacts", FilterGroups: groups(7)}, want: "at most 6 per group"},
		{name: "nineteen filters", search: walk.Search{ObjectType: "contacts", FilterGroups: groups(5, 5, 5, 4)}, want: "at most 18"},
		{name: "a body over 3000 characters", search: walk.Search{ObjectType: "contacts", FilterGroups: long}, want: "at most 3000"},
		{name: "no filter", search: walk.Search{ObjectType: "contacts"}, want: "filter_groups is empty"},
		{name: "unknown object type", search: walk.Search{ObjectType: "products", FilterGroups: groups(1)}, want: "object_type"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, corpus())
			_, err := f.walkWith(t.Context(), t, walk.Params{Searches: []walk.Search{c.search}})
			if err == nil || !strings.Contains(err.Error(), "params.searches[0]") || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want a refusal naming params.searches[0] and %q", err, c.want)
			}
			if n := len(f.requestsWith("")); n != 0 {
				t.Errorf("a refused collect made %d requests", n)
			}
		})
	}
	t.Run("AtTheCapsIsAccepted", func(t *testing.T) {
		raw, err := json.Marshal(groups(6, 6, 6))
		if err != nil {
			t.Fatal(err)
		}
		var at []map[string]any
		if err := json.Unmarshal(raw, &at); err != nil {
			t.Fatal(err)
		}
		routes := map[string][]answer{searchKey(""): {ok(searchPage(0, ""))}}
		f := newFixture(t, routes)
		if _, err := f.walkWith(t.Context(), t, walk.Params{Searches: []walk.Search{{ObjectType: "contacts", FilterGroups: at}}}); err != nil {
			t.Fatalf("a search at the caps was refused: %v", err)
		}
	})
}

// TestParamsAreRefusedNamingTheParam.
func TestParamsAreRefusedNamingTheParam(t *testing.T) {
	for _, c := range []struct {
		name   string
		params walk.Params
		want   string
	}{
		{name: "no target", params: walk.Params{}, want: "params.companies"},
		{name: "malformed company id", params: walk.Params{Companies: []string{"acme"}}, want: "params.companies[0]"},
		{name: "malformed list id", params: walk.Params{Lists: []string{""}}, want: "params.lists[0]"},
		{name: "properties of an unknown type", params: walk.Params{Contacts: []string{"1"}, Properties: map[string][]string{"products": {"x"}}}, want: "params.properties"},
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
