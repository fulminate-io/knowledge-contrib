// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"slices"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// helpers_test.go — running ONE enumeration, or all of them, over a fixture.

// runOne runs the named enumeration and fails on any error. Use it where the
// enumeration is expected to succeed; [runOneAllowingError] is the arm for the
// rows that assert a failure.
func runOne(t *testing.T, api *fakeAPI, name string) glgraph.Result {
	t.Helper()
	got, err := runOneAllowingError(t, api, name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return got
}

// runOneAllowingError runs the named enumeration and returns whatever it produced
// together with whatever it reported, which is the pair the completeness rows
// assert over: an enumeration that reports an incompleteness still returns
// everything it read.
func runOneAllowingError(t *testing.T, api *fakeAPI, name string) (glgraph.Result, error) {
	t.Helper()
	return runOneWithCaps(t, api, name, collect.Caps{})
}

// runOneWithCaps is runOneAllowingError with the two caps set, for the rows that
// drive them.
func runOneWithCaps(
	t *testing.T, api *fakeAPI, name string, caps collect.Caps,
) (glgraph.Result, error) {
	t.Helper()
	api.newWalk()
	plan := collect.All(api.bundle(), caps, fixtureGroup)
	for _, sub := range plan.Subcollectors {
		if sub.Name == name {
			return sub.Run(context.Background())
		}
	}
	t.Fatalf("no enumeration is named %q; the enumeration list is %v", name, enumerationNames(api))
	return glgraph.Result{}, nil
}

// runAll runs every enumeration serially and returns what they found, what each
// of them reported, and what the SHARED PROJECT DISCOVERY reported about itself.
//
// THE DISCOVERY'S OWN GAPS ARE A THIRD RETURN because they belong to no single
// enumeration: six of them read one discovery, and the walk folds its
// incompleteness in once rather than letting six enumerations repeat it.
func runAll(t *testing.T, api *fakeAPI) (glgraph.Result, map[string]error, []string) {
	t.Helper()
	api.newWalk()
	plan := collect.All(api.bundle(), collect.Caps{}, fixtureGroup)
	out := glgraph.Result{}
	errs := make(map[string]error, len(plan.Subcollectors))
	for _, sub := range plan.Subcollectors {
		got, err := sub.Run(context.Background())
		out.Add(got)
		errs[sub.Name] = err
	}
	return out, errs, plan.Projects.Incompleteness()
}

// enumerationNames is every enumeration's name, for a failure message that names
// what was available rather than only what was missing.
func enumerationNames(api *fakeAPI) []string {
	plan := collect.All(api.bundle(), collect.Caps{}, fixtureGroup)
	names := make([]string, 0, len(plan.Subcollectors))
	for _, sub := range plan.Subcollectors {
		names = append(names, sub.Name)
	}
	return names
}

// countByType counts the emitted resources of one kind.
func countByType(got glgraph.Result, resourceType string) int {
	n := 0
	for _, res := range got.Resources {
		if res.ResourceType == resourceType {
			n++
		}
	}
	return n
}

// hasRelation reports whether the walk emitted exactly this edge.
func hasRelation(got glgraph.Result, from, to, edgeType string) bool {
	return slices.ContainsFunc(got.Relations, func(rel glgraph.Relation) bool {
		return rel.FromID == from && rel.ToID == to && rel.Type == edgeType
	})
}

// resourceIDs is every node id the walk emitted, mapped to its resource type.
func resourceIDs(got glgraph.Result) map[string]string {
	out := make(map[string]string, len(got.Resources))
	for _, res := range got.Resources {
		out[res.ID] = res.ResourceType
	}
	return out
}

// resourceByID is one emitted resource, for the rows that assert on metadata.
func resourceByID(got glgraph.Result, id string) (glgraph.Resource, bool) {
	for _, res := range got.Resources {
		if res.ID == id {
			return res, true
		}
	}
	return glgraph.Resource{}, false
}
