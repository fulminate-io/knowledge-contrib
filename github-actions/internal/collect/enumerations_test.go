// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"slices"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// enumerations_test.go — the input classes every enumeration meets: an empty
// organization, one repository with one of everything, the archived repository
// every enumeration must skip, and the two capped reads.

// TestAnEmptyOrganizationIsCompleteAndCarriesOnlyItself. An empty read is not an
// incomplete one, and this is the row that keeps those apart: an organization
// with no repositories yields the organization node and nothing else, and the
// enumerations report no incompleteness at all.
func TestAnEmptyOrganizationIsCompleteAndCarriesOnlyItself(t *testing.T) {
	repos := &fakeRepos{}
	actions := &fakeActions{}

	for _, name := range enumerationNames(collect.API{Repos: repos, Actions: actions}) {
		got, err := runOneAllowingError(t, repos, actions, name)
		if err != nil {
			t.Errorf("%s over an empty organization reported %v; an empty read is not an "+
				"incomplete one", name, err)
		}
		if len(got.Relations) != 0 {
			t.Errorf("%s over an empty organization emitted %d edges", name, len(got.Relations))
		}
		for _, res := range got.Resources {
			if res.ResourceType != ghgraph.ResourceTypeOrganization {
				t.Errorf("%s over an empty organization emitted a %q node (%s)",
					name, res.ResourceType, res.ID)
			}
		}
	}

	// The organization node itself IS emitted, unconditionally. Without this the
	// assertions above would pass on a walk that produced nothing at all.
	got := runOne(t, repos, actions, "github-repos")
	if ids := resourceIDs(got); ids["github:acme/Organization/acme"] != ghgraph.ResourceTypeOrganization {
		t.Errorf("the organization node is missing from an empty organization's walk: %v", ids)
	}
}

