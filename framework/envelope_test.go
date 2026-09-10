// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"strings"
	"testing"
)

// envelope_test.go — the envelope's own rows: the empty walk, the completeness
// assertion, the two refusals, the edge pass-through and the walk-error arm.
// Every one runs on BOTH transports, because a refusal that fires on one and not
// the other is exactly the silent drop the matrix exists to catch.

// TestEmptyWalkIsASuccessfulCollect is row 15b: a walk that found nothing is a
// SUCCESSFUL collect carrying two empty arrays, not a failed one.
//
// Without the encoder's normalization both Go slices are nil, they marshal to
// null, and the SDK's own validation against the advertised contract schema
// fails the whole call with a type error naming whichever of the two properties
// its map iteration reached first. This is the cell a collector with nothing to
// collect hits on its first real run.
func TestEmptyWalkIsASuccessfulCollect(t *testing.T) {
	for _, transport := range []string{overStdio, overHTTP} {
		t.Run(transport, func(t *testing.T) {
			p := dial(t, transport, fixtureEmptyWalk, "")
			res, err := p.call(t, map[string]any{"id": "inst-7"})
			if err != nil {
				t.Fatalf("a walk that found nothing failed the whole collect: %v", err)
			}
			if res.IsError {
				t.Fatalf("a walk that found nothing was refused: %s", resultText(res))
			}
			doc := envelopeOf(t, res)
			assertArray(t, doc, "nodes", 0)
			assertArray(t, doc, "edges", 0)
			if doc["walk_complete"] != true {
				t.Errorf("walk_complete is %v, want true", doc["walk_complete"])
			}
		})
	}
}

// TestIncompleteWalkCarriesWalkComplete is R9's row, and it rides the INCOMPLETE
// arm deliberately: with omitempty on the tag a complete walk still emits
// `true` and the defect is invisible, so a suite that only asserts complete
// walks passes straight over the regression.
func TestIncompleteWalkCarriesWalkComplete(t *testing.T) {
	for _, transport := range []string{overStdio, overHTTP} {
		t.Run(transport, func(t *testing.T) {
			p := dial(t, transport, fixtureIncompleteWalk, "")
			res, err := p.call(t, map[string]any{"id": "inst-7"})
			if err != nil {
				t.Fatalf("an incomplete walk failed the whole collect: %v", err)
			}
			if res.IsError {
				t.Fatalf("an incomplete walk was refused: %s", resultText(res))
			}
			doc := envelopeOf(t, res)
			raw, present := doc["walk_complete"]
			if !present {
				t.Fatalf("walk_complete is ABSENT from an incomplete walk's envelope: %s", mustJSON(t, doc))
			}
			if raw != false {
				t.Errorf("walk_complete is %v, want false", raw)
			}
		})
	}
}

