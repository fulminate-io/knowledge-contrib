// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// discovery_completeness_test.go — the SHARED PROJECT DISCOVERY's own outcomes,
// which belong to no single enumeration.
//
// THE SPLIT IN THIS FILE IS THE ONE THE DISCOVERY MAKES. Failing to list the
// GROUP'S OWN projects means six enumerations never found out what the group
// contains, so it fails them all; failing to list ONE SUBGROUP of many, or
// stopping at the recursion limit, means the discovery learned most of it and is
// a partial read. The per-enumeration read outcomes are in completeness_test.go.

// TestAFailureListingTheGroupsProjectsFailsEveryEnumeration is the arm that is
// NOT a partial read. Six enumerations start from that discovery, so a walk that
// could not read it never found out what the group contains.
func TestAFailureListingTheGroupsProjectsFailsEveryEnumeration(t *testing.T) {
	api := fixtureAPI()
	api.groupProjectsErr = map[string]error{
		fixtureGroup: refusal(http.StatusInternalServerError, "500 Server Error"),
	}

	_, errs, _ := runAll(t, api)
	for name, err := range errs {
		if err == nil {
			t.Errorf("%s survived a failure to list the group's projects", name)
			continue
		}
		if errors.Is(err, collect.ErrDenied) || errors.Is(err, collect.ErrPartial) {
			t.Errorf("%s reported a failure to list the group's projects as a partial read: %v",
				name, err)
		}
	}
}

// TestASubgroupThatCannotBeListedIsAPartialDiscovery is the level BELOW that one:
// one subgroup of many failing means the discovery learned most of it.
//
// THE SOURCE PROVIDER WARNS AND CONTINUES HERE and its walk still asserts
// complete, so the projects under that subgroup — and everything in them —
// silently disappear from the graph.
func TestASubgroupThatCannotBeListedIsAPartialDiscovery(t *testing.T) {
	api := fixtureAPI()
	api.groupProjectsErr = map[string]error{
		fixtureSubgroup: refusal(http.StatusForbidden, "403 Forbidden"),
	}

	got, errs, listerPartial := runAll(t, api)
	for name, err := range errs {
		if err != nil {
			t.Errorf("%s failed over a subgroup this collector could not list: %v", name, err)
		}
	}
	if len(listerPartial) != 1 {
		t.Fatalf("the project discovery reported %d gaps, want exactly 1: %v",
			len(listerPartial), listerPartial)
	}
	if !strings.Contains(listerPartial[0], fixtureSubgroup) {
		t.Errorf("the gap does not name the subgroup it could not read: %q", listerPartial[0])
	}
	// The top-level projects are still there, which is what makes this a partial
	// discovery rather than a failed one.
	if _, ok := resourceIDs(got)["gitlab:acme/Project/acme/api"]; !ok {
		t.Error("one unreadable subgroup cost the walk the group's own projects")
	}
}

// TestASubgroupTreeDeeperThanTheLimitIsReportedRatherThanTruncatedSilently.
//
// The source provider logs a warning and returns an empty list, so every project
// below the limit vanishes from a walk that still reports itself complete.
func TestASubgroupTreeDeeperThanTheLimitIsReportedRatherThanTruncatedSilently(t *testing.T) {
	api := deepSubgroupAPI(12)

	_, errs, listerPartial := runAll(t, api)
	for name, err := range errs {
		if err != nil {
			t.Errorf("%s failed over a deep subgroup tree: %v", name, err)
		}
	}
	if len(listerPartial) == 0 {
		t.Fatal("a subgroup tree deeper than the recursion limit was truncated with nothing " +
			"saying so, which is the source provider's own behavior and the defect this row exists " +
			"to prevent")
	}
	joined := strings.Join(listerPartial, " | ")
	if !strings.Contains(joined, "deeper than") {
		t.Errorf("the gap does not say the tree was too deep: %q", joined)
	}
	// The known positive: a tree WITHIN the limit reports nothing.
	if _, _, shallow := runAll(t, deepSubgroupAPI(3)); len(shallow) != 0 {
		t.Errorf("a subgroup tree within the limit also reported gaps (%v), so the row above is "+
			"not measuring the limit", shallow)
	}
}

// deepSubgroupAPI builds a group whose subgroups nest `depth` levels, each
// carrying one project.
func deepSubgroupAPI(depth int) *fakeAPI {
	api := &fakeAPI{
		groupProjects: map[string]pages[gl.Project]{},
		subgroups:     map[string]pages[gl.Group]{},
		runnerDetails: map[int64]*gl.RunnerDetails{},
	}
	path := fixtureGroup
	for level := range depth {
		child := fmt.Sprintf("%s/level%d", path, level)
		api.subgroups[path] = pages[gl.Group]{{{ID: int64(100 + level), FullPath: child}}}
		api.groupProjects[child] = pages[gl.Project]{{{
			ID:                int64(200 + level),
			Name:              fmt.Sprintf("p%d", level),
			PathWithNamespace: child + "/p",
			Visibility:        gl.PrivateVisibility,
		}}}
		path = child
	}
	return api
}

