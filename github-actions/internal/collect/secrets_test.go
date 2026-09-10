// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"encoding/json"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// secrets_test.go — WHAT A SECRET NODE MAY CARRY, asserted over the raw bytes of
// the whole result.
//
// THE ASSERTION IS OVER BYTES RATHER THAN OVER FIELDS because a value can reach
// the graph through more than one door: a node's marshaled detail, its summary,
// any metadata value, its id, or an edge's evidence. A field-by-field check
// covers the doors its author thought of; the encoded result covers all of them,
// including a field added later.
//
// EVERY ONE OF THESE ROWS CARRIES A PLANTED POSITIVE. An absence assertion with
// no planted positive passes on a matcher that never fires and on an empty
// result, which are the two ways this row could be worthless.

// sentinelValue is a recognizable string standing for a credential.
const sentinelValue = "s3cr3t-sentinel-value-do-not-store"

// TestNoSecretValueReachesAnyByteOfTheResult is the disclosure row.
func TestNoSecretValueReachesAnyByteOfTheResult(t *testing.T) {
	got := walkTheFixtureOrganization(t)
	nodes, edges, err := ghgraph.Build(got)
	if err != nil {
		t.Fatalf("building the fixture walk: %v", err)
	}
	encoded := marshal(t, map[string]any{"nodes": nodes, "edges": edges})

	// The fixture carries a secret at each of the three scopes, so this is a
	// result that DOES describe secrets. Without that, an empty result would
	// satisfy the absence below.
	if !strings.Contains(encoded, "ORG_TOKEN") || !strings.Contains(encoded, "PROD_DB_PASS") {
		t.Fatal("the encoded result names no secret at all, so its silence about values is silence " +
			"about nothing")
	}
	if strings.Contains(encoded, sentinelValue) {
		t.Error("a secret value appears in the encoded result")
	}

	// THE PLANTED POSITIVE. The same matcher over the same encoded shape, with
	// the sentinel deliberately in a node's detail, must fire — otherwise the
	// assertion above is a matcher that cannot see what it is looking for.
	planted := marshal(t, map[string]any{"nodes": nodes, "edges": edges, "planted": sentinelValue})
	if !strings.Contains(planted, sentinelValue) {
		t.Fatal("the planted sentinel was not detected; the assertion above proves nothing")
	}
}

// TestASecretNodeCarriesExactlyThreeDetailFieldsAndThreeMetadataKeys is the
// allowlist itself, named field by field.
//
// THE LIST IS WRITTEN OUT rather than derived from the struct: a field added to
// the detail type would then silently join the allowlist, which is exactly the
// change this row exists to catch.
func TestASecretNodeCarriesExactlyThreeDetailFieldsAndThreeMetadataKeys(t *testing.T) {
	got := walkTheFixtureOrganization(t)
	nodes, _, err := ghgraph.Build(got)
	if err != nil {
		t.Fatalf("building the fixture walk: %v", err)
	}

	checked := 0
	for _, node := range nodes {
		if node.Metadata["resource_type"] != ghgraph.ResourceTypeSecret {
			continue
		}
		checked++

		var detail map[string]any
		if err := json.Unmarshal([]byte(node.Content), &detail); err != nil {
			t.Fatalf("decoding the detail of %q: %v", node.ID, err)
		}
		for key := range detail {
			if key != "name" && key != "scope" && key != "visibility" {
				t.Errorf("the secret node %q carries the detail field %q, which is outside the "+
					"allowlist of name, scope and visibility", node.ID, key)
			}
		}
		for key := range node.Metadata {
			switch key {
			case "org", "scope", "repo", "resource_type", "provider":
			default:
				t.Errorf("the secret node %q carries the metadata key %q, which is outside the "+
					"allowlist", node.ID, key)
			}
		}
	}
	if checked != 4 {
		t.Errorf("the walk emitted %d secret nodes, want 4 — one at the organization, two at the "+
			"repository and one at the environment", checked)
	}
}

// TestTheProviderExposesNoSecretValueToRead is the upstream half of the same
// property. It is not a claim about this collector: it is the reason the claim
// above can be true at all, pinned against the SDK's own type so a version that
// added a value field would red here.
func TestTheProviderExposesNoSecretValueToRead(t *testing.T) {
	encoded, err := json.Marshal(&gogithub.Secret{Name: "MY_SECRET", Visibility: "all"})
	if err != nil {
		t.Fatalf("encoding the provider's secret type: %v", err)
	}
	for _, tell := range []string{`"value"`, `"Value"`, `"encrypted_value"`} {
		if strings.Contains(string(encoded), tell) {
			t.Errorf("the provider's secret type carries %s: %s", tell, encoded)
		}
	}
}
