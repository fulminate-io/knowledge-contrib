// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/walk"
)

// walk_test.go — the walk as a whole: the two phases, the completeness verdict,
// the foreign-context block and what the result does and does not carry.

// TestACleanWalkIsCompleteAndCarriesTheWholeGraph.
func TestACleanWalkIsCompleteAndCarriesTheWholeGraph(t *testing.T) {
	withCredentials(t)
	result, err := newFixture(t).collector().Walk(
		context.Background(), "acme", walk.Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("the clean walk: %v", err)
	}
	if !result.Complete.IsAsserted() {
		t.Fatal("the walk returned a zero Completeness, which asserts nothing and is refused " +
			"when the envelope is encoded")
	}
	if !result.Complete.IsComplete() {
		t.Errorf("a clean recorded walk reported itself incomplete: %s", result.Complete.Reason())
	}
	if len(result.Nodes) == 0 || len(result.Edges) == 0 {
		t.Fatalf("the walk produced %d nodes and %d edges", len(result.Nodes), len(result.Edges))
	}

	// ALL THREE JOINS RAN. Each of these edges names a node a DIFFERENT
	// enumeration read, so none of them can be emitted where the reference was
	// seen — which is the whole reason the joins happen after the fan-out. A join
	// that did not run leaves its edge class absent entirely.
	for _, want := range []struct{ edge, target, why string }{
		{"USES_SECRET", "bitbucket:acme/Variable/repository/api/API_KEY",
			"the pipeline's $API_KEY against the variables enumeration's own variables"},
		{"DEPLOYS_TO", "bitbucket:acme/Environment/api/production",
			"the step's deployment against the environments enumeration's own environments"},
		{"RUNS_IN", "bitbucket:acme/Label/self-hosted",
			"the step's runs-on label against the labels the runners enumeration minted"},
	} {
		var joined bool
		for _, edge := range result.Edges {
			if edge.Type == want.edge && edge.ToID == want.target {
				joined = true
			}
		}
		if !joined {
			t.Errorf("the %s join did not run: %s resolved to nothing", want.edge, want.why)
		}
	}

	// AND EVERY EDGE THE WALK EMITTED NAMES A NODE IT ALSO EMITTED, which is the
	// property the three joins exist for, asserted here over the walk's own
	// output rather than over an enumeration helper's.
	ids := map[string]bool{}
	for _, node := range result.Nodes {
		ids[node.ID] = true
	}
	for _, edge := range result.Edges {
		if !ids[edge.FromID] || !ids[edge.ToID] {
			t.Errorf("the edge %s %s -> %s names an endpoint no node carries",
				edge.Type, edge.FromID, edge.ToID)
		}
	}
}

// TestTheResultCarriesNoGraphIdentityOfItsOwn.
//
// FOR A REGISTERED FAMILY THE GRAPH NAME IS THE COLLECT ID, resolved on the
// client's side before this binary is called, and the contract's result type has
// no graph-identity field at all. The source provider built one — a function
// prefixing the id — and reproducing it would be a second definition of a
// graph's identity that the collect dispatch cannot see.
func TestTheResultCarriesNoGraphIdentityOfItsOwn(t *testing.T) {
	withCredentials(t)
	result, err := newFixture(t).collector().Walk(
		context.Background(), "acme", walk.Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("the walk: %v", err)
	}

	// The contract result carries three fields and this asserts it as the ENCODED
	// shape, which is what the framework puts on the wire.
	encoded, err := json.Marshal(struct {
		Nodes any `json:"nodes"`
		Edges any `json:"edges"`
	}{result.Nodes, result.Edges})
	if err != nil {
		t.Fatalf("encoding the result: %v", err)
	}
	for _, tell := range []string{"bitbucket-acme", "graph_name", "GraphName", "graph_type"} {
		if strings.Contains(string(encoded), tell) {
			t.Errorf("the result carries %q, which is a graph identity this collector does not "+
				"own", tell)
		}
	}
}

