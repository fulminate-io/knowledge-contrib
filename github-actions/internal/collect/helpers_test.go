// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// helpers_test.go — running ONE enumeration over a fixture, which is what every
// per-enumeration row does.

// runOne runs the named enumeration and fails on any error. Use it where the
// enumeration is expected to succeed; [runOneExpectingError] is the arm for the
// rows that assert a failure.
func runOne(t *testing.T, repos *fakeRepos, actions *fakeActions, name string) ghgraph.Result {
	t.Helper()
	got, err := runOneAllowingError(t, repos, actions, name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return got
}

// runOneAllowingError runs the named enumeration and returns whatever it
// produced together with whatever it reported, which is the pair the
// completeness rows assert over: an enumeration that reports an incompleteness
// still returns everything it read.
func runOneAllowingError(
	t *testing.T, repos *fakeRepos, actions *fakeActions, name string,
) (ghgraph.Result, error) {
	t.Helper()
	api := collect.API{Repos: repos, Actions: actions}
	for _, sub := range collect.All(api, collect.Caps{}) {
		if sub.Name == name {
			return sub.Run(context.Background(), fixtureOrg)
		}
	}
	t.Fatalf("no enumeration is named %q; the enumeration list is %v", name, enumerationNames(api))
	return ghgraph.Result{}, nil
}

// runOneWithCaps is runOneAllowingError with the two caps set, for the rows that
// drive them.
func runOneWithCaps(
	t *testing.T, repos *fakeRepos, actions *fakeActions, name string, caps collect.Caps,
) ghgraph.Result {
	t.Helper()
	api := collect.API{Repos: repos, Actions: actions}
	for _, sub := range collect.All(api, caps) {
		if sub.Name == name {
			got, err := sub.Run(context.Background(), fixtureOrg)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return got
		}
	}
	t.Fatalf("no enumeration is named %q", name)
	return ghgraph.Result{}
}

// enumerationNames is every enumeration's name, for a failure message that names
// what was available rather than only what was missing.
func enumerationNames(api collect.API) []string {
	subs := collect.All(api, collect.Caps{})
	names := make([]string, 0, len(subs))
	for _, sub := range subs {
		names = append(names, sub.Name)
	}
	return names
}

// countByTypeInRepo counts the emitted resources of one kind belonging to one
// repository.
//
// THE CAPS ARE PER REPOSITORY, so a whole-walk count cannot assert them: with two
// repositories carrying runs, a cap of 10 yields 11 nodes across the walk and 10
// in each repository, and only the second number is the property.
func countByTypeInRepo(got ghgraph.Result, resourceType, repo string) int {
	n := 0
	for _, res := range got.Resources {
		if res.ResourceType == resourceType && res.Metadata["repo"] == repo {
			n++
		}
	}
	return n
}
