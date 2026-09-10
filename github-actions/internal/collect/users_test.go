// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"encoding/json"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// users_test.go — THE PEOPLE THE WALKED OBJECTS NAME, and the one field of theirs
// that may never reach the graph.
//
// THE SOURCE PROVIDER READ EVERY ONE OF THESE FIELDS AND DROPPED ALL OF THEM. A
// run's actor, a run's triggering actor and a deployment's creator ride responses
// this collector already fetches, so a graph without them is a CI/CD inventory
// that cannot answer who ran anything — while the calls to find out have already
// been paid for.
//
// EVERY ASSERTION BELOW IS BY LITERAL ID ON BOTH ENDS, on the same terms as the
// BELONGS_TO fan-in row: a test that built its expectation from the id helpers
// would agree with them however wrong both were.

// TestEveryUserBearingFieldMintsItsUserNode is R1: each of the four fields that
// names a person produces a node keyed on the login, carrying the numeric id, the
// User/Bot kind and the provider URL.
func TestEveryUserBearingFieldMintsItsUserNode(t *testing.T) {
	nodes := builtNodesByID(t)

	for _, want := range []struct {
		what   string
		id     string
		detail string
	}{
		{
			// The run's actor. It is ALSO the environment's reviewer, so this row
			// additionally says which of the two minted copies survives
			// deduplication: the one carrying more of the provider's fields.
			"a workflow run's actor",
			"github:acme/User/ada",
			`{"kind":"User","name":"ada","id":7,"html_url":"https://github.com/ada"}`,
		},
		{
			"a workflow run's triggering actor",
			"github:acme/User/dependabot[bot]",
			`{"kind":"Bot","name":"dependabot[bot]","id":49699333,` +
				`"html_url":"https://github.com/apps/dependabot"}`,
		},
		{
			"a deployment's creator",
			"github:acme/User/grace",
			`{"kind":"User","name":"grace","id":9,"html_url":"https://github.com/grace"}`,
		},
	} {
		node, ok := nodes[want.id]
		if !ok {
			t.Errorf("%s minted no node with the id %q", want.what, want.id)
			continue
		}
		if node.Metadata["resource_type"] != ghgraph.ResourceTypeUser {
			t.Errorf("%q is a %q node, want %q",
				want.id, node.Metadata["resource_type"], ghgraph.ResourceTypeUser)
		}
		if node.Content != want.detail {
			t.Errorf("%s (%s) carries the detail\n  %s\nwant\n  %s",
				want.what, want.id, node.Content, want.detail)
		}
	}

	// The team reviewer beside them is untouched, which is what keeps this row a
	// statement about the user class rather than about the reviewer path being
	// rewritten underneath it.
	if node, ok := nodes["github:acme/Team/platform"]; !ok {
		t.Error("the team reviewer node is gone")
	} else if node.Content != `{"kind":"Team","name":"platform"}` {
		t.Errorf("the team node's detail changed: %s", node.Content)
	}
}