// TestANonEmptyForeignContextChangesNoByteOfTheOutput. The block carries what
// this collector's registration entry declared it needs from the operator's
// other graphs, and this entry declares nothing — so reading it could tell this
// walk nothing, and the `_` in the signature is a statement rather than an
// omission.
func TestANonEmptyForeignContextChangesNoByteOfTheOutput(t *testing.T) {
	withCredentials(t)
	fixture := newFixture(t)

	empty, err := fixture.collector().Walk(
		context.Background(), "acme", walk.Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("the walk with an empty block: %v", err)
	}
	populated, err := fixture.collector().Walk(context.Background(), "acme", walk.Params{},
		framework.ForeignContext{
			"code": {{
				GraphName: "knowledge",
				Nodes: []framework.ForeignNode{{
					ID: "cmd/main.go:main", Type: "function", SymbolName: "main",
				}},
				Edges: []framework.ForeignEdge{{FromID: "a", ToID: "b"}},
			}},
		})
	if err != nil {
		t.Fatalf("the walk with a populated block: %v", err)
	}

	if a, b := marshal(t, empty), marshal(t, populated); a != b {
		t.Errorf("a declared foreign context changed the output:\n%s\nvs\n%s", a, b)
	}
	// The known positive: the walk produced something, so the two are equal
	// because nothing changed rather than because both are empty.
	if len(empty.Nodes) == 0 {
		t.Fatal("the walk produced no nodes; the comparison above means nothing")
	}
}

// TestA404OnAListingMakesTheWalkIncompleteAndCarriesTheURL observes the rule at
// the layer it is stated in: a 404 on a listing scope is an incomplete WALK that
// names the URL.
//
// THE ENUMERATION'S ROW IS NOT THIS ROW. The enumeration package asserts that
// the 404 produces an error of the partial class carrying the path; that is the
// value at the seam, not the verdict an operator reads. What decides whether the
// receiving server may treat what this collect did not carry as deleted is
// [framework.Result].Complete, and the only thing that had driven a partial-class
// value across that seam was a refusal. A status that reaches the verdict only
// through a mapping nothing exercises end to end is a requirement observed one
// layer below where it is written.
func TestA404OnAListingMakesTheWalkIncompleteAndCarriesTheURL(t *testing.T) {
	withCredentials(t)
	fixture := newFixture(t)
	const path = "repositories/acme/api/pipelines_config/variables"
	fixture.status[path] = http.StatusNotFound

	result, err := fixture.collector().Walk(
		context.Background(), "acme", walk.Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("a 404 on one listing failed the whole walk: %v", err)
	}
	if result.Complete.IsComplete() {
		t.Fatal("a 404 on a listing produced a COMPLETE walk. A scope with nothing configured " +
			"answers with an empty page, so this walk asserted it had seen a scope it could not " +
			"read — which is what lets the server delete what the collect did not carry")
	}
	// THE URL SURVIVES THE WHOLE WAY UP. The provider's error type carries a
	// status and a body and no path, so the enumeration splices it in; a verdict
	// that dropped it again would leave an operator a 404 they cannot go and
	// reproduce.
	if !strings.Contains(result.Complete.Reason(), path) {
		t.Errorf("the walk's reason does not carry the URL %q: %s", path, result.Complete.Reason())
	}
	// AND THE REST OF THE WORKSPACE IS STILL CARRIED.
	if len(result.Nodes) == 0 {
		t.Error("the incomplete walk carried no nodes at all")
	}
}

// TestARefusedEnumerationMakesTheWalkIncompleteAndNamesIt is the walk-level half
// of the completeness matrix: the enumeration's own error reaches the verdict.
func TestARefusedEnumerationMakesTheWalkIncompleteAndNamesIt(t *testing.T) {
	withCredentials(t)
	fixture := newFixture(t)
	fixture.status["repositories/acme/api/pipelines_config/variables"] = http.StatusForbidden

	result, err := fixture.collector().Walk(
		context.Background(), "acme", walk.Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("a refused enumeration failed the whole walk: %v", err)
	}
	if result.Complete.IsComplete() {
		t.Fatal("a walk that could not read one repository's variables asserted it saw the whole " +
			"workspace. That assertion is what lets the receiving server treat what this walk did " +
			"not carry as deleted")
	}
	for _, want := range []string{"bitbucket-variables", "api", "acme"} {
		if !strings.Contains(result.Complete.Reason(), want) {
			t.Errorf("the reason does not name %q: %s", want, result.Complete.Reason())
		}
	}
	// THE REST OF THE WORKSPACE IS STILL THERE. A refusal on one enumeration may
	// not cost the operator the five that answered.
	if len(result.Nodes) == 0 {
		t.Error("the incomplete walk carried no nodes at all")
	}
}

