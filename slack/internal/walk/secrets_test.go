// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/slack/internal/walk"
)

// allowedMetadata is every metadata key a node of each type may carry.
var allowedMetadata = map[string][]string{
	walk.NodeChannel: {"channel_id", "created", "is_archived", "is_private", "kind", "name", "purpose", "topic"},
	walk.NodeMessage: {"bot_id", "channel_id", "permalink", "reply_count", "subtype", "thread_ts", "ts", "user", "username"},
	walk.NodeUser:    {"deleted", "display_name", "is_bot", "name", "real_name", "team_id", "title", "tz", "user_id"},
}

// allowedFields is every typed node field a node may set.
var allowedFields = []string{"content", "id", "metadata", "symbol_name", "type"}

// tokenEncodings are the forms of the fixture token a leak could take.
func tokenEncodings() []string {
	return []string{
		fixtureToken,
		url.QueryEscape(fixtureToken),
		strings.Trim(string(must(json.Marshal(fixtureToken))), `"`),
		base64.StdEncoding.EncodeToString([]byte(fixtureToken)),
		base64.URLEncoding.EncodeToString([]byte(fixtureToken)),
	}
}

// assertCarriesNoToken fails the test when text holds the token in any form.
func assertCarriesNoToken(t *testing.T, what, text string) {
	t.Helper()
	for _, enc := range tokenEncodings() {
		if strings.Contains(text, enc) {
			t.Errorf("%s carries the token in the form %q", what, enc)
		}
	}
}

// assertMetadataAllowlisted fails the test for a node field or metadata key
// outside the allowlists, and for an allowlisted metadata key the corpus never
// exercises.
func assertMetadataAllowlisted(t *testing.T, res framework.Result) {
	t.Helper()
	seenMeta := map[string]map[string]bool{}
	for _, n := range res.Nodes {
		var fields map[string]any
		if err := json.Unmarshal(must(json.Marshal(n)), &fields); err != nil {
			t.Fatalf("re-decoding node %s: %v", n.ID, err)
		}
		for k := range fields {
			if !slices.Contains(allowedFields, k) {
				t.Errorf("%s sets the field %q, which is not allowlisted", n.ID, k)
			}
		}
		if seenMeta[n.Type] == nil {
			seenMeta[n.Type] = map[string]bool{}
		}
		for k := range n.Metadata {
			seenMeta[n.Type][k] = true
			if !slices.Contains(allowedMetadata[n.Type], k) {
				t.Errorf("%s carries the metadata key %q, which is not allowlisted for %s", n.ID, k, n.Type)
			}
		}
	}
	for kind, keys := range allowedMetadata {
		if got := sortedKeys(seenMeta[kind]); !slices.Equal(got, keys) {
			t.Errorf("the corpus exercises %s metadata %v, the allowlist is %v", kind, got, keys)
		}
	}
}

// TestTheTokenReachesOnlyTheAuthorizationHeader: the token is sent in the
// Authorization header only; no node, edge or error carries it in any
// encoding; nodes carry exactly the allowlisted fields and metadata keys.
func TestTheTokenReachesOnlyTheAuthorizationHeader(t *testing.T) {
	f := newFixture(t, corpus())
	res := f.mustWalk(t, fullParams())

	f.mu.Lock()
	requests := slices.Clone(f.requests)
	f.mu.Unlock()
	for _, r := range requests {
		if r.auth != "Bearer "+fixtureToken {
			t.Errorf("%s carried Authorization %q", r.key, r.auth)
		}
		assertCarriesNoToken(t, r.key+"'s query", r.rawQuery)
	}
	assertCarriesNoToken(t, "the graph", string(must(json.Marshal(struct {
		N []framework.Node
		E []framework.Edge
	}{res.Nodes, res.Edges}))))
	assertMetadataAllowlisted(t, res)

	refused := newFixture(t, withRoute(infoKey("C0123GENERAL"), ok(`{"ok": false, "error": "invalid_auth"}`)))
	_, err := refused.walkWith(t.Context(), t, fullParams())
	if err == nil {
		t.Fatalf("a refused token produced no error")
	}
	assertCarriesNoToken(t, "the refusal", err.Error())
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