// TestEachUserBearingFieldCarriesItsOwnEdgeClass is R2: three fields, three
// classes, each resolving at both ends.
//
// WHY THE CLASSES ARE ASSERTED SEPARATELY. A single class for all three would
// satisfy any row that only asked whether a run reaches a person, and would then
// make a re-run — the one case where a run's two actors differ — unreadable in the
// graph. Naming the pairs pins which field produced which edge.
func TestEachUserBearingFieldCarriesItsOwnEdgeClass(t *testing.T) {
	got := walkTheFixtureOrganization(t)
	nodes := resourceIDs(got)

	for _, want := range []struct {
		what, from, to, edgeType string
	}{
		{
			"the run's actor",
			"github:acme/WorkflowRun/acme/api/100",
			"github:acme/User/ada",
			ghgraph.EdgeAttributedTo,
		},
		{
			"the run's triggering actor",
			"github:acme/WorkflowRun/acme/api/100",
			"github:acme/User/dependabot[bot]",
			ghgraph.EdgeInitiatedBy,
		},
		{
			"the deployment's creator",
			"github:acme/Deployment/acme/api/500",
			"github:acme/User/grace",
			ghgraph.EdgeCreatedBy,
		},
	} {
		if !hasRelation(got, want.from, want.to, want.edgeType) {
			t.Errorf("%s: no %s edge from %q to %q", want.what, want.edgeType, want.from, want.to)
		}
		// BOTH ENDPOINTS RESOLVE, which is the observable this whole class exists
		// for: the source provider emitted approval edges naming nodes it never
		// created, and a person edge naming nothing would be the same failure.
		if _, ok := nodes[want.from]; !ok {
			t.Errorf("%s: the source node %q is not in the same result", want.what, want.from)
		}
		if _, ok := nodes[want.to]; !ok {
			t.Errorf("%s: the target node %q is not in the same result", want.what, want.to)
		}
	}

	// AND NO CLASS REACHES BEYOND ITS OWN FIELD. Each is counted over the whole
	// walk, so a class emitted from a second site nobody meant to add is a red
	// rather than a silent addition.
	for _, want := range []struct {
		edgeType string
		count    int
	}{
		{ghgraph.EdgeAttributedTo, 1},
		{ghgraph.EdgeInitiatedBy, 1},
		{ghgraph.EdgeCreatedBy, 1},
	} {
		emitted := 0
		for _, rel := range got.Relations {
			if rel.Type == want.edgeType {
				emitted++
			}
		}
		if emitted != want.count {
			t.Errorf("the walk emitted %d %s edges, want %d — this fixture names one person per "+
				"field and the second run names nobody", emitted, want.edgeType, want.count)
		}
	}
}

// TestTriggeredByStillNamesTheRepositoryAndCarriesItsEvent is the regression row
// for the class this change deliberately did NOT touch.
//
// THE REQUIREMENT AS FIRST WRITTEN READ "the existing TRIGGERED_BY class where a
// run's actor is meant". That class already means something else and every one of
// its edges already resolves: it names the repository the trigger fired in and
// carries the kind of trigger as evidence. Repointing it at a person would have
// deleted a relationship and its evidence to add one that a new class adds for
// nothing, so the class is left exactly as it was and this row is what says so.
func TestTriggeredByStillNamesTheRepositoryAndCarriesItsEvent(t *testing.T) {
	got := walkTheFixtureOrganization(t)

	_, edges, err := ghgraph.Build(got)
	if err != nil {
		t.Fatalf("building the fixture walk: %v", err)
	}

	found := 0
	for _, edge := range edges {
		if edge.Type != ghgraph.EdgeTriggeredBy {
			continue
		}
		found++
		if !strings.Contains(edge.ToID, "/Repository/") {
			t.Errorf("a TRIGGERED_BY edge points at %q, which is not a repository. This class names "+
				"the repository the trigger fired in; the person who caused it is at the other end "+
				"of %s and %s", edge.ToID, ghgraph.EdgeAttributedTo, ghgraph.EdgeInitiatedBy)
		}
	}
	if found == 0 {
		t.Fatal("the walk emitted no TRIGGERED_BY edge at all, so the assertion above says nothing")
	}

	// The first run's edge written out whole — endpoints, type, evidence and
	// method — so a change to any part of it is a red here.
	want := struct{ from, to, evidence, method string }{
		"github:acme/WorkflowRun/acme/api/100",
		"github:acme/Repository/acme/api",
		`{"event":"push"}`,
		"cicd-collect",
	}
	matched := false
	for _, edge := range edges {
		if edge.FromID != want.from || edge.Type != ghgraph.EdgeTriggeredBy {
			continue
		}
		matched = true
		if edge.ToID != want.to || edge.Evidence != want.evidence || edge.Method != want.method {
			t.Errorf("the run's TRIGGERED_BY edge is now (to=%q evidence=%q method=%q), want "+
				"(to=%q evidence=%q method=%q)",
				edge.ToID, edge.Evidence, edge.Method, want.to, want.evidence, want.method)
		}
	}
	if !matched {
		t.Errorf("the run %q emits no TRIGGERED_BY edge at all", want.from)
	}
}

