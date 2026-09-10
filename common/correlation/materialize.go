// SPDX-License-Identifier: Apache-2.0

package correlation

import (
	"fmt"
	"strconv"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// materialize.go — THE EMITTED EDGE, and why the rendering lives here rather
// than in each collector.
//
// THE EVIDENCE STRING IS AN INFORMAL CONTRACT WITH EVERY CONSUMER: a checked-in
// parity golden, an operator reading an edge, a test asserting a substring. It
// had three independent copies before this module existed — the built-in's
// materializer and one inside each of two collectors — which is exactly the
// shape that drifts one spelling at a time. Rendering it beside the detector
// that produces the numbers is what makes the contract one thing.
//
// AN UNCONFIRMED PAIR EMITS NOTHING, and that is the point of the flag rather
// than a filter applied for tidiness: two services logging errors at the same
// moment is a coincidence until something says their resources actually depend
// on each other. Writing the unconfirmed pairs as edges would put guesses in the
// graph under the same edge type as facts.

const (
	// EdgeCorrelatesWith is the edge type a confirmed correlation emits.
	EdgeCorrelatesWith = "CORRELATES_WITH"
	// CorrelationMethod names how the edge was derived, and it is read by name
	// downstream.
	CorrelationMethod = "temporal+cloud-dependency"
	// correlationScoreDecimals is the fixed precision the evidence string
	// renders the score at. It is fixed rather than shortest-form so two
	// collects producing the same score produce the same bytes.
	correlationScoreDecimals = 3
)

// MaterializeCorrelations emits one edge per STRUCTURALLY CONFIRMED
// correlation, with the confidence, method and evidence shape the built-in
// pipeline emits (materialize.go:138-145 at the parity tree).
func MaterializeCorrelations(correlations []Result) []framework.Edge {
	if len(correlations) == 0 {
		return nil
	}
	edges := make([]framework.Edge, 0, len(correlations))
	for _, c := range correlations {
		if !c.StructurallyConfirmed {
			continue
		}
		edges = append(edges, framework.Edge{
			FromID:     c.TemplateA,
			ToID:       c.TemplateB,
			Type:       EdgeCorrelatesWith,
			Confidence: c.CooccurrenceScore,
			Method:     CorrelationMethod,
			Evidence: fmt.Sprintf("services=%s,%s resources=%s,%s score=%s",
				c.ServiceA, c.ServiceB, c.ResourceA, c.ResourceB,
				strconv.FormatFloat(c.CooccurrenceScore, 'f', correlationScoreDecimals, 64)),
		})
	}
	return edges
}
