// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"fmt"
	"testing"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// pagination_boundary_test.go — ONE ROW PER PAGING READ, and there are EIGHT.
//
// WHY EIGHT ROWS AND NOT ONE. Each loop in this collector carries its own break
// condition, so coverage of one transfers to none of the others: a stop-after-page-
// one mutation in six of them left the whole module suite green, and a group whose
// project list, subgroup list, runner list, group variables or job list is exactly
// one full page long would have lost its whole tail while the walk reported
// COMPLETE. That is the same class the completeness work exists to prevent,
// reached by a different route.
//
// THE EIGHT ARE ENUMERATED FROM THE SOURCE, not from memory. Every loop ends on
// the provider's own next-page marker, so the census is one call:
//
//	ast(operation:"match", language:"go", pattern:"resp.NextPage == 0",
//	    package_prefixes:["cmd/collectors/gitlab-ci"], include_tests:false)
//	-> total 7
//	   internal/collect/environments.go:76     environments
//	   internal/collect/variables.go:77        group variables
//	   internal/collect/variables.go:109       project variables
//	   internal/collect/project_lister.go:175  group projects
//	   internal/collect/project_lister.go:192  subgroups
//	   internal/collect/runners.go:119         runners, BOTH scopes
//	   internal/collect/pipeline_runs.go:170   jobs under a pipeline run
//
// SEVEN SYNTACTIC LOOPS CARRY EIGHT LOGICAL READS: the runners loop is shared by
// the group scope and the project scope, which reach it through different callers
// with different parents, so both are driven here. A row's `site` names the loop
// it covers, so a failure says which one.
//
// EACH ROW ASSERTS THREE OBSERVABLES, and each catches a different way a loop can
// be wrong while its output looks right.
//
//   - THE NODE COUNT: everything the read returned reached the graph.
//   - THE REQUEST COUNT: 99 items in one page and 100 items in one page produce
//     identical nodes and differ only in whether a second request was made, so a
//     reader that stopped on a short page rather than on the provider's marker is
//     invisible to the node count alone.
//   - THE REQUESTED PAGE SIZE: a read that asked for seven items a page returns
//     exactly the same nodes over more round trips, so the size reaches neither
//     the graph nor the request count at these sizes. It reaches the provider.
//
// The three arms are 99 (one page, one request), exactly 100 (one FULL page that
// is still the last, one request) and 101 (two pages, two requests).
//
// AND THE FIXTURE PAGES AT THE PRODUCTION SIZE, not at a number of its own: the
// recorded answers are split at [fixturePageSize], which the page-size assertion
// pins to what the collector asks for. A fixture that split at its own constant
// would keep passing while production asked for something else entirely.

// fixturePageSize is the page size this collector asks the provider for, and the
// size the recorded answers are split at so the arms line up with the real one.
//
// IT IS PRODUCTION'S OWN CONSTANT, READ, NOT A LITERAL BESIDE IT. Written as its
// own 100 this would be a second, independent number: change the collector's page
// size and the arm named "exactly 100 items, one full page that is still the last"
// would stop being a full page in production terms, and the table would keep
// passing while the boundary it exists for went unexercised. Reading the real one
// makes the arms follow it, and makes the per-read size assertion below a
// comparison against the collector's own choice rather than against a copy of it.
// See export_test.go for why the value crosses the package boundary that way.
const fixturePageSize = collect.PageSizeForTest

// paginate splits a flat recorded answer into pages of the provider's own
// maximum, which is what [pages.page] then serves in the provider's next-page
// convention.
func paginate[T any](items []*T) pages[T] {
	var out pages[T]
	for start := 0; start < len(items); start += fixturePageSize {
		out = append(out, items[start:min(start+fixturePageSize, len(items))])
	}
	return out
}

// pagingRead is one of the eight reads, with everything needed to drive it at a
// chosen size and read back both observables.
type pagingRead struct {
	// name is the read, in the words the finding and the source use.
	name string
	// site is the loop this row covers, so a red names the code rather than only
	// the test.
	site string
	// enumeration is the subcollector that performs the read.
	enumeration string
	// arrange builds a provider whose read under test answers exactly count items
	// and whose every other read answers nothing.
	arrange func(count int) *fakeAPI
	// emitted counts the nodes this read produced.
	emitted func(glgraph.Result) int
	// logged is the name the recorded provider logs this read under, which is how
	// the row reads back the request count and the requested page size.
	logged string
}

