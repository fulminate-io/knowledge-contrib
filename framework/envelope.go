// SPDX-License-Identifier: Apache-2.0

package framework

import "fmt"

// envelope.go — the CONTRACT ENVELOPE the served tool returns, and the two
// refusals plus the one normalization that stand between a walk's Result and it.
//
// NO omitempty AND NO omitzero ON ANY OF THE THREE FIELDS, and walk_complete is
// the one that matters. The contract output schema REQUIRES walk_complete, and
// the SDK validates this value's JSON against the advertised schema before the
// result leaves the provider; with omitempty a walk asserting INCOMPLETE emits
// no walk_complete at all and the whole collect fails with a missing-property
// error. A walk asserting COMPLETE emits `true` and the defect is invisible,
// which is why the test that pins this rides the incomplete arm.

// collectOutput is the served tool's Out type: the contract envelope, exactly.
type collectOutput struct {
	Nodes        []Node `json:"nodes"`
	Edges        []Edge `json:"edges"`
	WalkComplete bool   `json:"walk_complete"`
}

// encodeResult turns one walk's Result into the envelope, refusing the two
// shapes that are bad input and normalizing the one that is a Go artifact.
//
// collector names the served tool, which is the only identity this package has
// for the collector, so a refusal reaching an operator names something they can
// find in their config entry.
func encodeResult(collector string, r Result) (collectOutput, error) {
	if !r.Complete.IsAsserted() {
		return collectOutput{}, fmt.Errorf(
			"framework: collector %q returned a zero Completeness value, which asserts nothing; "+
				"a walk returns framework.Complete() or framework.Incomplete(reason)", collector)
	}
	if !r.Complete.IsComplete() && r.Complete.Reason() == "" {
		return collectOutput{}, fmt.Errorf(
			"framework: collector %q asserted an INCOMPLETE walk with no reason; "+
				"framework.Incomplete takes the reason the walk did not finish", collector)
	}
	for i := range r.Nodes {
		if r.Nodes[i].Type == "" {
			return collectOutput{}, fmt.Errorf(
				"framework: collector %q: node[%d] (id=%q) has an empty type", collector, i, r.Nodes[i].ID)
		}
	}

	// THE EMPTY WALK IS NORMALIZED, and this is the line between a collector
	// that finds nothing and a collect that fails. A walk that found nothing
	// leaves both slices nil in Go; nil marshals to null; the contract output
	// schema declares both as "array" with no null union; and the SDK validates
	// this value against that schema server-side, so the call fails with a type
	// error naming whichever of the two properties the validator reached first.
	// An empty region, an empty log group or a filter that matched nothing is
	// the first real run of a new collector, so this is the cell it hits.
	//
	// It emits an empty ARRAY, never a fabricated node: an empty complete
	// collect lands an empty generation, and the server's deletion phase
	// declines to derive anything from a collect that named nothing.
	nodes := r.Nodes
	if nodes == nil {
		nodes = []Node{}
	}
	edges := r.Edges
	if edges == nil {
		edges = []Edge{}
	}
	return collectOutput{Nodes: nodes, Edges: edges, WalkComplete: r.Complete.IsComplete()}, nil
}
