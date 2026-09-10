// SPDX-License-Identifier: Apache-2.0

package glgraph_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// summarize_test.go — ELEVEN declared types and ELEVEN registered summarizers.
//
// THE COUNT IS THE FLOOR AND THE FALL-THROUGH IS THE TELL. The source provider
// registers one summarizer per resource type, so a module that reproduced its
// resource types and not its summarizers would ship a whole class of node whose
// summary is the generic "<kind> <name>" — collected, indexed and useless to read.
// Both halves are asserted: that every declared type has its own function, and
// that a type nothing registered still gets something rather than an empty line.

// TestEveryDeclaredTypeHasItsOwnSummarizer is the floor.
func TestEveryDeclaredTypeHasItsOwnSummarizer(t *testing.T) {
	summarized := glgraph.SummarizedTypes()
	slices.Sort(summarized)

	declared := glgraph.ResourceTypes()
	slices.Sort(declared)

	if !slices.Equal(summarized, declared) {
		t.Errorf("the registered summarizers are %v and the declared types are %v; a declared type "+
			"with no summarizer ships nodes whose summary nobody wrote", summarized, declared)
	}
	if len(declared) != 11 {
		t.Errorf("%d resource types are declared, and the parity floor is 11", len(declared))
	}
}

// TestEveryDeclaredTypeProducesANonEmptySummary drives each one, because a
// registered function that returned nothing would satisfy the count above.
func TestEveryDeclaredTypeProducesANonEmptySummary(t *testing.T) {
	for _, resourceType := range glgraph.ResourceTypes() {
		got := glgraph.Summarize(glgraph.Resource{
			ID: "id", Name: "thing", ResourceType: resourceType,
		})
		if strings.TrimSpace(got) == "" {
			t.Errorf("the summary of a %q is empty", resourceType)
		}
		if !strings.Contains(got, "thing") {
			t.Errorf("the summary of a %q does not name the resource: %q", resourceType, got)
		}
		if !strings.HasPrefix(got, "GitLab ") {
			t.Errorf("the summary of a %q is %q; the source provider's own lines all name the "+
				"provider first", resourceType, got)
		}
	}
}

// TestTheSummariesReproduceTheSourceProvidersOwnLines, by literal, for the shapes
// that carry more than the kind and the name.
func TestTheSummariesReproduceTheSourceProvidersOwnLines(t *testing.T) {
	for _, row := range []struct {
		name string
		res  glgraph.Resource
		want string
	}{
		{
			"a group, which carries nothing to scope it",
			glgraph.Resource{Name: "acme", ResourceType: glgraph.ResourceTypeGroup},
			"GitLab group acme",
		},
		{
			"a project inside its group",
			glgraph.Resource{
				Name: "api", ResourceType: glgraph.ResourceTypeProject,
				Metadata: map[string]string{"path_with_namespace": "acme/api"},
			},
			"GitLab project api",
		},
		{
			"an environment, scoped by its project",
			glgraph.Resource{
				Name: "production", ResourceType: glgraph.ResourceTypeEnvironment,
				Metadata: map[string]string{"project": "acme/api"},
			},
			"GitLab environment production (acme/api)",
		},
		{
			"a pipeline run, with its status and ref",
			glgraph.Resource{
				Name: "pipeline #100", ResourceType: glgraph.ResourceTypePipelineRun,
				Metadata: map[string]string{"status": "success", "ref": "main", "project": "acme/api"},
			},
			"GitLab pipeline run pipeline #100 status=success ref=main (acme/api)",
		},
		{
			"a protection rule, with its approval count",
			glgraph.Resource{
				Name: "production protection", ResourceType: glgraph.ResourceTypeProtectionRule,
				Metadata: map[string]string{
					"required_approval_count": "2", "environment": "production", "project": "acme/api",
				},
			},
			"GitLab protection rule production protection approvals=2 env=production (acme/api)",
		},
		{
			"a project-scoped variable with both flags",
			glgraph.Resource{
				Name: "API_KEY", ResourceType: glgraph.ResourceTypeVariable,
				Metadata: map[string]string{
					"scope": "project", "project": "acme/api", "protected": "true", "masked": "true",
				},
			},
			"GitLab variable API_KEY scope=project protected masked (acme/api)",
		},
		{
			"a group-scoped variable, which carries neither flag",
			glgraph.Resource{
				Name: "GROUP_DEPLOY_TOKEN", ResourceType: glgraph.ResourceTypeVariable,
				Metadata: map[string]string{"scope": "group", "group": "acme"},
			},
			"GitLab variable GROUP_DEPLOY_TOKEN scope=group (acme)",
		},
		{
			"a runner tag, which carries nothing at all",
			glgraph.Resource{Name: "docker", ResourceType: glgraph.ResourceTypeRunnerTag},
			"GitLab runner tag docker",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := glgraph.Summarize(row.res); got != row.want {
				t.Errorf("the summary is %q, want %q", got, row.want)
			}
		})
	}
}

// TestAnUnregisteredTypeFallsThroughRatherThanProducingNothing. The fall-through
// exists so a resource type nobody registered still has something a keyword
// search can match; no declared type reaches it, which the row above is what
// keeps true.
func TestAnUnregisteredTypeFallsThroughRatherThanProducingNothing(t *testing.T) {
	if got := glgraph.Summarize(glgraph.Resource{Name: "thing", ResourceType: "widget"}); got != "widget thing" {
		t.Errorf("an unregistered type summarizes as %q", got)
	}
	if got := glgraph.Summarize(glgraph.Resource{Name: "thing"}); got != "<unknown> thing" {
		t.Errorf("a resource with no type summarizes as %q; a leading space would leave downstream "+
			"keyword search nothing to match", got)
	}
}

// TestTheDisplayLineIsCappedAtTheSourceProvidersOwnLength.
//
// WHAT IS CAPPED IS THE DISPLAY LINE AND NOTHING ELSE. A node's content is
// carried whole and is what a summarizer reads; this bounds the short human-
// readable line beside the id, which is the same artifact the source provider
// capped at the same length.
func TestTheDisplayLineIsCappedAtTheSourceProvidersOwnLength(t *testing.T) {
	long := strings.Repeat("x", 900)
	got := glgraph.Summarize(glgraph.Resource{Name: long, ResourceType: glgraph.ResourceTypeProject})
	if len(got) != 500 {
		t.Errorf("a 900-character name summarized to %d bytes, want the 500-byte cap", len(got))
	}
	// The control: a short line is not truncated.
	short := glgraph.Summarize(glgraph.Resource{Name: "api", ResourceType: glgraph.ResourceTypeProject})
	if short != "GitLab project api" {
		t.Errorf("a short summary was altered: %q", short)
	}
}