// TestAFailureListingTheRepositoriesFailsTheWholeWalk is the arm that is NOT a
// partial read, and it is where this collector's phase structure shows.
//
// FIVE ENUMERATIONS START FROM THAT LISTING, so a walk that could not read it
// never found out what the workspace contains. Reporting that as an incomplete
// SUCCESS would land an empty generation the server then reconciles against.
//
// THE SIBLING CI/CD COLLECTOR REJECTED THIS COUPLING and has every enumeration
// list the repositories for itself. This module keeps the shared list because
// the parity floor names the two phases, because four of the five enumerations
// need each repository's main branch as well as its slug, and because the source
// provider records the shared list as its own decision — and the cost is exactly
// this row.
func TestAFailureListingTheRepositoriesFailsTheWholeWalk(t *testing.T) {
	withCredentials(t)
	fixture := newFixture(t)
	fixture.status["repositories/acme"] = http.StatusInternalServerError

	_, err := fixture.collector().Walk(
		context.Background(), "acme", walk.Params{}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("a walk that could not list the workspace's repositories returned a result")
	}
	for _, want := range []string{"acme", "repositories"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not name %q: %v", want, err)
		}
	}
	// AND THE FIVE LATER ENUMERATIONS NEVER RAN, which is the observable that the
	// phases really are ordered rather than merely written in order.
	for _, path := range []string{
		"repositories/acme/api/pipelines",
		"workspaces/acme/pipelines-config/runners",
		"repositories/acme/api/environments",
	} {
		if fixture.requests(path) != 0 {
			t.Errorf("%q was read after the repository listing failed", path)
		}
	}
}

// TestAWorkspaceWithNoRepositoriesStillAnchorsItsWorkspaceScopedResources is a
// DEFECT FOUND WHILE BUILDING THIS MODULE, and its fix.
//
// The repositories enumeration emits the workspace node only when the workspace
// has at least one repository, which is the source provider's behavior and is
// reproduced. The runners and variables enumerations read the workspace SCOPE
// directly, so a workspace with no repositories and one workspace-level runner
// produced a runner whose BELONGS_TO edge named a workspace node nothing ever
// created — the same defect class as the USES_SECRET target this module already
// fixes. The walk mints the root when there is something for it to be the root
// of.
func TestAWorkspaceWithNoRepositoriesStillAnchorsItsWorkspaceScopedResources(t *testing.T) {
	withCredentials(t)
	fixture := newFixture(t)
	withEmptyResponse(t, "repositories/acme")

	result, err := fixture.collector().Walk(
		context.Background(), "acme", walk.Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("a workspace with no repositories failed the walk: %v", err)
	}
	if !result.Complete.IsComplete() {
		t.Errorf("a workspace with no repositories was reported incomplete: %s",
			result.Complete.Reason())
	}

	ids := map[string]bool{}
	for _, node := range result.Nodes {
		ids[node.ID] = true
	}
	if !ids["bitbucket:acme/Workspace/acme"] {
		t.Error("the workspace-level runner's BELONGS_TO edge names a workspace node the walk " +
			"never created")
	}
	for _, edge := range result.Edges {
		if !ids[edge.ToID] || !ids[edge.FromID] {
			t.Errorf("the edge %s -> %s names an endpoint no node carries", edge.FromID, edge.ToID)
		}
	}
	// The known positive: the workspace-scoped runner really was emitted, so the
	// root above is anchoring something rather than standing alone.
	if !ids["bitbucket:acme/Runner/{runner}"] {
		t.Error("the workspace-level runner was not emitted at all")
	}
}