// TestARunnerDetailFailureKeepsTheRunnerAndItsParentAndLosesItsTags is the split
// the emission order forces, asserted explicitly rather than left for a reader to
// assume the whole runner is dropped.
//
// The source provider reports this one at DEBUG and returns, so at default
// verbosity a runner's whole tag set disappears with no signal at all.
func TestARunnerDetailFailureKeepsTheRunnerAndItsParentAndLosesItsTags(t *testing.T) {
	api := fixtureAPI()
	api.runnerDetailsErr = map[int64]error{1: refusal(http.StatusForbidden, "403 Forbidden")}

	got, err := runOneAllowingError(t, api, "gitlab-runners")
	if err == nil {
		t.Fatal("a runner whose details could not be read produced a clean result")
	}
	if !strings.Contains(err.Error(), "runner 1") {
		t.Errorf("the reason does not name the runner whose tags were lost: %v", err)
	}

	const runner = "gitlab:acme/Runner/1"
	if _, ok := resourceIDs(got)[runner]; !ok {
		t.Error("the runner node was dropped along with its tags")
	}
	if !hasRelation(got, runner, "gitlab:acme/Group/acme", glgraph.EdgeBelongsTo) {
		t.Error("the runner lost its parent edge too; the node and the edge are emitted before " +
			"the detail read, so a detail failure costs the tags and nothing else")
	}
	for _, rel := range got.Relations {
		if rel.FromID == runner && rel.Type == glgraph.EdgeHasLabel {
			t.Errorf("a HAS_LABEL edge survived a detail read that failed: %s -> %s",
				rel.FromID, rel.ToID)
		}
	}
	// The known positive: the OTHER runners still have their tags, so this is one
	// runner's loss rather than the enumeration's.
	if !hasRelation(got, "gitlab:acme/Runner/2", "gitlab:acme/RunnerTag/docker", glgraph.EdgeHasLabel) {
		t.Error("the other runners lost their tags too")
	}
}

// TestAProtectedEnvironmentsFailureLosesOnlyTheApprovalEdges is the second
// DEBUG-level swallow in the source provider.
func TestAProtectedEnvironmentsFailureLosesOnlyTheApprovalEdges(t *testing.T) {
	api := fixtureAPI()
	api.protectedEnvsErr = map[int64]error{apiID: refusal(http.StatusForbidden, "403 Forbidden")}

	got, err := runOneAllowingError(t, api, "gitlab-environments")
	if err == nil {
		t.Fatal("a protected-environments read that failed produced a clean result; every " +
			"approval edge for that project is then missing with nothing saying so")
	}
	if !strings.Contains(err.Error(), "protected environments") {
		t.Errorf("the reason does not name the read that failed: %v", err)
	}
	if _, ok := resourceIDs(got)["gitlab:acme/Environment/acme/api/production"]; !ok {
		t.Error("the environments themselves were lost; only the protection rules failed")
	}
	if countByType(got, glgraph.ResourceTypeProtectionRule) != 0 {
		t.Error("a protection rule survived a read that failed")
	}
}

// TestAJobListingFailureKeepsItsRun. The run was enumerated and only its contents
// were unreadable.
func TestAJobListingFailureKeepsItsRun(t *testing.T) {
	api := fixtureAPI()
	api.jobsErr = map[string]error{"1/100": refusal(http.StatusInternalServerError, "500")}

	got, err := runOneAllowingError(t, api, "gitlab-pipeline-runs")
	if err == nil {
		t.Fatal("a job listing that failed produced a clean result")
	}
	if !strings.Contains(err.Error(), "pipeline run 100") {
		t.Errorf("the reason does not name the run whose jobs were lost: %v", err)
	}
	if _, ok := resourceIDs(got)["gitlab:acme/PipelineRun/acme/api/100"]; !ok {
		t.Error("the run node was dropped along with its jobs")
	}
	if countByType(got, glgraph.ResourceTypeJob) != 0 {
		t.Error("a job survived a listing that failed")
	}
}

// TestBothClassesAtOnceAreReportedAsBoth. An enumeration refused one project and
// unable to reach another has two different things wrong with it, and an operator
// fixes them differently.
func TestBothClassesAtOnceAreReportedAsBoth(t *testing.T) {
	api := fixtureAPI()
	api.projectVarErr = map[int64]error{
		apiID: refusal(http.StatusForbidden, "403 Forbidden"),
		webID: refusal(http.StatusInternalServerError, "500 Server Error"),
	}

	_, err := runOneAllowingError(t, api, "gitlab-variables")
	if err == nil {
		t.Fatal("two failing projects produced a clean read")
	}
	if !errors.Is(err, collect.ErrDenied) {
		t.Errorf("the refusal was lost: %v", err)
	}
	if !errors.Is(err, collect.ErrPartial) {
		t.Errorf("the failure was lost: %v", err)
	}
	for _, want := range []string{apiPath, webPath} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the reason does not name %q: %v", want, err)
		}
	}
}
