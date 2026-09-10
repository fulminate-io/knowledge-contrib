// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/walk"
)

// params_test.go — THE CAP MATRIX AND THE COLLECT ID, both refused before
// anything is dialed.
//
// THE MATRIX IS FIVE ARMS PER PARAMETER, and the low one is the reason both caps
// are pointers: unset takes the default; a value below 1 is REFUSED rather than
// silently read as unset; a value in range is honored; the ceiling itself is
// honored, which is the boundary an off-by-one breaks; and a value above it is
// refused rather than clamped, because this enumeration reads a single page and a
// caller told it got 500 would have got 100.

//go:fix inline
func TestTheCapMatrix(t *testing.T) {
	for _, row := range []struct {
		name          string
		params        walk.Params
		wantRefusal   []string
		wantRuns      int64
		wantDeploysTo int64
	}{
		{
			name:   "unset takes this collector's defaults",
			params: walk.Params{}, wantRuns: 20, wantDeploysTo: 20,
		},
		{
			name:     "a value in range is honored exactly",
			params:   walk.Params{MaxPipelineRuns: new(7), MaxDeployments: new(9)},
			wantRuns: 7, wantDeploysTo: 9,
		},
		{
			name:     "the ceiling itself is honored",
			params:   walk.Params{MaxPipelineRuns: new(100), MaxDeployments: new(100)},
			wantRuns: 100, wantDeploysTo: 100,
		},
		{
			name:        "zero is refused rather than read as unset",
			params:      walk.Params{MaxPipelineRuns: new(0)},
			wantRefusal: []string{"max_pipeline_runs", "at least 1", "default of 20"},
		},
		{
			name:        "a negative value is refused",
			params:      walk.Params{MaxDeployments: new(-1)},
			wantRefusal: []string{"max_deployments", "-1", "at least 1"},
		},
		{
			name:        "a value above the ceiling is refused rather than clamped",
			params:      walk.Params{MaxPipelineRuns: new(500)},
			wantRefusal: []string{"max_pipeline_runs", "500", "100", "single page"},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			api := &recordingAPI{}
			got, err := walk.Collector{API: api.build}.Walk(
				context.Background(), fixtureGroup, row.params, framework.ForeignContext{})

			if len(row.wantRefusal) > 0 {
				if err == nil {
					t.Fatalf("the cap was accepted; the walk produced %d nodes", len(got.Nodes))
				}
				for _, want := range row.wantRefusal {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("the refusal does not name %q: %v", want, err)
					}
				}
				// NOTHING WAS DIALED. A bad parameter is refused before the provider
				// is built, so a refused collect costs the operator no round trip and
				// no credential read.
				if api.wasBuilt() {
					t.Error("the provider was built before the parameters were validated")
				}
				return
			}

			if err != nil {
				t.Fatalf("a valid cap was refused: %v", err)
			}
			api.mu.Lock()
			runs, deployments := api.runsPerPage, api.deploymentsPerPage
			api.mu.Unlock()
			if runs != row.wantRuns {
				t.Errorf("the runs listing asked for %d per page, want %d", runs, row.wantRuns)
			}
			if deployments != row.wantDeploysTo {
				t.Errorf("the deployments listing asked for %d per page, want %d",
					deployments, row.wantDeploysTo)
			}
		})
	}
}

// TestTheCollectIDIsValidatedOnceAndLoudly.
//
// The id is interpolated into every request path the walk builds AND into every
// node id it emits, so a malformed one passed through reaches the caller as seven
// per-enumeration errors, or worse as a walk that matched nothing and looked like
// an empty group.
//
// A GITLAB GROUP PATH MAY CARRY SLASHES, which is where this validator differs
// from the sibling collector's: `acme/platform` is an ordinary subgroup and must
// be accepted.
func TestTheCollectIDIsValidatedOnceAndLoudly(t *testing.T) {
	for _, row := range []struct {
		name, id    string
		wantRefusal []string
	}{
		{name: "a plain group", id: "acme"},
		{name: "a subgroup path", id: "acme/platform"},
		{name: "a deep subgroup path", id: "acme/platform/infra"},
		{name: "a name with the punctuation GitLab allows", id: "acme_co.2-group"},
		{name: "surrounding whitespace is trimmed", id: "  acme  "},
		{
			name: "empty", id: "",
			wantRefusal: []string{"no group was named", "nothing to fall back to"},
		},
		{
			name: "only whitespace", id: "   ",
			wantRefusal: []string{"no group was named"},
		},
		{
			name: "a URL", id: "https://gitlab.example.com/acme",
			wantRefusal: []string{"looks like a URL", "acme/platform"},
		},
		{
			name: "a leading slash", id: "/acme",
			wantRefusal: []string{"starts or ends with a slash"},
		},
		{
			name: "a trailing slash", id: "acme/",
			wantRefusal: []string{"starts or ends with a slash"},
		},
		{
			name: "an empty segment", id: "acme//platform",
			wantRefusal: []string{"empty path segment"},
		},
		{
			name: "a reserved suffix", id: "acme/api.git",
			wantRefusal: []string{".git", "reserves"},
		},
		{
			name: "a segment starting with a hyphen", id: "acme/-platform",
			wantRefusal: []string{"starts with"},
		},
		{
			name: "a character GitLab does not allow", id: "acme/plat form",
			wantRefusal: []string{"letters, digits, underscores", "pass the group alone"},
		},
		{
			// A SEGMENT BEGINNING WITH A MULTI-BYTE RUNE. The refusal is right —
			// a GitLab path segment is ASCII — and what this row pins is that it
			// quotes the character the caller actually typed. Reading the leading
			// BYTE instead names a character that is not in the id at all, which is
			// the one direction a refusal must not be wrong in.
			name: "a segment beginning with a multi-byte rune", id: "édition",
			wantRefusal: []string{"starts with", "'é'"},
		},
		{
			name: "a multi-byte rune in a later segment", id: "acme/日本",
			wantRefusal: []string{"starts with", "'日'"},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			api := &recordingAPI{}
			_, err := walk.Collector{API: api.build}.Walk(
				context.Background(), row.id, walk.Params{}, framework.ForeignContext{})

			if len(row.wantRefusal) == 0 {
				if err != nil {
					t.Fatalf("a valid group path was refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("a malformed collect id was walked rather than refused")
			}
			for _, want := range row.wantRefusal {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not name %q: %v", want, err)
				}
			}
			if api.wasBuilt() {
				t.Error("the provider was built before the collect id was validated")
			}
		})
	}
}

// TestATrimmedIDIsTheOneThatReachesTheNodeIDs. The trim is not cosmetic: it
// decides the string every node id in the graph is built from.
func TestATrimmedIDIsTheOneThatReachesTheNodeIDs(t *testing.T) {
	got := mustWalk(t, &recordingAPI{}, "  acme  ")
	rendered := render(t, got)
	if !strings.Contains(rendered, "gitlab:acme/Group/acme") {
		t.Errorf("the node ids were not built from the trimmed id:\n%s", rendered)
	}
	if strings.Contains(rendered, "gitlab:  acme") {
		t.Errorf("an untrimmed id reached the node ids:\n%s", rendered)
	}
}
