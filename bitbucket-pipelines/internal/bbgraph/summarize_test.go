// SPDX-License-Identifier: Apache-2.0

package bbgraph_test

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// summarize_test.go — one summarizer per declared resource type, and the fix to
// the one the source provider got wrong.

// TestEveryDeclaredResourceTypeHasItsOwnSummary is the nine-summarizer floor as
// a property: no declared type falls through to the undeclared-type marker, and
// no two types produce the same sentence for the same node.
func TestEveryDeclaredResourceTypeHasItsOwnSummary(t *testing.T) {
	seen := map[string]string{}
	for _, resourceType := range bbgraph.ResourceTypes() {
		summary := bbgraph.Summarize(bbgraph.Resource{
			ID:           "bitbucket:acme/Thing/x",
			Name:         "thing",
			ResourceType: resourceType,
			Metadata:     map[string]string{"workspace": "acme"},
		})
		if summary == "" {
			t.Errorf("%s: the summary is empty", resourceType)
		}
		if strings.Contains(summary, "undeclared type") {
			t.Errorf("%s: it summarized through the undeclared-type arm: %q", resourceType, summary)
		}
		if other, dup := seen[summary]; dup {
			t.Errorf("%s and %s produce the identical summary %q, so one of them is not "+
				"registered", resourceType, other, summary)
		}
		seen[summary] = resourceType
	}
}

// TestAnUndeclaredTypeSummarizesAsAVisibleMarker. It is unreachable through the
// walk — the builder refuses such a type first — so it is written as something a
// reader would notice rather than as a plausible sentence.
func TestAnUndeclaredTypeSummarizesAsAVisibleMarker(t *testing.T) {
	summary := bbgraph.Summarize(bbgraph.Resource{
		Name: "thing", ResourceType: "not-a-declared-type",
	})
	if !strings.Contains(summary, "undeclared type") {
		t.Errorf("an undeclared type summarized as %q, which reads like a real summary", summary)
	}
}

// TestTheScopeSuffixTakesBothArmsAndNeither.
func TestTheScopeSuffixTakesBothArmsAndNeither(t *testing.T) {
	for _, row := range []struct {
		metadata map[string]string
		want     string
	}{
		{map[string]string{"workspace": "acme", "repo": "api"}, "(acme/api)"},
		{map[string]string{"workspace": "acme"}, "(acme)"},
		{map[string]string{}, ""},
	} {
		summary := bbgraph.Summarize(bbgraph.Resource{
			Name: "thing", ResourceType: bbgraph.ResourceTypeRunner, Metadata: row.metadata,
		})
		if row.want == "" {
			if strings.Contains(summary, "(") {
				t.Errorf("a resource with no workspace carries a scope suffix: %q", summary)
			}
			continue
		}
		if !strings.HasSuffix(summary, row.want) {
			t.Errorf("the summary %q does not end in %q", summary, row.want)
		}
	}
}

// TestThePipelineRunSummaryCarriesTheStatusTheConverterWrites is the fix.
//
// THE SOURCE PROVIDER'S SUMMARIZER READS `state` AND `result`, AND ITS CONVERTER
// WRITES NEITHER: it writes the flattened outcome under `status`. So every
// pipeline-run summary that provider produced silently omitted the run's
// outcome, which is the one thing an operator scanning a run list wants. This
// summarizer reads the key the converter actually writes.
func TestThePipelineRunSummaryCarriesTheStatusTheConverterWrites(t *testing.T) {
	summary := bbgraph.Summarize(bbgraph.Resource{
		Name:         "api #41",
		ResourceType: bbgraph.ResourceTypePipelineRun,
		Metadata: map[string]string{
			"workspace": "acme", "repo": "api", "status": "SUCCESSFUL", "branch": "trunk",
		},
	})
	for _, want := range []string{"api #41", "status=SUCCESSFUL", "branch=trunk", "(acme/api)"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the run summary %q does not carry %q", summary, want)
		}
	}

	// The control: a summarizer reading the source provider's own keys would
	// produce the same sentence for a run carrying them under `state`, and this
	// one must NOT — otherwise it is reading both and the fix is untested.
	stale := bbgraph.Summarize(bbgraph.Resource{
		Name:         "api #41",
		ResourceType: bbgraph.ResourceTypePipelineRun,
		Metadata:     map[string]string{"workspace": "acme", "state": "COMPLETED"},
	})
	if strings.Contains(stale, "COMPLETED") {
		t.Errorf("the run summary reads the source provider's own `state` key, which its "+
			"converter never wrote: %q", stale)
	}
}

// TestThePipelineSummaryCarriesItsTrigger, both ways: the default pipeline has
// no trigger key and carries no trigger clause.
func TestThePipelineSummaryCarriesItsTrigger(t *testing.T) {
	withTrigger := bbgraph.Summarize(bbgraph.Resource{
		Name: "api/branches/trunk", ResourceType: bbgraph.ResourceTypePipeline,
		Metadata: map[string]string{"workspace": "acme", "repo": "api", "trigger_key": "trunk"},
	})
	if !strings.Contains(withTrigger, "trigger=trunk") {
		t.Errorf("the pipeline summary %q does not carry its trigger key", withTrigger)
	}

	withoutTrigger := bbgraph.Summarize(bbgraph.Resource{
		Name: "api/default", ResourceType: bbgraph.ResourceTypePipeline,
		Metadata: map[string]string{"workspace": "acme", "repo": "api", "trigger_key": ""},
	})
	if strings.Contains(withoutTrigger, "trigger=") {
		t.Errorf("the default pipeline's summary carries an empty trigger clause: %q",
			withoutTrigger)
	}
}