// TestNoUserEmailReachesAnyByteOfTheResult is R5, over the raw bytes of the whole
// built result rather than field by field — the shape the secret-value row already
// uses, and for the same reason: a value can reach the graph through a detail, a
// summary, a metadata value, an id or an edge's evidence.
//
// THE FIXTURE CARRIES TWO ADDRESSES THE PROVIDER REALLY SENDS: the actor object's
// own `email` field, and the run's head-commit author. The second is the one a
// literal reading of "every user-bearing field" would have collected, and it is
// the reason this row names both rather than one.
func TestNoUserEmailReachesAnyByteOfTheResult(t *testing.T) {
	got := walkTheFixtureOrganization(t)
	nodes, edges, err := ghgraph.Build(got)
	if err != nil {
		t.Fatalf("building the fixture walk: %v", err)
	}
	encoded := marshal(t, map[string]any{"nodes": nodes, "edges": edges})

	// THE KNOWN POSITIVE FOR THE SUBJECT: the result DOES describe the person
	// whose address was dropped. Without it, a walk that emitted no user at all
	// would satisfy every absence below.
	if !strings.Contains(encoded, `"github:acme/User/ada"`) {
		t.Fatal("the encoded result names no user node, so its silence about addresses is silence " +
			"about nothing")
	}

	for _, address := range []string{fixtureActorEmail, fixtureCommitEmail} {
		if strings.Contains(encoded, address) {
			t.Errorf("the address %q appears in the encoded result", address)
		}
	}
	// AND NO FIELD NAMED FOR ONE, which is the arm that catches an address this
	// fixture did not name: an empty one, a differently spelled one, one a later
	// version of the provider adds.
	//
	// THE ESCAPED SPELLINGS ARE THE ONES A REAL LEAK PRODUCES, and searching only
	// the plain ones was a matcher that could not fire on the case the arm exists
	// for. A node's detail is marshaled into its Content as a STRING, so the
	// detail's own keys reach the encoded result with their quotes escaped: a
	// field named `email` on the user detail produces \"email\" and never
	// "email". Measured on this tree with exactly that field added, carrying a
	// value that is not address-shaped so the value search above stayed silent
	// too: this row PASSED while every user node carried the field.
	for _, tell := range append(escapedAddressTells(), plainAddressTells()...) {
		if strings.Contains(encoded, tell) {
			t.Errorf("the encoded result carries %s", tell)
		}
	}
	if strings.Contains(encoded, fixtureAddressDomain) {
		t.Errorf("the encoded result carries %q", fixtureAddressDomain)
	}

	// THE PLANTED POSITIVES, ONE PER DOOR, because an address reaches the encoded
	// result by two different paths and one plant cannot exercise both. The first
	// is the door a leak takes today — a field on the detail document, which
	// arrives escaped. The second is a contract field named for an address, which
	// would arrive plain. Planting only the second is what left the first arm
	// unexercised while reading as a control.
	for _, planted := range []struct {
		door    string
		tells   []string
		payload string
	}{
		{
			"a field on a user's detail document",
			escapedAddressTells(),
			marshal(t, map[string]any{
				"nodes": withPlantedDetail(nodes,
					`{"kind":"User","name":"planted","email":"`+fixtureActorEmail+
						`","Email":"`+fixtureActorEmail+`"}`),
				"edges": edges,
			}),
		},
		{
			"a contract field named for an address",
			plainAddressTells(),
			marshal(t, map[string]any{
				"nodes": nodes, "edges": edges,
				"email": fixtureActorEmail, "Email": fixtureActorEmail,
			}),
		},
	} {
		for _, tell := range append(planted.tells, fixtureActorEmail, fixtureAddressDomain) {
			if !strings.Contains(planted.payload, tell) {
				t.Fatalf("%s did not produce %s in the encoded result, so the tell for it can never "+
					"fire and the absence asserted above is an absence of nothing",
					planted.door, tell)
			}
		}
	}
}

// escapedAddressTells are the spellings a field on a marshaled DETAIL document
// produces, which is where a user's address would actually land: the detail is
// carried as a string, so its keys arrive with their quotes escaped.
func escapedAddressTells() []string { return []string{`\"email\"`, `\"Email\"`} }

// plainAddressTells are the spellings a field on the contract NODE itself would
// produce. The capitalized one is not decoration: a Go field with no json tag
// marshals under its own name.
func plainAddressTells() []string { return []string{`"email"`, `"Email"`} }

