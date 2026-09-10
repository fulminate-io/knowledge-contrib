// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/collect"
)

// parity_test.go — THE COVERAGE ASSERTION, and the one row that says whether
// this collector meets its target at all.
//
// The declared vocabulary is a promise: a consumer that queries a resource type
// or walks an edge type expects this collector to produce it. A module that
// declared the list and emitted three quarters of it would pass every other test
// in this suite — each converter is correct in isolation — and produce a graph
// missing whole classes of thing. This runs the WHOLE collector over one
// workspace's recorded responses and asserts the emitted set covers the declared
// one.
//
// THE OTHER DIRECTION IS CLOSED BY CONSTRUCTION: the graph builder refuses a
// type outside the declared vocabulary, so the emitted set is a subset already
// and these assertions close it into an equality.

func TestEveryDeclaredResourceTypeIsEmitted(t *testing.T) {
	got := walkTheFixture(t)

	emitted := map[string]bool{}
	for _, res := range got.Resources {
		emitted[res.ResourceType] = true
	}

	var missing []string
	for _, want := range bbgraph.ResourceTypes() {
		if !emitted[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d declared resource types are never emitted: %s\n"+
			"A type in the declared vocabulary that nothing produces is a promise this collector "+
			"does not keep.", len(missing), strings.Join(missing, ", "))
	}

	// The control: the assertion above would pass just as well against a
	// collector that emitted EVERY string, so the emitted set is also checked for
	// anything outside the declared one.
	for emittedType := range emitted {
		if !bbgraph.IsDeclaredResourceType(emittedType) {
			t.Errorf("the walk emitted %q, which is not in the declared vocabulary", emittedType)
		}
	}
}

// TestTheEnvironmentAndApprovalGateKindsLandFromTheLiveEnvironmentsShape names
// the two kinds the floor above counted while the provider produced neither.
//
// THE FLOOR CANNOT DISTINGUISH "EMITTED" FROM "EMITTED HERE". Its corpus is
// this module's recorded workspace, and that workspace recorded
// `restrictions.admin_only` as an empty ARRAY, which is a shape the provider
// does not send: live it is a BOOLEAN, the decode failed, the whole environments
// page was lost, and with it every environment, every approval gate and every
// deployment-scope variable — while this floor stayed green. So the two kinds
// are named here, on a corpus now keyed on the live shape, and the failure text
// says which read they come from rather than reporting them as a missing type.
func TestTheEnvironmentAndApprovalGateKindsLandFromTheLiveEnvironmentsShape(t *testing.T) {
	got := walkTheFixture(t)
	emitted := map[string]bool{}
	for _, res := range got.Resources {
		emitted[res.ResourceType] = true
	}
	for _, kind := range []string{bbgraph.ResourceTypeEnvironment, bbgraph.ResourceTypeApprovalGate} {
		if !emitted[kind] {
			t.Errorf("no %q node came out of the recorded workspace. Both kinds are produced by "+
				"the environments read alone, so the whole read failing — a decode this corpus "+
				"used to model in a shape the provider does not send — takes both of them, and "+
				"the deployment-scope variables with them", kind)
		}
	}
}

func TestEveryDeclaredEdgeTypeIsEmitted(t *testing.T) {
	got := walkTheFixture(t)

	emitted := map[string]bool{}
	for _, rel := range got.Relations {
		emitted[rel.Type] = true
	}

	var missing []string
	for _, want := range bbgraph.EdgeTypes() {
		if !emitted[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d declared edge types are never emitted: %s",
			len(missing), strings.Join(missing, ", "))
	}
	for emittedType := range emitted {
		if !bbgraph.IsDeclaredEdgeType(emittedType) {
			t.Errorf("the walk emitted the edge type %q, which is not in the declared vocabulary",
				emittedType)
		}
	}
}

// TestTriggeredByIsEmittedByNothing is the absence half of the floor, and it
// needs its own row because a coverage assertion that only checks presence lets
// a seventh edge type appear unnoticed.
//
// THE SAME-RUN KNOWN POSITIVE is the assertion above it: the same walk is
// checked for six types it MUST emit, so a walk that produced no edges at all
// cannot satisfy this one silently.
func TestTriggeredByIsEmittedByNothing(t *testing.T) {
	got := walkTheFixture(t)
	if len(got.Relations) == 0 {
		t.Fatal("the fixture walk emitted no edges at all; the absence below would mean nothing")
	}
	for _, rel := range got.Relations {
		if rel.Type == bbgraph.EdgeTriggeredBy {
			t.Errorf("the walk emitted %s (%s -> %s). This provider names no other resource on a "+
				"run — it carries a trigger TYPE and nothing else — so an edge asserting one was "+
				"invented.", bbgraph.EdgeTriggeredBy, rel.FromID, rel.ToID)
		}
	}
}