// pagingReads is the eight, in the order the census reports their loops.
func pagingReads() []pagingRead {
	return []pagingRead{
		{
			name: "group projects", site: "project_lister.go:175", enumeration: "gitlab-projects",
			arrange: func(n int) *fakeAPI {
				api := emptyProvider()
				api.groupProjects[fixtureGroup] = paginate(makeProjects(n, fixtureGroup))
				return api
			},
			emitted: func(got glgraph.Result) int { return countByType(got, glgraph.ResourceTypeProject) },
			logged:  scoped(readGroupProjects, fixtureGroup),
		},
		{
			name: "subgroups", site: "project_lister.go:192", enumeration: "gitlab-projects",
			arrange: func(n int) *fakeAPI {
				api := emptyProvider()
				subgroups := make([]*gl.Group, 0, n)
				for i := range n {
					path := fmt.Sprintf("%s/sub%03d", fixtureGroup, i)
					subgroups = append(subgroups, &gl.Group{ID: int64(900 + i), FullPath: path})
					// ONE PROJECT PER SUBGROUP, so the subgroup count is observable as
					// a project count. A subgroup carrying nothing would leave this row
					// asserting over an empty result.
					api.groupProjects[path] = paginate(makeProjects(1, path))
				}
				api.subgroups[fixtureGroup] = paginate(subgroups)
				return api
			},
			emitted: func(got glgraph.Result) int { return countByType(got, glgraph.ResourceTypeProject) },
			logged:  scoped(readSubgroups, fixtureGroup),
		},
		{
			name: "environments", site: "environments.go:76", enumeration: "gitlab-environments",
			arrange: func(n int) *fakeAPI {
				api := oneProjectProvider()
				environments := make([]*gl.Environment, 0, n)
				for i := range n {
					environments = append(environments, &gl.Environment{
						ID: int64(1000 + i), Name: fmt.Sprintf("env%03d", i), State: "available",
					})
				}
				api.environments = map[int64]pages[gl.Environment]{apiID: paginate(environments)}
				return api
			},
			emitted: func(got glgraph.Result) int {
				return countByType(got, glgraph.ResourceTypeEnvironment)
			},
			logged: scoped(readEnvironments, apiID),
		},
		{
			name: "group runners", site: "runners.go:119 (group scope)", enumeration: "gitlab-runners",
			arrange: func(n int) *fakeAPI {
				api := emptyProvider()
				api.groupRunners = paginate(makeRunners(api, 0, n))
				return api
			},
			emitted: func(got glgraph.Result) int { return countByType(got, glgraph.ResourceTypeRunner) },
			logged:  readGroupRunners,
		},
		{
			name: "project runners", site: "runners.go:119 (project scope)", enumeration: "gitlab-runners",
			arrange: func(n int) *fakeAPI {
				api := oneProjectProvider()
				api.projectRunners = map[int64]pages[gl.Runner]{
					apiID: paginate(makeRunners(api, 5000, n)),
				}
				return api
			},
			emitted: func(got glgraph.Result) int { return countByType(got, glgraph.ResourceTypeRunner) },
			logged:  scoped(readProjectRunners, apiID),
		},
		{
			name: "group variables", site: "variables.go:77", enumeration: "gitlab-variables",
			arrange: func(n int) *fakeAPI {
				api := emptyProvider()
				variables := make([]*gl.GroupVariable, 0, n)
				for i := range n {
					variables = append(variables, &gl.GroupVariable{Key: fmt.Sprintf("GROUP_VAR_%03d", i)})
				}
				api.groupVars = paginate(variables)
				return api
			},
			emitted: func(got glgraph.Result) int { return countByType(got, glgraph.ResourceTypeVariable) },
			logged:  readGroupVariables,
		},
		{
			name: "project variables", site: "variables.go:109", enumeration: "gitlab-variables",
			arrange: func(n int) *fakeAPI {
				api := oneProjectProvider()
				api.projectVars = map[int64]pages[gl.ProjectVariable]{
					apiID: paginate(makeProjectVariables(n)),
				}
				return api
			},
			emitted: func(got glgraph.Result) int { return countByType(got, glgraph.ResourceTypeVariable) },
			logged:  scoped(readProjectVars, apiID),
		},
		{
			name: "jobs under a pipeline run", site: "pipeline_runs.go:170",
			enumeration: "gitlab-pipeline-runs",
			arrange: func(n int) *fakeAPI {
				api := oneProjectProvider()
				api.pipelines = map[int64][]*gl.PipelineInfo{
					apiID: {{ID: fixtureRunID, Status: "success", Ref: "main"}},
				}
				jobs := make([]*gl.Job, 0, n)
				for i := range n {
					jobs = append(jobs, &gl.Job{
						ID: int64(9000 + i), Name: fmt.Sprintf("job%03d", i), Stage: "build",
					})
				}
				api.jobs = map[string]pages[gl.Job]{"1/100": paginate(jobs)}
				return api
			},
			emitted: func(got glgraph.Result) int { return countByType(got, glgraph.ResourceTypeJob) },
			logged:  scoped(readJobs, "1/100"),
		},
	}
}

