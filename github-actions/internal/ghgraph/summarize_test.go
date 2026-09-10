// SPDX-License-Identifier: Apache-2.0

package ghgraph_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// summarize_test.go — the eleven registered summary lines, and the census that
// says there are eleven.

// TestEveryDeclaredTypeHasItsOwnSummarizer is the census. A declared type with no
// registered function falls through to a generic "<kind> <name>", which is the
// shape a node gets when nobody thought about it — and a materialized node class
// added to the vocabulary and nowhere else would show up exactly that way.
func TestEveryDeclaredTypeHasItsOwnSummarizer(t *testing.T) {
	registered := ghgraph.SummarizedTypes()
	slices.Sort(registered)
	declared := ghgraph.ResourceTypes()
	slices.Sort(declared)

	if !slices.Equal(registered, declared) {
		t.Errorf("the registered summarizers and the declared vocabulary differ:\n"+
			"registered: %v\ndeclared:   %v", registered, declared)
	}
	if len(declared) != 11 {
		t.Errorf("this collector declares %d resource types, want 11 — the source provider's eight "+
			"plus the three materialized classes", len(declared))
	}
}

// TestNoDeclaredTypeFallsThroughToTheGenericSummary is the census's behavioral
// half: the count agreeing is one thing, and every type actually producing its
// OWN line is another.
//
// THE GENERIC SHAPE IS "<kind> <name>", so a type that fell through would produce
// a summary starting with its own resource-type string. None does.
func TestNoDeclaredTypeFallsThroughToTheGenericSummary(t *testing.T) {
	for _, resourceType := range ghgraph.ResourceTypes() {
		got := ghgraph.Summarize(ghgraph.Resource{
			ID: "id", Name: "thing", ResourceType: resourceType,
			Metadata: map[string]string{"org": "acme", "repo": "acme/api"},
		})
		if got == resourceType+" thing" {
			t.Errorf("%q falls through to the generic summary %q", resourceType, got)
		}
		if !strings.HasPrefix(got, "GitHub ") {
			t.Errorf("%q produced %q, which does not read as this provider's own line",
				resourceType, got)
		}
	}

	// THE KNOWN POSITIVE for the matcher: a type nothing registers DOES fall
	// through, so the absence above is a measurement rather than an assertion
	// nothing could fail.
	if got := ghgraph.Summarize(ghgraph.Resource{
		ID: "id", Name: "thing", ResourceType: "unregistered",
	}); got != "unregistered thing" {
		t.Errorf("an unregistered type produced %q, so the fall-through this row detects does not "+
			"look the way it assumes", got)
	}
}

