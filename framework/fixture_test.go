// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"context"
	"errors"
	"sync"
)

// fixture_test.go — THE FIXTURE COLLECTOR this module's suite serves, and it is
// deliberately the whole of what a collector author writes.
//
// It implements the Collector interface and nothing else: no MCP server, no
// transport, no schema, no envelope. A census in census_test.go reads this file
// and asserts that, because "a collector author writes the walk and a params
// schema and nothing else" is a requirement of this ticket rather than a hope,
// and the fixture is the only collector this module can hold.
//
// Each mode is one input class the framework's own encoder or the SDK's
// validation has to be proven against, and the two error modes are the arms.

// fixtureParams is the fixture's params type. Region carries NO omitempty, so
// the inferred params sub-schema marks it required — which is what makes the
// invalid-params column of the matrix a real refusal and the absent-params
// column a real distinction from an empty params object.
type fixtureParams struct {
	Region string `json:"region"`
	Depth  int    `json:"depth,omitempty"`
}

type fixtureMode string

const (
	// fixtureConforming: a complete walk with nodes and edges.
	fixtureConforming fixtureMode = "conforming"
	// fixtureIncompleteWalk: the same rows, asserting an incomplete walk.
	fixtureIncompleteWalk fixtureMode = "incomplete-walk"
	// fixtureEmptyWalk: a complete walk that found nothing, returning NIL
	// slices — the shape a Go author writes without thinking about it.
	fixtureEmptyWalk fixtureMode = "empty-walk"
	// fixtureEmptyIncomplete: found nothing AND could not finish.
	fixtureEmptyIncomplete fixtureMode = "empty-incomplete"
	// fixtureWalkError: the walk fails.
	fixtureWalkError fixtureMode = "walk-error"
	// fixtureEmptyNodeType: a node with no type, which is the envelope's own
	// bad input.
	fixtureEmptyNodeType fixtureMode = "empty-node-type"
	// fixtureZeroCompleteness: the walk returns the zero Completeness literal.
	fixtureZeroCompleteness fixtureMode = "zero-completeness"
	// fixtureIncompleteNoReason: an incomplete assertion with no reason.
	fixtureIncompleteNoReason fixtureMode = "incomplete-no-reason"
	// fixtureDanglingEdge: an edge naming an endpoint this result omits.
	fixtureDanglingEdge fixtureMode = "dangling-edge"
)

// fixtureCall is one recorded invocation of the walk. The record is what makes
// "the walk never ran" observable rather than inferred from a refusal.
type fixtureCall struct {
	id      string
	params  fixtureParams
	foreign ForeignContext
}

type fixtureCollector struct {
	mode fixtureMode
	name string
	// declBad makes Describe return a declaration the framework must refuse.
	declBad bool

	mu    sync.Mutex
	calls []fixtureCall
}

func (f *fixtureCollector) Tool() ToolSpec {
	return ToolSpec{Name: f.name, Description: "a fixture collector for the framework's own suite"}
}

// Describe is the fixture's declaration, and it is deliberately a FULL one:
// every half of the document (the three booleans, the three field lists, a
// per-node-type override, both vocabularies, all FOUR environment classes and a
// foreign-context family) so the describe rows assert against a value with
// something in every arm rather than against a mostly-zero one.
//
// declBad switches it to a declaration NewServer must refuse, which is how the
// "a malformed declaration refuses the server" row observes the gate.
func (f *fixtureCollector) Describe() Declaration {
	if f.declBad {
		return Declaration{
			Behavior:  BehaviorDeclaration{Summarizable: new(false), Embeddable: new(false), Syncable: new(true)},
			NodeTypes: []string{"issue"},
			EdgeTypes: []string{},
			Environment: []EnvDeclaration{
				{Name: "NOT A NAME", Class: EnvClassSelector},
			},
		}
	}
	return fixtureDeclaration()
}