// TestEveryPagingReadStopsWhenTheProviderSaysSoAndNotWhenThePageIsShort is the
// eight-row boundary.
func TestEveryPagingReadStopsWhenTheProviderSaysSoAndNotWhenThePageIsShort(t *testing.T) {
	// THE ARMS ARE DERIVED FROM THE PAGE SIZE, not written as three numbers beside
	// it. The boundary this table is named for is "one full page", and a full page
	// is whatever the collector asks for — so if that size changes, the arms move
	// with it and keep testing the boundary instead of testing three numbers that
	// used to be one.
	arms := []struct {
		name         string
		items        int
		wantRequests int
	}{
		{fmt.Sprintf("%d items, one short page", fixturePageSize-1), fixturePageSize - 1, 1},
		// THE ARM THAT MATTERS. A FULL page that is also the LAST one: the provider
		// says so with its next-page marker, and a reader that stopped on a short
		// page instead makes a second request here for nothing — producing the
		// identical nodes, which is why the request count is asserted beside them.
		{
			fmt.Sprintf("exactly %d items, one full page that is still the last", fixturePageSize),
			fixturePageSize, 1,
		},
		{
			fmt.Sprintf("%d items across two pages", fixturePageSize+1),
			fixturePageSize + 1, 2,
		},
	}

	reads := pagingReads()
	// THE ANTI-VACUITY FLOOR ON THE TABLE ITSELF. The census over the source finds
	// seven loops carrying eight logical reads; a table that lost a row would run
	// its remaining rows perfectly and say nothing about the read it dropped.
	if len(reads) != 8 {
		t.Fatalf("the table drives %d reads and the source carries 8; re-run the census in this "+
			"file's header and add the row it names", len(reads))
	}

	for _, read := range reads {
		for _, arm := range arms {
			t.Run(read.name+", "+arm.name, func(t *testing.T) {
				api := read.arrange(arm.items)
				got := runOne(t, api, read.enumeration)

				if emitted := read.emitted(got); emitted != arm.items {
					t.Errorf("%s emitted %d node(s) from %d item(s) (%s)",
						read.name, emitted, arm.items, read.site)
				}
				asked := api.asked(read.logged)
				if asked.requests != arm.wantRequests {
					t.Errorf("%s took %d request(s) for %d item(s), want %d — the provider's "+
						"next-page marker is what ends the paging, not the item count (%s)",
						read.name, asked.requests, arm.items, arm.wantRequests, read.site)
				}
				// THE PAGE SIZE THE COLLECTOR ASKED FOR. It never reaches the graph:
				// a read that asked for seven items a page returns exactly the same
				// nodes over more round trips, so no node count and no request count
				// at these sizes can see it. It reaches the PROVIDER, which is where
				// this reads it back.
				if asked.perPage != fixturePageSize {
					t.Errorf("%s asked for %d items a page, want the provider's own maximum of "+
						"%d — a smaller page is the same items in more round trips, and it is the "+
						"collector's own constant this compares against rather than a copy (%s)",
						read.name, asked.perPage, fixturePageSize, read.site)
				}
			})
		}
	}
}