// TestWalkErrorBecomesAToolError is row 19: a failed walk is a tool error naming
// the collector and the cause, and carries NO envelope. Both halves are the row
// — an empty complete result here would assert a successful walk that found
// nothing, which is the opposite of what happened.
func TestWalkErrorBecomesAToolError(t *testing.T) {
	for _, transport := range []string{overStdio, overHTTP} {
		t.Run(transport, func(t *testing.T) {
			p := dial(t, transport, fixtureWalkError, "")
			res, err := p.call(t, map[string]any{"id": "inst-7"})
			if err != nil {
				t.Fatalf("a failed walk became a protocol error rather than a tool error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("a failed walk returned a SUCCESSFUL result: %s", mustJSON(t, envelopeOf(t, res)))
			}
			if res.StructuredContent != nil {
				t.Errorf("a failed walk carried an envelope: %v", res.StructuredContent)
			}
			text := resultText(res)
			if !strings.Contains(text, DefaultToolName) || !strings.Contains(text, "upstream 503") {
				t.Errorf("the error names neither the collector nor the cause: %s", text)
			}
		})
	}
}

// TestEmptyNodeTypeIsRefused is row 14: the envelope's own bad input, refused
// naming the index and the id, and refused EARLIER than the client would refuse
// it — so a collector author sees it on their own run rather than in a collect.
func TestEmptyNodeTypeIsRefused(t *testing.T) {
	for _, transport := range []string{overStdio, overHTTP} {
		t.Run(transport, func(t *testing.T) {
			p := dial(t, transport, fixtureEmptyNodeType, "")
			res, err := p.call(t, map[string]any{"id": "inst-7"})
			if err != nil {
				t.Fatalf("the call failed at the protocol level: %v", err)
			}
			if !res.IsError {
				t.Fatalf("a node with an empty type was ACCEPTED: %s", mustJSON(t, envelopeOf(t, res)))
			}
			text := resultText(res)
			if !strings.Contains(text, "node[1]") || !strings.Contains(text, `"n2"`) {
				t.Errorf("the refusal names neither the index nor the id: %s", text)
			}
		})
	}
}

// TestZeroCompletenessIsRefused is row 18: the residual the type cannot close. A
// collector writing framework.Completeness{} asserts nothing, and the encoder
// refuses it loudly naming the collector rather than shipping it as "incomplete"
// — which is what a bare bool would have done silently.
func TestZeroCompletenessIsRefused(t *testing.T) {
	for _, transport := range []string{overStdio, overHTTP} {
		t.Run(transport, func(t *testing.T) {
			p := dial(t, transport, fixtureZeroCompleteness, "")
			res, err := p.call(t, map[string]any{"id": "inst-7"})
			if err != nil {
				t.Fatalf("the call failed at the protocol level: %v", err)
			}
			if !res.IsError {
				t.Fatalf("a zero Completeness was ACCEPTED: %s", mustJSON(t, envelopeOf(t, res)))
			}
			text := resultText(res)
			if !strings.Contains(text, "zero Completeness") || !strings.Contains(text, DefaultToolName) {
				t.Errorf("the refusal does not name the condition and the collector: %s", text)
			}
		})
	}
}

// TestIncompleteWithNoReasonIsRefused covers the other unusable value the
// constructors admit. The reason is not sent on the wire, and it is required
// anyway: it is what the collector logs and what a reviewer reads to judge
// whether the incomplete arm is reachable at all.
func TestIncompleteWithNoReasonIsRefused(t *testing.T) {
	p := dial(t, overHTTP, fixtureIncompleteNoReason, "")
	res, err := p.call(t, map[string]any{"id": "inst-7"})
	if err != nil {
		t.Fatalf("the call failed at the protocol level: %v", err)
	}
	if !res.IsError {
		t.Fatalf("an incomplete assertion with no reason was ACCEPTED: %s", mustJSON(t, envelopeOf(t, res)))
	}
	if text := resultText(res); !strings.Contains(text, "no reason") {
		t.Errorf("the refusal does not name the condition: %s", text)
	}
}

// TestEmptyCollectIDIsRefused: the contract schema requires the id to be present
// and a string; it cannot require it to be non-empty. An empty collect id names
// no graph instance, so the framework refuses it rather than walking for a
// result with nowhere to land.
func TestEmptyCollectIDIsRefused(t *testing.T) {
	p := dial(t, overHTTP, fixtureConforming, "")
	res, err := p.call(t, map[string]any{"id": ""})
	if err != nil {
		t.Fatalf("the call failed at the protocol level: %v", err)
	}
	if !res.IsError {
		t.Fatalf("an empty collect id was ACCEPTED: %s", mustJSON(t, envelopeOf(t, res)))
	}
	if text := resultText(res); !strings.Contains(text, "collect id is empty") {
		t.Errorf("the refusal does not name the condition: %s", text)
	}
	if p.fixture.callCount() != 0 {
		t.Errorf("the walk ran %d times on an empty collect id", p.fixture.callCount())
	}
}

// TestDanglingEdgePassesThrough is row 15: an edge naming an endpoint this
// result does not carry is emitted AS GIVEN. The framework adds no endpoint
// check, because endpoint resolution belongs to the write path and every
// built-in collector emits cross-graph edges the same way.
func TestDanglingEdgePassesThrough(t *testing.T) {
	for _, transport := range []string{overStdio, overHTTP} {
		t.Run(transport, func(t *testing.T) {
			p := dial(t, transport, fixtureDanglingEdge, "")
			res, err := p.call(t, map[string]any{"id": "inst-7"})
			if err != nil {
				t.Fatalf("a dangling edge failed the collect: %v", err)
			}
			if res.IsError {
				t.Fatalf("a dangling edge was REFUSED: %s", resultText(res))
			}
			doc := envelopeOf(t, res)
			assertArray(t, doc, "nodes", 1)
			assertArray(t, doc, "edges", 1)
			edges, _ := doc["edges"].([]any)
			edge, ok := edges[0].(map[string]any)
			if !ok {
				t.Fatalf("the edge is %T, not an object", edges[0])
			}
			if edge["to_id"] != "absent" {
				t.Errorf("the edge's endpoint was rewritten to %v, want it passed through as \"absent\"", edge["to_id"])
			}
		})
	}
}