// TestTheSummaryLinesReproduceTheSourceProvidersOwn is the parity half, written
// as literals: a consumer's stored node carries these exact strings.
func TestTheSummaryLinesReproduceTheSourceProvidersOwn(t *testing.T) {
	for _, row := range []struct {
		res  ghgraph.Resource
		want string
	}{
		{
			ghgraph.Resource{Name: "myorg", ResourceType: ghgraph.ResourceTypeOrganization},
			"GitHub organization myorg",
		},
		{
			ghgraph.Resource{
				Name: "o/r", ResourceType: ghgraph.ResourceTypeRepository,
				Metadata: map[string]string{"visibility": "public", "default_branch": "main"},
			},
			"GitHub repository o/r visibility=public default=main",
		},
		{
			ghgraph.Resource{
				Name: "CI", ResourceType: ghgraph.ResourceTypeWorkflow,
				Metadata: map[string]string{"path": ".github/workflows/ci.yml", "repo": "o/r"},
			},
			"GitHub workflow CI path=.github/workflows/ci.yml (o/r)",
		},
		{
			ghgraph.Resource{
				Name: "CI #1", ResourceType: ghgraph.ResourceTypeWorkflowRun,
				Metadata: map[string]string{
					"status": "completed", "conclusion": "success", "event": "push", "repo": "o/r",
				},
			},
			"GitHub workflow run CI #1 status=completed conclusion=success event=push (o/r)",
		},
		{
			ghgraph.Resource{Name: "r", ResourceType: ghgraph.ResourceTypeRunner},
			"GitHub runner r",
		},
		{
			ghgraph.Resource{
				Name: "prod", ResourceType: ghgraph.ResourceTypeEnvironment,
				Metadata: map[string]string{"org": "o", "repo": "o/r"},
			},
			"GitHub environment prod (o/o/r)",
		},
		{
			ghgraph.Resource{
				Name: "deploy/o/r/prod", ResourceType: ghgraph.ResourceTypeDeployment,
				Metadata: map[string]string{"environment": "prod", "ref": "main", "repo": "o/r"},
			},
			"GitHub deployment deploy/o/r/prod env=prod ref=main (o/r)",
		},
		{
			ghgraph.Resource{
				Name: "TOKEN", ResourceType: ghgraph.ResourceTypeSecret,
				Metadata: map[string]string{"scope": "org", "org": "o"},
			},
			"GitHub secret TOKEN scope=org (o)",
		},
		{
			ghgraph.Resource{
				Name: "ada", ResourceType: ghgraph.ResourceTypeUser,
				Metadata: map[string]string{"org": "o"},
			},
			"GitHub user ada (o)",
		},
		{
			ghgraph.Resource{
				Name: "platform", ResourceType: ghgraph.ResourceTypeTeam,
				Metadata: map[string]string{"org": "o"},
			},
			"GitHub reviewer team platform (o)",
		},
		{
			ghgraph.Resource{
				Name: "self-hosted", ResourceType: ghgraph.ResourceTypeLabel,
				Metadata: map[string]string{"org": "o"},
			},
			"GitHub runner label self-hosted (o)",
		},
	} {
		t.Run(row.res.ResourceType, func(t *testing.T) {
			if got := ghgraph.Summarize(row.res); got != row.want {
				t.Errorf("got %q, want %q", got, row.want)
			}
		})
	}
}

// TestAnAbsentMetadataValueLeavesNoDanglingKeyOrDoubleSpace. A summary is a
// display line and half these fields are optional on a real resource.
func TestAnAbsentMetadataValueLeavesNoDanglingKeyOrDoubleSpace(t *testing.T) {
	for _, resourceType := range ghgraph.ResourceTypes() {
		got := ghgraph.Summarize(ghgraph.Resource{Name: "thing", ResourceType: resourceType})
		if strings.Contains(got, "  ") {
			t.Errorf("%q with no metadata produced %q, which carries a double space",
				resourceType, got)
		}
		if strings.HasSuffix(got, "=") || strings.Contains(got, "()") {
			t.Errorf("%q with no metadata produced %q, which carries an empty field",
				resourceType, got)
		}
		if got != strings.TrimSpace(got) {
			t.Errorf("%q produced %q, which is not trimmed", resourceType, got)
		}
	}
}

// TestTheDisplayLineIsCappedAtTheSourceProvidersOwnLength. The cap is on the
// node's Summary FIELD and on nothing else: a node's content — what a summarizer
// reads — is carried whole.
func TestTheDisplayLineIsCappedAtTheSourceProvidersOwnLength(t *testing.T) {
	long := strings.Repeat("n", 900)
	got := ghgraph.Summarize(ghgraph.Resource{
		Name: long, ResourceType: ghgraph.ResourceTypeOrganization,
	})
	if len(got) != 500 {
		t.Errorf("a 900-character name produced a %d-byte summary, want the source provider's own "+
			"500-byte cap", len(got))
	}

	// And the content is NOT capped, which is the distinction this collector's
	// summarize file exists to state: what a summarizer reads is the content.
	nodes, _, err := ghgraph.Build(ghgraph.Result{Resources: []ghgraph.Resource{{
		ID: "id", Name: "n", ResourceType: ghgraph.ResourceTypeOrganization,
		Content: strings.Repeat("c", 5000),
	}}})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if len(nodes[0].Content) != 5000 {
		t.Errorf("a 5000-byte content was carried as %d bytes; nothing here may truncate what a "+
			"summarizer reads", len(nodes[0].Content))
	}
}
