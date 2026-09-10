// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/collect"
)

// helpers_test.go — the shared walk over the recorded workspace, and the small
// readers the assertions are written in terms of.

// fixtureHistoryDepth is the depth the fixture walks read at. It is above the
// number of runs the fixture carries, so the corpus walk is not measuring the
// cap; the cap has its own rows.
const fixtureHistoryDepth = 50

// walkTheFixture runs every enumeration over the recorded workspace, in the
// declared order, and fails on any error.
func walkTheFixture(t *testing.T) bbgraph.Result {
	t.Helper()
	got, err := walkTheFixtureAllowingError(t, newFixture(t), func([]collect.Subcollector) {})
	if err != nil {
		t.Fatalf("the recorded workspace walked with an error: %v", err)
	}
	return got
}

// walkTheFixtureAllowingError is the same walk with the enumeration order under
// the caller's control and any incompleteness returned rather than fatal.
//
// THE ORDER IS A PARAMETER BECAUSE THE FIXTURES RUN SERIALLY AND THE REAL WALK
// DOES NOT. Production fans out concurrently, so two collects of one unchanged
// workspace merge their results in whatever order the goroutines finished. A
// stability test that ran the same fixed order twice would produce identical
// input both times and pass with the sorting deleted.
func walkTheFixtureAllowingError(
	t *testing.T, fixture *fixtureServer, arrange func([]collect.Subcollector),
) (bbgraph.Result, error) {
	t.Helper()
	client := fixture.client()

	repositories, err := collect.Repositories(client).Run(context.Background(), fixtureWorkspace)
	if err != nil {
		return repositories, err
	}
	subs := collect.AfterRepos(client, collect.RepoInfos(repositories), fixtureHistoryDepth)
	if arrange != nil {
		arrange(subs)
	}

	out := repositories
	var firstErr error
	for _, sub := range subs {
		got, err := sub.Run(context.Background(), fixtureWorkspace)
		out.Add(got)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// THE SAME THREE JOINS THE WALK RUNS, in the same order. A helper that ran
	// fewer would let a suite assert about a graph the collector never produces.
	bbgraph.ResolveSecretRefs(&out, fixtureWorkspace)
	bbgraph.ResolveStepTargets(&out, fixtureWorkspace)
	bbgraph.EnsureWorkspaceRoot(&out, fixtureWorkspace)
	return out, firstErr
}

// shuffled is the arrange function that randomizes the enumeration order.
func shuffled(subs []collect.Subcollector) {
	rand.Shuffle(len(subs), func(i, j int) { subs[i], subs[j] = subs[j], subs[i] })
}

// runOne runs a single named enumeration over a fixture, after the repository
// listing it depends on, and returns what it read alongside any incompleteness.
func runOne(
	t *testing.T, fixture *fixtureServer, name string,
) (bbgraph.Result, error) {
	t.Helper()
	client := fixture.client()
	repositories, err := collect.Repositories(client).Run(context.Background(), fixtureWorkspace)
	if name == "bitbucket-repos" {
		return repositories, err
	}
	if err != nil {
		t.Fatalf("the repository listing this enumeration depends on failed: %v", err)
	}
	for _, sub := range collect.AfterRepos(
		client, collect.RepoInfos(repositories), fixtureHistoryDepth,
	) {
		if sub.Name == name {
			return sub.Run(context.Background(), fixtureWorkspace)
		}
	}
	t.Fatalf("no enumeration is named %q", name)
	return bbgraph.Result{}, nil
}

// enumerationNames is every enumeration the walk runs, the repository listing
// first.
func enumerationNames(t *testing.T, fixture *fixtureServer) []string {
	t.Helper()
	names := []string{"bitbucket-repos"}
	for _, sub := range collect.AfterRepos(fixture.client(), nil, fixtureHistoryDepth) {
		names = append(names, sub.Name)
	}
	return names
}

// resourceIDs maps every emitted node id to its resource type.
func resourceIDs(got bbgraph.Result) map[string]string {
	out := make(map[string]string, len(got.Resources))
	for _, res := range got.Resources {
		out[res.ID] = res.ResourceType
	}
	return out
}

// resourceByID returns the first resource with this id.
func resourceByID(got bbgraph.Result, id string) (bbgraph.Resource, bool) {
	for _, res := range got.Resources {
		if res.ID == id {
			return res, true
		}
	}
	return bbgraph.Resource{}, false
}

// hasRelation reports whether the walk emitted exactly this edge.
func hasRelation(got bbgraph.Result, from, to, edgeType string) bool {
	return slices.ContainsFunc(got.Relations, func(rel bbgraph.Relation) bool {
		return rel.FromID == from && rel.ToID == to && rel.Type == edgeType
	})
}

// marshal renders a value as the JSON the envelope carries.
func marshal(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling a collect result: %v", err)
	}
	return string(raw)
}
