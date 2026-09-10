// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// pipelines_parse_test.go — the pipeline definition's own input classes.
//
// THE SOURCE PROVIDER'S SUITE COVERS NONE OF THIS. It has no test of the parse,
// of the five trigger sections, of the reference extraction or of the built-in
// filter, so this file is where this module's suite exceeds its parity target
// rather than mirroring it.

// TestAllFiveTriggerSectionsBecomePipelines is the coverage row for the parse.
func TestAllFiveTriggerSectionsBecomePipelines(t *testing.T) {
	got := walkTheFixture(t)

	var pipelines []string
	for _, res := range got.Resources {
		if res.ResourceType == bbgraph.ResourceTypePipeline {
			pipelines = append(pipelines, res.ID)
		}
	}
	slices.Sort(pipelines)

	want := []string{
		"bitbucket:acme/Pipeline/api/branches/trunk",
		"bitbucket:acme/Pipeline/api/custom/nightly",
		"bitbucket:acme/Pipeline/api/default",
		"bitbucket:acme/Pipeline/api/pull-requests/**",
		"bitbucket:acme/Pipeline/api/tags/v*",
	}
	if !slices.Equal(pipelines, want) {
		t.Errorf("the parse produced the pipelines\n  %v\nwant\n  %v", pipelines, want)
	}
}

// TestTheTriggerKeyIsTheMapKeyAndIsEmptyForTheDefaultPipeline.
func TestTheTriggerKeyIsTheMapKeyAndIsEmptyForTheDefaultPipeline(t *testing.T) {
	got := walkTheFixture(t)
	for id, want := range map[string]string{
		"bitbucket:acme/Pipeline/api/default":          "",
		"bitbucket:acme/Pipeline/api/branches/trunk":   "trunk",
		"bitbucket:acme/Pipeline/api/pull-requests/**": "**",
		"bitbucket:acme/Pipeline/api/custom/nightly":   "nightly",
		"bitbucket:acme/Pipeline/api/tags/v*":          "v*",
	} {
		pipeline, ok := resourceByID(got, id)
		if !ok {
			t.Errorf("no pipeline node %q", id)
			continue
		}
		if pipeline.Metadata["trigger_key"] != want {
			t.Errorf("%s: trigger_key is %q, want %q", id, pipeline.Metadata["trigger_key"], want)
		}
	}
}

// TestAnEntryWithNoStepKeyIsSkipped. A `parallel` or `stage` entry is a grouping
// rather than a step: it carries no script, so there is nothing to convert, and
// this collector does not descend into it.
//
// THE OBSERVABLE IS THE REFERENCE IT WOULD HAVE CARRIED. The fixture's parallel
// entry names $IGNORED_BY_PARALLEL and nothing else does, so a collector that
// descended into it would record that name as an unresolved reference.
func TestAnEntryWithNoStepKeyIsSkipped(t *testing.T) {
	got := walkTheFixture(t)
	envelope := marshal(t, got.Resources) + marshal(t, got.Relations)
	if strings.Contains(envelope, "IGNORED_BY_PARALLEL") {
		t.Error("a `parallel` entry's step was descended into; this collector converts the steps " +
			"a pipeline declares directly, as the source provider does")
	}
	// The known positive: the SIBLING step in the same pipeline was converted, so
	// this is one entry skipped rather than the whole pipeline dropped.
	if !strings.Contains(envelope, "MISSING_VAR") {
		t.Error("the pipeline's direct step was dropped too; the absence above means nothing")
	}
}

// TestBothReferenceFormsAreExtracted. `$VAR` and `${VAR}` are the two spellings
// a script uses, and the fixture carries one of each.
func TestBothReferenceFormsAreExtracted(t *testing.T) {
	got := walkTheFixture(t)
	// $API_KEY is the bare form; ${WORKSPACE_TOKEN} is the braced one.
	for _, want := range []string{
		"bitbucket:acme/Variable/repository/api/API_KEY",
		"bitbucket:acme/Variable/workspace/WORKSPACE_TOKEN",
	} {
		if !hasRelation(got, "bitbucket:acme/Pipeline/api/default", want, bbgraph.EdgeUsesSecret) {
			t.Errorf("the reference to %q was not extracted", want)
		}
	}
}

// TestAReferenceRepeatedInOneStepIsOneEdge. The extraction deduplicates within a
// step, so a name mentioned on three script lines is one relationship and not
// three.
func TestAReferenceRepeatedInOneStepIsOneEdge(t *testing.T) {
	fixture := newFixture(t)
	fixture.raw["repositories/acme/api/src/trunk/bitbucket-pipelines.yml"] = `
pipelines:
  default:
    - step:
        name: repeated
        script:
          - echo "$API_KEY"
          - echo "$API_KEY again"
          - echo "${API_KEY} once more"
`
	got, err := walkTheFixtureAllowingError(t, fixture, nil)
	if err != nil {
		t.Fatalf("the walk: %v", err)
	}
	var edges int
	for _, rel := range got.Relations {
		if rel.Type == bbgraph.EdgeUsesSecret &&
			rel.ToID == "bitbucket:acme/Variable/repository/api/API_KEY" {
			edges++
		}
	}
	if edges != 1 {
		t.Errorf("a reference repeated three times in one step produced %d edges, want 1", edges)
	}
}

// TestAPipelineDefinitionThatIsNotYAMLIsAnIncompleteRead. It is not a 404 and it
// is not a clean empty: the repository HAS a definition and this collector could
// not read it, so the edges its steps declare are missing and the walk says so.
func TestAPipelineDefinitionThatIsNotYAMLIsAnIncompleteRead(t *testing.T) {
	fixture := newFixture(t)
	fixture.raw["repositories/acme/api/src/trunk/bitbucket-pipelines.yml"] =
		"pipelines:\n  default:\n  - step: [this is not a step body\n"

	_, err := runOne(t, fixture, "bitbucket-pipelines-config")
	if err == nil {
		t.Fatal("a definition that does not parse produced a clean read, so the relationships it " +
			"declares are missing with nothing saying so")
	}
	if !strings.Contains(err.Error(), "api") {
		t.Errorf("the reason does not name the repository: %v", err)
	}
}

// TestAnEmptyDefinitionIsCleanAndEmpty. A file that parses to no pipelines is a
// repository with the file checked in and nothing declared in it, which is a true
// reading rather than a failure.
func TestAnEmptyDefinitionIsCleanAndEmpty(t *testing.T) {
	fixture := newFixture(t)
	fixture.raw["repositories/acme/api/src/trunk/bitbucket-pipelines.yml"] = ""

	got, err := runOne(t, fixture, "bitbucket-pipelines-config")
	if err != nil {
		t.Fatalf("an empty definition was reported as a failure: %v", err)
	}
	if len(got.Resources) != 0 {
		t.Errorf("an empty definition emitted %d resources", len(got.Resources))
	}
}

// TestARepositoryWithNoDefinitionIsCleanAndEmpty is the 404 arm at this
// converter: the provider answers 404 for a file that is not there.
func TestARepositoryWithNoDefinitionIsCleanAndEmpty(t *testing.T) {
	fixture := newFixture(t)
	fixture.status["repositories/acme/api/src/trunk/bitbucket-pipelines.yml"] = http.StatusNotFound

	got, err := runOne(t, fixture, "bitbucket-pipelines-config")
	if err != nil {
		t.Fatalf("a repository with no pipeline definition was reported as a failure: %v", err)
	}
	for _, res := range got.Resources {
		if strings.Contains(res.ID, "/Pipeline/api/") {
			t.Errorf("a repository with no definition emitted %q", res.ID)
		}
	}
}