// TestTheArchivedRepositoryIsSkippedByEveryEnumeration. The fixture carries one
// archived repository beside two active ones, and nothing may reach it.
func TestTheArchivedRepositoryIsSkippedByEveryEnumeration(t *testing.T) {
	repos, actions := fixtureAPI()

	for _, name := range enumerationNames(collect.API{Repos: repos, Actions: actions}) {
		got, err := runOneAllowingError(t, repos, actions, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, res := range got.Resources {
			if strings.Contains(res.ID, "legacy") {
				t.Errorf("%s emitted %q, which belongs to the archived repository", name, res.ID)
			}
		}
		for _, rel := range got.Relations {
			if strings.Contains(rel.FromID, "legacy") || strings.Contains(rel.ToID, "legacy") {
				t.Errorf("%s emitted the edge %s -> %s, which names the archived repository",
					name, rel.FromID, rel.ToID)
			}
		}
	}

	// The known positive: the two ACTIVE repositories were emitted in the same
	// run, so this is a walk that skipped one rather than one that read nothing.
	ids := resourceIDs(runOne(t, repos, actions, "github-repos"))
	for _, want := range []string{"github:acme/Repository/acme/api", "github:acme/Repository/acme/web"} {
		if _, ok := ids[want]; !ok {
			t.Errorf("the active repository %q was not emitted either", want)
		}
	}
}

// TestARepositoryWithOneOfEverythingResolvesItsWholeChain is the single-repository
// row: every kind is emitted once and every BELONGS_TO in the chain resolves.
func TestARepositoryWithOneOfEverythingResolvesItsWholeChain(t *testing.T) {
	got := walkTheFixtureOrganization(t)
	ids := resourceIDs(got)

	for id, want := range map[string]string{
		"github:acme/Organization/acme":                          ghgraph.ResourceTypeOrganization,
		"github:acme/Repository/acme/api":                        ghgraph.ResourceTypeRepository,
		"github:acme/Workflow/acme/api/.github/workflows/ci.yml": ghgraph.ResourceTypeWorkflow,
		"github:acme/WorkflowRun/acme/api/100":                   ghgraph.ResourceTypeWorkflowRun,
		"github:acme/Runner/1":                                   ghgraph.ResourceTypeRunner,
		"github:acme/Environment/acme/api/production":            ghgraph.ResourceTypeEnvironment,
		"github:acme/Deployment/acme/api/500":                    ghgraph.ResourceTypeDeployment,
		"github:acme/Secret/acme/api/repo/API_KEY":               ghgraph.ResourceTypeSecret,
		"github:acme/User/ada":                                   ghgraph.ResourceTypeUser,
		"github:acme/Team/platform":                              ghgraph.ResourceTypeTeam,
		"github:acme/Label/self-hosted":                          ghgraph.ResourceTypeLabel,
	} {
		if ids[id] != want {
			t.Errorf("%q is a %q node, want %q", id, ids[id], want)
		}
	}
}

// TestTheDisabledWorkflowIsSkipped. A workflow that is not active runs nothing.
func TestTheDisabledWorkflowIsSkipped(t *testing.T) {
	repos, actions := fixtureAPI()
	got := runOne(t, repos, actions, "github-workflows")

	ids := resourceIDs(got)
	if _, present := ids["github:acme/Workflow/acme/api/.github/workflows/old.yml"]; present {
		t.Error("the disabled workflow was emitted")
	}
	if _, present := ids["github:acme/Workflow/acme/api/.github/workflows/ci.yml"]; !present {
		t.Error("the active workflow beside it was dropped too; the assertion above is not " +
			"measuring the state filter")
	}
}

// TestTheCapsHoldAtTheirDefaults is the cap row at the values a collect with no
// parameters uses.
func TestTheCapsHoldAtTheirDefaults(t *testing.T) {
	repos, actions := fixtureAPI()
	actions.runs["acme/api"] = &gogithub.WorkflowRuns{WorkflowRuns: manyRuns(25)}
	repos.deployments["acme/api"] = manyDeployments(40)

	runs := runOne(t, repos, actions, "github-workflow-runs")
	if got := countByTypeInRepo(runs, ghgraph.ResourceTypeWorkflowRun, fixtureAPIRepo); got != collect.DefaultMaxRuns {
		t.Errorf("a repository with 25 runs produced %d run nodes, want the default cap of %d",
			got, collect.DefaultMaxRuns)
	}
	// THE CAP IS PER REPOSITORY, and the second repository is what makes that
	// observable rather than asserted: its own single run is untouched by the
	// first repository's cap.
	if got := countByTypeInRepo(runs, ghgraph.ResourceTypeWorkflowRun, fixtureWebRepo); got != 1 {
		t.Errorf("the second repository produced %d run nodes, want its own 1 — the cap is per "+
			"repository, not per walk", got)
	}

	deployments := runOne(t, repos, actions, "github-deployments")
	if got := countByTypeInRepo(deployments, ghgraph.ResourceTypeDeployment, fixtureAPIRepo); got != collect.DefaultMaxDeployments {
		t.Errorf("a repository with 40 deployments produced %d deployment nodes, want the default "+
			"cap of %d", got, collect.DefaultMaxDeployments)
	}
}

// TestTheCapsAreHonoredExactlyAtEveryAdmittedValue drives the three admitted
// cells of the cap matrix over the enumerations themselves. The refusal cell
// lives with the parameter validation, which is where the refusal happens.
func TestTheCapsAreHonoredExactlyAtEveryAdmittedValue(t *testing.T) {
	for _, value := range []int{1, 7, 99, 100} {
		repos, actions := fixtureAPI()
		actions.runs["acme/api"] = &gogithub.WorkflowRuns{WorkflowRuns: manyRuns(150)}
		repos.deployments["acme/api"] = manyDeployments(150)

		runs := runOneWithCaps(t, repos, actions, "github-workflow-runs", collect.Caps{MaxRuns: value})
		if got := countByTypeInRepo(runs, ghgraph.ResourceTypeWorkflowRun, fixtureAPIRepo); got != value {
			t.Errorf("max_runs=%d produced %d run nodes for %s", value, got, fixtureAPIRepo)
		}
		deployments := runOneWithCaps(t, repos, actions, "github-deployments",
			collect.Caps{MaxDeployments: value})
		if got := countByTypeInRepo(deployments, ghgraph.ResourceTypeDeployment, fixtureAPIRepo); got != value {
			t.Errorf("max_deployments=%d produced %d deployment nodes for %s",
				value, got, fixtureAPIRepo)
		}
	}
}

// TestThePaginationBoundaryIsDecidedByTheProvider is the 99 / 100 / 101 row over
// every read that pages.
//
// EXACTLY 100 IS THE CELL THAT MATTERS. A full page is not the last page: the
// provider answers a full page with a next-page number, and a reader that stopped
// counting items would lose the whole tail of an organization with exactly one
// page of anything.
func TestThePaginationBoundaryIsDecidedByTheProvider(t *testing.T) {
	for _, count := range []int{99, 100, 101} {
		repos, actions := fixtureAPI()
		repos.repos = repositoryPages(count)

		got := runOne(t, repos, actions, "github-repos")
		// One organization node plus one per repository.
		if want := count + 1; len(got.Resources) != want {
			t.Errorf("%d repositories over %d page(s) produced %d resources, want %d",
				count, len(repos.repos), len(got.Resources), want)
		}
	}

	// The same boundary on the other paged reads, driven through the runners
	// enumeration, whose org-level read pages independently of the repositories.
	for _, count := range []int{99, 100, 101} {
		repos, actions := fixtureAPI()
		actions.orgRunners = runnerPages(count)

		got := runOne(t, repos, actions, "github-runners")
		orgRunners := 0
		for _, res := range got.Resources {
			if res.ResourceType == ghgraph.ResourceTypeRunner && res.Metadata["repo"] == "" {
				orgRunners++
			}
		}
		if orgRunners != count {
			t.Errorf("%d organization runners over %d page(s) produced %d runner nodes",
				count, len(actions.orgRunners), orgRunners)
		}
	}
}

// TestSplitFullNameHandlesAValueWithNoSeparator pins what a malformed repository
// name does. It is reached only through a provider answer this collector cannot
// produce itself, so the assertion is on the request it builds rather than on a
// refusal: the request fails and the scope is recorded, which is a partial read
// rather than a silent skip.
func TestSplitFullNameHandlesAValueWithNoSeparator(t *testing.T) {
	repos, actions := fixtureAPI()
	repos.repos = pages[gogithub.Repository]{{
		{FullName: new("nameless"), Name: new("nameless")},
	}}
	actions.workflowsErr = map[string]error{"nameless/": refusal(404, "Not Found")}

	got, err := runOneAllowingError(t, repos, actions, "github-workflows")
	if err == nil {
		t.Fatal("a repository name with no separator produced no incompleteness at all")
	}
	if !strings.Contains(err.Error(), "nameless") {
		t.Errorf("the incompleteness does not name the repository it happened on: %v", err)
	}
	if len(got.Resources) != 0 {
		t.Errorf("the failed read still produced %d resources", len(got.Resources))
	}
}

// manyRuns builds n recorded runs, each with a distinct id.
func manyRuns(n int) []*gogithub.WorkflowRun {
	out := make([]*gogithub.WorkflowRun, 0, n)
	for i := range n {
		out = append(out, &gogithub.WorkflowRun{
			ID:        new(int64(1000 + i)),
			Name:      new("CI"),
			RunNumber: new(i),
			Status:    new("completed"),
			Event:     new("push"),
			Path:      new(fixtureWorkflow),
		})
	}
	return out
}

// manyDeployments builds n recorded deployments, each with a distinct id.
func manyDeployments(n int) []*gogithub.Deployment {
	out := make([]*gogithub.Deployment, 0, n)
	for i := range n {
		out = append(out, &gogithub.Deployment{
			ID:          new(int64(2000 + i)),
			Environment: new(fixtureEnvironment),
			Ref:         new("main"),
		})
	}
	return out
}

// repositoryPages builds n repositories split into pages of 100, which is what
// the provider does.
func repositoryPages(n int) pages[gogithub.Repository] {
	var all []*gogithub.Repository
	for i := range n {
		name := "acme/repo-" + itoa(i)
		all = append(all, &gogithub.Repository{
			FullName: new(name),
			Name:     new("repo-" + itoa(i)),
		})
	}
	return chunk(all, 100)
}

// runnerPages builds n runners split into pages of 100.
func runnerPages(n int) pages[gogithub.Runner] {
	var all []*gogithub.Runner
	for i := range n {
		all = append(all, &gogithub.Runner{
			ID:     new(int64(100 + i)),
			Name:   new("runner-" + itoa(i)),
			Status: new("online"),
		})
	}
	return chunk(all, 100)
}

// chunk splits a recorded answer into provider-sized pages.
func chunk[T any](all []*T, size int) pages[T] {
	var out pages[T]
	for start := 0; start < len(all); start += size {
		out = append(out, slices.Clone(all[start:min(start+size, len(all))]))
	}
	return out
}

// itoa avoids a strconv import in a file that needs it for nothing else.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// TestTheCapDefaultsAreTheParityValues is the row that says WHAT the defaults
// are, as opposed to that they are applied.
//
// THE LITERALS ARE THE POINT AND THE CONSTANTS ARE NOT ADMISSIBLE HERE. Every
// other cap row asserts against collect.DefaultMaxRuns and
// collect.DefaultMaxDeployments, which are declared by the code under test — so
// the subject supplies its own answer key, and changing both constants leaves
// the whole suite green. Measured on this tree: with DefaultMaxRuns at 25 and
// DefaultMaxDeployments at 35, every package passed.
//
// WHY THAT MATTERS MORE THAN A TUNING KNOB WOULD. These two numbers are the
// SOURCE PROVIDER'S OWN defaults, carried across as one of the parity decisions
// this module is required to reproduce: a collect with no parameters must land
// the graph a consumer already has. A change to either is a parity departure to
// record, not a value to adjust — and it would also silently falsify the
// operator-facing table in this module's README and the two jsonschema
// descriptions the served tool advertises.
func TestTheCapDefaultsAreTheParityValues(t *testing.T) {
	if collect.DefaultMaxRuns != 10 {
		t.Errorf("the workflow-run cap defaults to %d; the source provider's own default is 10, "+
			"and a collect with no parameters must land the graph a consumer already has",
			collect.DefaultMaxRuns)
	}
	if collect.DefaultMaxDeployments != 20 {
		t.Errorf("the deployment cap defaults to %d; the source provider's own default is 20",
			collect.DefaultMaxDeployments)
	}
}

// TestEveryPaginationLoopReadsEveryPage extends the boundary row to the two loops
// no arm reached.
//
// FIVE LOOPS, NO SHARED HELPER. Each enumeration carries its own, so a boundary
// proven on one says nothing about the next. Measured on this tree before this
// row existed: replacing the break condition with an unconditional stop — which
// is the source provider's own defect, taking the first page and calling it the
// answer — left the environments loop and the secrets loop green, and the secrets
// read is one the what-to-test list names by name.
//
// THE 100 CELL IS THE ONE THAT MATTERS, here as in the repository row: a full
// page is not the last page. A reader that stopped on item count rather than on
// the provider's own next-page number loses the whole tail of an organization
// with exactly one page of anything, and every smaller fixture passes.
func TestEveryPaginationLoopReadsEveryPage(t *testing.T) {
	for _, count := range []int{99, 100, 101} {
		t.Run("secrets", func(t *testing.T) {
			repos, actions := fixtureAPI()
			actions.orgSecrets = secretPages(count, "ORG_SECRET_")

			got := runOne(t, repos, actions, "github-secrets")
			orgSecrets := 0
			for _, res := range got.Resources {
				if res.ResourceType == ghgraph.ResourceTypeSecret && res.Metadata["scope"] == "org" {
					orgSecrets++
				}
			}
			if orgSecrets != count {
				t.Errorf("%d organization secrets over %d page(s) produced %d secret nodes",
					count, len(actions.orgSecrets), orgSecrets)
			}
		})

		t.Run("environments", func(t *testing.T) {
			repos, actions := fixtureAPI()
			repos.environments["acme/api"] = environmentPages(count)

			got := runOne(t, repos, actions, "github-environments")
			if emitted := countByTypeInRepo(got, ghgraph.ResourceTypeEnvironment, fixtureAPIRepo); emitted != count {
				t.Errorf("%d environments over %d page(s) produced %d environment nodes",
					count, len(repos.environments["acme/api"]), emitted)
			}
		})
	}
}

// secretPages builds n recorded secrets split into pages of 100.
func secretPages(n int, prefix string) pages[gogithub.Secret] {
	var all []*gogithub.Secret
	for i := range n {
		all = append(all, &gogithub.Secret{Name: prefix + itoa(i), Visibility: "all"})
	}
	return chunk(all, 100)
}

// environmentPages builds n recorded environments split into pages of 100.
func environmentPages(n int) pages[gogithub.Environment] {
	var all []*gogithub.Environment
	for i := range n {
		all = append(all, &gogithub.Environment{
			ID:   new(int64(2000 + i)),
			Name: new("env-" + itoa(i)),
		})
	}
	return chunk(all, 100)
}
