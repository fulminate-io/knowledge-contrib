// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/walk"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	return raw
}

// tokenEncodings are the forms of the fixture token a leak could take.
func tokenEncodings(t *testing.T) []string {
	t.Helper()
	return []string{
		fixtureToken,
		url.QueryEscape(fixtureToken),
		strings.Trim(string(mustJSON(t, fixtureToken)), `"`),
		base64.StdEncoding.EncodeToString([]byte(fixtureToken)),
		base64.URLEncoding.EncodeToString([]byte(fixtureToken)),
	}
}

// assertCarriesNoToken fails the test when text holds the token in any form.
func assertCarriesNoToken(t *testing.T, what, text string) {
	t.Helper()
	for _, enc := range tokenEncodings(t) {
		if strings.Contains(text, enc) {
			t.Errorf("%s carries the token in the form %q", what, enc)
		}
	}
}

// allowedMetadata is every metadata key a node of each kind may carry; a
// record also carries one property:<name> key per property it was read with.
var allowedMetadata = map[string][]string{
	"record":      {"archived", "created_at", "object_type", "record_id", "updated_at"},
	walk.NodeList: {"list_id", "object_type_id"},
}

func kindOf(nodeType string) string {
	if nodeType == walk.NodeList {
		return walk.NodeList
	}
	return "record"
}

var allowedFields = []string{"content", "id", "metadata", "symbol_name", "type"}

// assertMetadataAllowlisted fails the test for a node field or metadata key
// outside the allowlists, and for an allowlisted key the corpus never sets.
func assertMetadataAllowlisted(t *testing.T, res framework.Result) {
	t.Helper()
	seen := map[string]map[string]bool{}
	for _, n := range res.Nodes {
		var fields map[string]any
		if err := json.Unmarshal(mustJSON(t, n), &fields); err != nil {
			t.Fatalf("re-decoding node %s: %v", n.ID, err)
		}
		for k := range fields {
			if !slices.Contains(allowedFields, k) {
				t.Errorf("%s sets the field %q, which is not allowlisted", n.ID, k)
			}
		}
		kind := kindOf(n.Type)
		if seen[kind] == nil {
			seen[kind] = map[string]bool{}
		}
		for k := range n.Metadata {
			if kind == "record" && strings.HasPrefix(k, "property:") {
				continue
			}
			seen[kind][k] = true
			if !slices.Contains(allowedMetadata[kind], k) {
				t.Errorf("%s carries the metadata key %q, which is not allowlisted for %s", n.ID, k, n.Type)
			}
		}
	}
	for kind, keys := range allowedMetadata {
		if got := sortedKeys(seen[kind]); !slices.Equal(got, keys) {
			t.Errorf("the corpus exercises %s metadata %v, the allowlist is %v", kind, got, keys)
		}
	}
}

// TestTheTokenReachesOnlyTheAuthorizationHeader: the token rides the
// Authorization header of every request and nothing else; no node, edge or
// error carries it; nodes carry exactly the allowlisted fields and metadata
// keys.
func TestTheTokenReachesOnlyTheAuthorizationHeader(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, fullParams(t))
	for _, r := range f.requestsWith("") {
		if r.auth != "Bearer "+fixtureToken {
			t.Errorf("%s carried Authorization %q", r.key, r.auth)
		}
		assertCarriesNoToken(t, r.key+"'s query", r.query)
		assertCarriesNoToken(t, r.key+"'s body", string(mustJSON(t, r.body)))
	}
	assertCarriesNoToken(t, "the graph", string(mustJSON(t, struct {
		N []framework.Node
		E []framework.Edge
	}{res.Nodes, res.Edges})))
	assertMetadataAllowlisted(t, res)

	refused := newFixture(t, withRoute(readKey("companies", "101"), answer{status: http.StatusUnauthorized,
		body: errorBody("INVALID_AUTHENTICATION", "Authentication credentials not found.")}))
	_, err := refused.walkWith(t.Context(), t, fullParams(t))
	if err == nil {
		t.Fatalf("a refused token produced no error")
	}
	assertCarriesNoToken(t, "the refusal", err.Error())
}
