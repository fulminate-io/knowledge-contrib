// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// credentials_test.go — THE ALLOWLIST OF WHAT MAY REACH A STORED NODE, asserted
// over the RAW BYTES of the whole built result rather than over a parsed field.
//
// WHY THE BYTES. A value can reach a consumer through the node id, the symbol
// name, the summary, the marshaled content, any metadata value or an edge — six
// carriers, and an assertion that read one field would say nothing about the
// other five. The result is marshaled exactly as the envelope carries it and
// searched whole.
//
// THREE PLANTED SENTINELS, because an absence assertion with no known positive
// passes on a matcher that never fires and on an empty result:
//
//   - a PROJECT-scoped variable's value, which is the operator's own secret and
//     the reason a CI/CD variable node carries no content at all;
//   - a GROUP-scoped variable's value, on the second of the two variable paths;
//   - a project's RUNNER REGISTRATION TOKEN, which the provider returns inside
//     its own project value. This is the one the source provider does not survive:
//     it marshals that value whole into the project node's content, so the token
//     lands in a stored node, its summarizer input and its search index.

// TestNoCredentialValueReachesAnyByteOfTheResult is the row.
func TestNoCredentialValueReachesAnyByteOfTheResult(t *testing.T) {
	got := walkTheFixtureGroup(t)
	nodes, edges, err := glgraph.Build(got)
	if err != nil {
		t.Fatalf("building the fixture walk: %v", err)
	}
	rendered := renderWhole(t, nodes, edges)

	// The known positive FIRST: the scan finds a planted value when one is there,
	// so a clean verdict below is a measurement rather than a matcher that never
	// fires.
	assertTheScanFires(t, rendered)

	for _, sentinel := range []struct{ what, value string }{
		{"a project-scoped variable's value", projectVarValue},
		{"a group-scoped variable's value", groupVarValue},
		{"a project's runner registration token", runnersTokenValue},
	} {
		if strings.Contains(rendered, sentinel.value) {
			t.Errorf("%s reached the result. Nothing this collector emits may carry one: the "+
				"variable enumeration reads the key and the flags, and every node's content is a "+
				"narrowed struct rather than the provider's own value", sentinel.what)
		}
	}

	// The known positive for the walk itself: the variables WERE enumerated, so
	// this is a result that read them and carried no value rather than one that
	// never looked.
	ids := resourceIDs(got)
	for _, want := range []string{
		"gitlab:acme/Variable/acme/api/API_KEY",
		"gitlab:acme/Variable/acme/GROUP_DEPLOY_TOKEN",
		"gitlab:acme/Project/acme/api",
	} {
		if _, ok := ids[want]; !ok {
			t.Errorf("the node %q is missing, so the absences above are about an empty result", want)
		}
	}
}

// TestAVariableNodeCarriesOnlyItsAllowedFields is the same rule stated per field
// rather than per byte, so a failure names WHICH carrier grew.
func TestAVariableNodeCarriesOnlyItsAllowedFields(t *testing.T) {
	got := runOne(t, fixtureAPI(), "gitlab-variables")

	for _, row := range []struct {
		id      string
		allowed []string
	}{
		{"gitlab:acme/Variable/acme/api/API_KEY",
			[]string{"scope", "project", "protected", "masked", "resource_type", "provider"}},
		{"gitlab:acme/Variable/acme/GROUP_DEPLOY_TOKEN",
			[]string{"scope", "group", "resource_type", "provider"}},
	} {
		res, ok := resourceByID(got, row.id)
		if !ok {
			t.Errorf("the variable node %q is missing", row.id)
			continue
		}
		if res.Content != "" {
			t.Errorf("%q carries content %q; a variable node carries none", row.id, res.Content)
		}
		nodes, _, err := glgraph.Build(glgraph.Result{Resources: []glgraph.Resource{res}})
		if err != nil {
			t.Fatalf("building %q: %v", row.id, err)
		}
		for key := range nodes[0].Metadata {
			if !contains(row.allowed, key) {
				t.Errorf("%q carries the metadata key %q, which is outside the allowlist %v",
					row.id, key, row.allowed)
			}
		}
	}
}

// renderWhole is the built result as the bytes the envelope carries.
func renderWhole(t *testing.T, nodes any, edges any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"nodes": nodes, "edges": edges})
	if err != nil {
		t.Fatalf("rendering the built result: %v", err)
	}
	return string(raw)
}

// assertTheScanFires drives the same matcher over a result that DOES carry a
// planted value, in the same run.
func assertTheScanFires(t *testing.T, cleanResult string) {
	t.Helper()
	planted := glgraph.Result{Resources: []glgraph.Resource{{
		ID:           "gitlab:acme/Variable/acme/api/PLANTED",
		Name:         "PLANTED",
		ResourceType: glgraph.ResourceTypeVariable,
		Content:      projectVarValue,
	}}}
	nodes, edges, err := glgraph.Build(planted)
	if err != nil {
		t.Fatalf("building the planted control: %v", err)
	}
	if !strings.Contains(renderWhole(t, nodes, edges), projectVarValue) {
		t.Fatal("the scan did not find a value planted directly in a node's content; its verdict " +
			"over the real result means nothing")
	}
	if cleanResult == "" {
		t.Fatal("the real result rendered as nothing at all")
	}
}

func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}