// TestAWorkspaceWithNOTHINGInItCollectsToZeroNodes is the other side of the same
// rule, and it is the source provider's behavior carried forward: the root is
// minted when there is something for it to be the root OF, and not otherwise.
func TestAWorkspaceWithNOTHINGInItCollectsToZeroNodes(t *testing.T) {
	withCredentials(t)
	fixture := newFixture(t)
	withEmptyResponse(t, "repositories/acme")
	withEmptyResponse(t, "workspaces/acme/pipelines-config/runners")
	withEmptyResponse(t, "workspaces/acme/pipelines-config/variables")

	result, err := fixture.collector().Walk(
		context.Background(), "acme", walk.Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("an empty workspace failed the walk: %v", err)
	}
	if !result.Complete.IsComplete() {
		t.Errorf("an empty workspace was reported incomplete: %s", result.Complete.Reason())
	}
	if len(result.Nodes) != 0 {
		t.Errorf("an empty workspace produced %d nodes, want none", len(result.Nodes))
	}
}

// TestACancelledWalkIsIncompleteAndSaysSo.
func TestACancelledWalkIsIncompleteAndSaysSo(t *testing.T) {
	withCredentials(t)
	fixture := newFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// The repository listing must succeed so the fan-out is reached; the
	// cancellation lands on the second phase, and it lands WHILE a second-phase
	// request is in flight: the fixture cancels from inside its handler on the
	// first request below the listing, before that response is written. A
	// goroutine spinning on the listing's count raced the walk instead, and on
	// a fast runner the whole fan-out finished before the cancel landed, so the
	// walk truthfully reported a complete workspace.
	var once sync.Once
	fixture.mu.Lock()
	fixture.onRequest = func(path string) {
		if path != "repositories/acme" && strings.HasPrefix(path, "repositories/acme/") {
			once.Do(cancel)
		}
	}
	fixture.mu.Unlock()

	result, err := fixture.collector().Walk(ctx, "acme", walk.Params{}, framework.ForeignContext{})
	if err != nil {
		// A cancellation caught by the repository listing itself is a hard error,
		// which is the other legal outcome of this race.
		if !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("the cancelled walk failed for another reason: %v", err)
		}
		return
	}
	if result.Complete.IsComplete() {
		t.Error("a cancelled walk asserted it saw the whole workspace")
	}
}

// TestTheToolIsNamedByTheFrameworksDefaultAndCarriesADescription.
func TestTheToolIsNamedByTheFrameworksDefaultAndCarriesADescription(t *testing.T) {
	spec := walk.Collector{}.Tool()
	if spec.Name != "" {
		t.Errorf("the collector names its tool %q; an empty name takes the framework's default, "+
			"which is what the config entry's own example names", spec.Name)
	}
	if spec.Description == "" {
		t.Error("the served tool carries no description; an operator installing it reads that")
	}
	for _, want := range []string{"Bitbucket", "workspace"} {
		if !strings.Contains(spec.Description, want) {
			t.Errorf("the description does not name %q: %q", want, spec.Description)
		}
	}
}

// TestNoStringInTheModuleClaimsTheBuiltInFamilyName is asserted at the walk
// because that is where a graph name would be built if this collector had one.
func TestNoStringInTheModuleClaimsTheBuiltInFamilyName(t *testing.T) {
	withCredentials(t)
	result, err := newFixture(t).collector().Walk(
		context.Background(), "acme", walk.Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("the walk: %v", err)
	}
	for _, node := range result.Nodes {
		if node.Metadata["provider"] == "cicd" {
			t.Errorf("%s: the provider metadata is the built-in graph type name", node.ID)
		}
	}
	// The `source` field IS "cicd", deliberately: it names the DOMAIN rather than
	// the family, and this is the known positive that the assertion above is
	// about the provider key and not about the string.
	if len(result.Nodes) > 0 && result.Nodes[0].Source != "cicd" {
		t.Errorf("the source tag is %q, want the domain tag %q", result.Nodes[0].Source, "cicd")
	}
}

// marshal renders a result as the bytes the envelope carries.
func marshal(t *testing.T, result framework.Result) string {
	t.Helper()
	raw, err := json.Marshal(struct {
		Nodes any `json:"nodes"`
		Edges any `json:"edges"`
	}{result.Nodes, result.Edges})
	if err != nil {
		t.Fatalf("marshaling a result: %v", err)
	}
	return string(raw)
}