// TestTheBelongsToFanInReachesEverySixParents pins the six emission sites the
// relationship has and the eight parent arms they take, by LITERAL id on both
// ends.
//
// Every pair is written out rather than built from the id helpers: a test that
// asked the code under test what it should have produced would agree with it
// however wrong both were.
func TestTheBelongsToFanInReachesEverySixParents(t *testing.T) {
	got := walkTheFixture(t)

	for _, want := range [][2]string{
		// repository -> workspace
		{"bitbucket:acme/Repository/api", "bitbucket:acme/Workspace/acme"},
		// pipeline -> repository
		{"bitbucket:acme/Pipeline/api/default", "bitbucket:acme/Repository/api"},
		// pipeline_run -> repository
		{"bitbucket:acme/PipelineRun/api/{run-1}", "bitbucket:acme/Repository/api"},
		// environment -> repository
		{"bitbucket:acme/Environment/api/production", "bitbucket:acme/Repository/api"},
		// runner -> workspace, and runner -> repository: BOTH arms
		{"bitbucket:acme/Runner/{runner-ws}", "bitbucket:acme/Workspace/acme"},
		{"bitbucket:acme/Runner/{runner-api}", "bitbucket:acme/Repository/api"},
		// variable -> its scope parent, all THREE scopes
		{"bitbucket:acme/Variable/workspace/WORKSPACE_TOKEN", "bitbucket:acme/Workspace/acme"},
		{"bitbucket:acme/Variable/repository/api/API_KEY", "bitbucket:acme/Repository/api"},
		{"bitbucket:acme/Variable/env/api/production/DEPLOY_KEY",
			"bitbucket:acme/Environment/api/production"},
	} {
		if !hasRelation(got, want[0], want[1], bbgraph.EdgeBelongsTo) {
			t.Errorf("no BELONGS_TO edge from %q to %q", want[0], want[1])
		}
	}
}

// TestEveryEmittedNodeCarriesASummary is the nine-summarizer floor, asserted as
// the property it exists for rather than by counting registrations.
//
// A MODULE REPRODUCING ONLY THE RESOURCE-TYPE AND EDGE-TYPE FLOORS WOULD SHIP
// NODES WITH NO SUMMARY, and summary is what this graph's embedding and its text
// index are built from by the behavior block its worked entry declares. The
// resource-type coverage row above is the same-run control: every declared type
// is present in this walk, so a summarizer missing for any one of them is
// reached here.
func TestEveryEmittedNodeCarriesASummary(t *testing.T) {
	nodes, _, err := bbgraph.Build(walkTheFixture(t))
	if err != nil {
		t.Fatalf("building the fixture walk: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("the fixture walk built no nodes; the assertion below would mean nothing")
	}
	for _, node := range nodes {
		if strings.TrimSpace(node.Summary) == "" {
			t.Errorf("the %q node %q carries no summary",
				node.Metadata["resource_type"], node.ID)
		}
		// AND IT IS NOT THE UNDECLARED-TYPE MARKER. A summarizer dispatch that fell
		// through to its default arm would produce a non-empty summary and pass the
		// assertion above while saying nothing about the resource.
		if strings.Contains(node.Summary, "undeclared type") {
			t.Errorf("the node %q summarized through the undeclared-type arm: %q",
				node.ID, node.Summary)
		}
	}
}

// TestTwoCollectsOfAnUnchangedWorkspaceAreIdentical is the carry-forward row.
//
// THE NONDETERMINISM IT CATCHES IS REAL AND NAMED: four of the five pipeline
// trigger sections are Go maps and ranging one yields a different order per run,
// and the five parallel enumerations merge under a mutex in completion order. So
// the second walk is run with the enumerations SHUFFLED, which is what makes the
// two inputs genuinely different orders rather than the same one twice.
func TestTwoCollectsOfAnUnchangedWorkspaceAreIdentical(t *testing.T) {
	first, err := walkTheFixtureAllowingError(t, newFixture(t), func([]collect.Subcollector) {})
	if err != nil {
		t.Fatalf("the first collect: %v", err)
	}
	second, err := walkTheFixtureAllowingError(t, newFixture(t), shuffled)
	if err != nil {
		t.Fatalf("the second collect: %v", err)
	}

	firstNodes, firstEdges, err := bbgraph.Build(first)
	if err != nil {
		t.Fatalf("building the first collect: %v", err)
	}
	secondNodes, secondEdges, err := bbgraph.Build(second)
	if err != nil {
		t.Fatalf("building the second collect: %v", err)
	}

	// COMPARED AS THE BYTES THAT GO ON THE WIRE, which is what "changes nothing"
	// means to the receiving server: it compares generations, not Go values. The
	// node struct carries a map and is not comparable in Go anyway, and a
	// field-by-field comparison would silently stop covering a field added later.
	if a, b := marshal(t, firstNodes), marshal(t, secondNodes); a != b {
		t.Errorf("two collects of an unchanged workspace produced different nodes:\n%s\nvs\n%s", a, b)
	}
	if a, b := marshal(t, firstEdges), marshal(t, secondEdges); a != b {
		t.Errorf("two collects of an unchanged workspace produced different edges:\n%s\nvs\n%s", a, b)
	}

	// The known positive: an empty walk would compare equal to itself. The
	// fixture workspace is not empty, and this says so in the same run.
	if len(firstNodes) == 0 || len(firstEdges) == 0 {
		t.Fatalf("the fixture walk produced %d nodes and %d edges; two empty results are equal "+
			"for the wrong reason", len(firstNodes), len(firstEdges))
	}
}