// fixtureDeclaration is the reference declaration, written once so a test
// asserting what the tool returned compares against the collector's own value
// rather than against a literal copy of it.
func fixtureDeclaration() Declaration {
	return Declaration{
		Behavior: BehaviorDeclaration{
			Summarizable:    new(true),
			Embeddable:      new(false),
			Syncable:        new(true),
			EmbedFields:     []string{"summary", "content"},
			SummarizeFields: []string{"content"},
			Bm25Fields:      []string{"keywords", "symbol_name"},
		},
		NodeTypeOverrides: map[string]NodeTypeOverrideDeclaration{
			"issue": {Summarizable: new(false), EmbedFields: []string{"summary"}},
		},
		NodeTypes: []string{"issue"},
		EdgeTypes: []string{"blocks"},
		Environment: []EnvDeclaration{
			{Name: "HOME", Class: EnvClassPath, Description: "the credential chain's base"},
			{Name: "FIXTURE_REGION", Class: EnvClassSelector},
			// MARKED, and on the SECRET class: the mark says this collector tells
			// the name present-and-empty apart from absent, which is a property of
			// what the collector READS and is independent of what an installer
			// WRITES. Two classes are marked here so no arm of the answer can be
			// satisfied by a single-class implementation.
			{Name: "FIXTURE_TOKEN", Class: EnvClassSecret, EmptySensitive: true},
			// THE FOURTH CLASS IS HERE SO THE INSTALLER TABLE'S EXCLUSION OF IT IS
			// OBSERVED, rather than only caught one level away by the generator's
			// drift gate. A collector declares a name it reads that no installed
			// entry should carry; EnvTable must render no row for it.
			//
			// IT IS ALSO MARKED, and that pair is the whole reason the mark does not
			// ride the class table: the class table drops this class by
			// construction, so a marked not-carried name would have nowhere to go.
			{Name: "FIXTURE_PROXY", Class: EnvClassNotCarried, EmptySensitive: true},
		},
		Context: ForeignContextDeclaration{
			"code": {
				NodeTypes:     []string{"file"},
				NodeFields:    []string{"id", "file_path"},
				PathBasenames: []string{"go.mod"},
				Reason:        "the fixture correlates its issues with module roots",
			},
		},
	}
}

func (f *fixtureCollector) Walk(
	_ context.Context, id string, params fixtureParams, foreign ForeignContext,
) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fixtureCall{id: id, params: params, foreign: foreign})
	f.mu.Unlock()

	switch f.mode {
	case fixtureWalkError:
		return Result{}, errors.New("upstream 503")
	case fixtureEmptyWalk:
		return Result{Complete: Complete()}, nil
	case fixtureEmptyIncomplete:
		return Result{Complete: Incomplete("the region listing paged out")}, nil
	case fixtureEmptyNodeType:
		return Result{
			Nodes:    []Node{{ID: "n1", Type: "issue"}, {ID: "n2"}},
			Edges:    []Edge{},
			Complete: Complete(),
		}, nil
	case fixtureZeroCompleteness:
		return Result{Nodes: []Node{{ID: "n1", Type: "issue"}}, Edges: []Edge{}}, nil
	case fixtureIncompleteNoReason:
		return Result{Nodes: []Node{{ID: "n1", Type: "issue"}}, Edges: []Edge{}, Complete: Incomplete("")}, nil
	case fixtureDanglingEdge:
		return Result{
			Nodes:    []Node{{ID: "n1", Type: "issue"}},
			Edges:    []Edge{{FromID: "n1", ToID: "absent", Type: "blocks"}},
			Complete: Complete(),
		}, nil
	case fixtureIncompleteWalk:
		return fixtureRows(Incomplete("the source rate-limited the walk")), nil
	default:
		return fixtureRows(Complete()), nil
	}
}

// callCount reports how many times the walk ran in THIS process. It is zero for
// a provider running as a child, which is why the walk-never-ran assertion is
// made on the in-process arm.
func (f *fixtureCollector) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// recorded returns every invocation of the walk in THIS process, which is what
// the declared-context rows read the received block off.
func (f *fixtureCollector) recorded() []fixtureCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fixtureCall(nil), f.calls...)
}

func (f *fixtureCollector) lastCall() (fixtureCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return fixtureCall{}, false
	}
	return f.calls[len(f.calls)-1], true
}

// fixtureRows is the reference result: one node carrying every optional field,
// one carrying none, and an edge between them.
func fixtureRows(c Completeness) Result {
	return Result{
		Nodes: []Node{
			{
				ID: "ISSUE-1", Type: "issue",
				SymbolName: "Login broken", FilePath: "src/login.go", Language: "go",
				StartLine: 10, EndLine: 20, Content: "body", Signature: "sig",
				Summary: "one line", Description: "longer", Source: "fixture", Status: "open",
				Keywords: "login auth", IsExported: true,
				Metadata: map[string]string{"priority": "high"},
			},
			{ID: "ISSUE-2", Type: "issue"},
		},
		Edges:    []Edge{{FromID: "ISSUE-1", ToID: "ISSUE-2", Type: "blocks"}},
		Complete: c,
	}
}