// withPlantedDetail returns the walk's nodes with one extra copy whose detail
// document carries the planted address, so the plant travels the same path a
// real leak would rather than an easier one.
func withPlantedDetail(nodes []framework.Node, detail string) []framework.Node {
	planted := nodes[0]
	planted.Content = detail
	return append([]framework.Node{planted}, nodes...)
}

// TestAUserNodeCarriesExactlyItsFourDetailFieldsAndItsOneMetadataKey is the
// allowlist, named field by field.
//
// THE LIST IS WRITTEN OUT rather than derived from the detail struct: a field
// added to that struct would otherwise join the allowlist silently, which is
// exactly the change this row exists to catch — and the field most likely to be
// added to a user is the one requirement 5 forbids.
func TestAUserNodeCarriesExactlyItsFourDetailFieldsAndItsOneMetadataKey(t *testing.T) {
	checked := 0
	for id, node := range builtNodesByID(t) {
		if node.Metadata["resource_type"] != ghgraph.ResourceTypeUser {
			continue
		}
		checked++

		var detail map[string]any
		if err := json.Unmarshal([]byte(node.Content), &detail); err != nil {
			t.Fatalf("decoding the detail of %q: %v", id, err)
		}
		for key := range detail {
			switch key {
			case "kind", "name", "id", "html_url":
			default:
				t.Errorf("the user node %q carries the detail field %q, which is outside the "+
					"allowlist of kind, name, id and html_url", id, key)
			}
		}
		for key := range node.Metadata {
			switch key {
			case "org", "resource_type", "provider":
			default:
				t.Errorf("the user node %q carries the metadata key %q, which is outside the "+
					"allowlist", id, key)
			}
		}
	}
	if checked != 3 {
		t.Errorf("the walk emitted %d user nodes, want 3 — the run's actor, its triggering actor "+
			"and the deployment's creator, with the actor and the reviewer being one person",
			checked)
	}
}

// TestAnObjectThatNamesNoPersonMintsNothing is the empty arm, on every one of the
// three fields.
func TestAnObjectThatNamesNoPersonMintsNothing(t *testing.T) {
	repos, actions := fixtureAPI()
	for _, run := range actions.runs["acme/api"].WorkflowRuns {
		run.Actor = nil
		// A user object with no login names nobody either, and it is a different
		// input from an absent one.
		run.TriggeringActor = &gogithub.User{ID: new(int64(7)), Type: new("User")}
	}
	for _, deployment := range repos.deployments["acme/api"] {
		deployment.Creator = nil
	}

	runs := runOne(t, repos, actions, "github-workflow-runs")
	deployments := runOne(t, repos, actions, "github-deployments")

	for _, got := range []ghgraph.Result{runs, deployments} {
		for _, res := range got.Resources {
			if res.ResourceType == ghgraph.ResourceTypeUser {
				t.Errorf("an object naming no person minted the user node %q", res.ID)
			}
		}
		for _, rel := range got.Relations {
			switch rel.Type {
			case ghgraph.EdgeAttributedTo, ghgraph.EdgeInitiatedBy, ghgraph.EdgeCreatedBy:
				t.Errorf("an object naming no person emitted %s %s -> %s",
					rel.Type, rel.FromID, rel.ToID)
			}
		}
	}

	// The known positive: the runs and the deployments themselves were still
	// enumerated, so the absence above is about the person and not about a walk
	// that read nothing.
	for _, want := range []struct {
		got ghgraph.Result
		id  string
	}{
		{runs, "github:acme/WorkflowRun/acme/api/100"},
		{deployments, "github:acme/Deployment/acme/api/500"},
	} {
		if _, ok := resourceIDs(want.got)[want.id]; !ok {
			t.Fatalf("%q was not emitted either; the absence above means nothing", want.id)
		}
	}
}

// builtNodesByID runs the whole fixture organization through the graph builder
// and indexes the result, which is where deduplication has happened.
func builtNodesByID(t *testing.T) map[string]framework.Node {
	t.Helper()
	nodes, _, err := ghgraph.Build(walkTheFixtureOrganization(t))
	if err != nil {
		t.Fatalf("building the fixture walk: %v", err)
	}
	out := make(map[string]framework.Node, len(nodes))
	for _, node := range nodes {
		out[node.ID] = node
	}
	return out
}
