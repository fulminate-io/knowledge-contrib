// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"fmt"

	gl "gitlab.com/gitlab-org/api/client-go"
)

// pagination_fixtures_test.go — the purpose-built providers the boundary rows
// drive.
//
// EACH ROW BUILDS ITS OWN, rather than mutating the shared recorded corpus, and
// that is what makes its counts exact: a provider carrying only the read under
// test emits exactly the nodes that read produced, so a node count is an
// assertion rather than an arithmetic exercise over everything else the corpus
// happens to contain.

// emptyProvider is a provider with a group that contains nothing. Each row fills
// in only the read it is about, so its counts are exact.
func emptyProvider() *fakeAPI {
	return &fakeAPI{
		groupProjects: map[string]pages[gl.Project]{fixtureGroup: {{}}},
		subgroups:     map[string]pages[gl.Group]{},
		runnerDetails: map[int64]*gl.RunnerDetails{},
	}
}

// oneProjectProvider is a group with exactly one project, for the reads that are
// per-project.
func oneProjectProvider() *fakeAPI {
	api := emptyProvider()
	api.groupProjects[fixtureGroup] = paginate(makeProjects(1, fixtureGroup))
	return api
}

// makeProjects builds n projects under a group path, numbered so their ids and
// paths are distinct.
func makeProjects(n int, group string) []*gl.Project {
	out := make([]*gl.Project, 0, n)
	for i := range n {
		id := apiID
		if group != fixtureGroup || i > 0 {
			id = int64(10_000 + len(group)*1_000 + i)
		}
		out = append(out, &gl.Project{
			ID:                id,
			Name:              fmt.Sprintf("p%03d", i),
			PathWithNamespace: fmt.Sprintf("%s/p%03d", group, i),
			Visibility:        gl.PrivateVisibility,
		})
	}
	return out
}

// makeRunners builds n runners and registers empty details for each, so the
// runner nodes are emitted and no tag rides along to confuse a count.
func makeRunners(api *fakeAPI, first, n int) []*gl.Runner {
	out := make([]*gl.Runner, 0, n)
	for i := range n {
		id := int64(first + i + 1)
		out = append(out, &gl.Runner{ID: id, Status: "online"})
		api.runnerDetails[id] = &gl.RunnerDetails{ID: id}
	}
	return out
}

// makeProjectVariables builds n project-scoped variables.
func makeProjectVariables(n int) []*gl.ProjectVariable {
	out := make([]*gl.ProjectVariable, 0, n)
	for i := range n {
		out = append(out, &gl.ProjectVariable{Key: fmt.Sprintf("VAR_%03d", i)})
	}
	return out
}
