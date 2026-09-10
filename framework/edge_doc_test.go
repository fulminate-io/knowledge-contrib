// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// edge_doc_test.go — the Edge doc comment is a LOAD-BEARING STATEMENT, so it
// gets a gate rather than a reviewer's memory.
//
// WHY A TEST AND NOT A REVIEW NOTE. The comment tells a collector author what
// they may rely on. Its previous text asserted two things that are not true of
// this tree — that endpoint resolution belongs to the write path, and that every
// built-in collector emits cross-graph edges the same way — and an author who
// believed either would emit a dangling cross-graph edge and get a graph with a
// broken reference and no message. A wrong sentence in this comment is a defect
// with a blast radius outside this repository.

// edgeDocComment returns the doc comment attached to `type Edge struct`, read
// from this module's own source. Reading the FILE rather than a transcription is
// what makes this a gate: the bytes under assertion are the bytes an author
// reads.
//
// It is scoped to the Edge block on purpose. An assertion over the whole file
// would pass on a corrected sentence that landed in some other type's comment,
// which is the failure a scoped read makes impossible.
func edgeDocComment(t *testing.T) string {
	t.Helper()
	const src = "framework.go"
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading %s, the file whose Edge doc comment this test is about: %v", src, err)
	}
	body := string(raw)
	const anchor = "// Edge is one edge a walk produced."
	start := strings.Index(body, anchor)
	if start < 0 {
		t.Fatalf("%s carries no Edge doc comment starting %q — this gate reads a comment that no longer exists", src, anchor)
	}
	end := strings.Index(body[start:], "type Edge struct")
	if end < 0 {
		t.Fatalf("%s: no `type Edge struct` follows the Edge doc comment; the comment this gate reads is attached to nothing", src)
	}
	return body[start : start+end]
}

// TestEdgeDocCommentStatesWhatTheTreeDoes asserts the corrected statements are
// present VERBATIM and that neither retired clause survives.
//
// BOTH HALVES ARE REQUIRED. Presence alone passes on a comment that says the
// right thing in one paragraph and the retired falsehood in the next, which is
// exactly the shape a partial edit leaves behind. Absence alone passes on a
// comment that was deleted rather than corrected.
func TestEdgeDocCommentStatesWhatTheTreeDoes(t *testing.T) {
	doc := edgeDocComment(t)

	for _, want := range []string{
		"THE WRITE PATH RESOLVES NO ENDPOINT",
		"THE BUILT-IN COLLECTORS DO NOT ALL EMIT CROSS-GRAPH EDGES THE SAME WAY",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the Edge doc comment does not state %q; a collector author reading it would not learn what this tree actually does:\n%s", want, doc)
		}
	}

	for _, retired := range []string{
		"endpoint resolution belongs to the write path",
		"every built-in collector emits cross-graph edges the same way",
	} {
		if strings.Contains(doc, retired) {
			t.Errorf("the Edge doc comment still carries the retired clause %q, which this tree disproves:\n"+
				"the server's batch-edge write returns from_id and to_id verbatim with no lookup and no proxy "+
				"materialization, and the cross-graph edges that exist are emitted by the post-collect linker "+
				"rather than by any collector's result", retired)
		}
	}
}

// TestEdgeDocCommentDocumentsTheTargetGraphField is the field's own half of the
// same gate: a carrier a collector author cannot find is a carrier nobody uses.
func TestEdgeDocCommentDocumentsTheTargetGraphField(t *testing.T) {
	doc := edgeDocComment(t)
	if !strings.Contains(doc, "TargetGraph") {
		t.Errorf("the Edge doc comment never names TargetGraph, the field that makes an edge cross-graph:\n%s", doc)
	}
}

// TestEdgeDocCommentDocumentsTheSourceGraphField is the same gate for the
// field's mirror. The two are documented together or not at all: an author who
// finds only TargetGraph reads the carrier as one-directional and either
// reverses an edge whose foreign endpoint is the FROM or emits it with no family
// at all, which are the two wrong forcings the k8s collector held its Helm
// relationship back rather than take.
func TestEdgeDocCommentDocumentsTheSourceGraphField(t *testing.T) {
	doc := edgeDocComment(t)
	if !strings.Contains(doc, "SourceGraph") {
		t.Errorf("the Edge doc comment never names SourceGraph, the field that makes an edge's FROM the foreign endpoint:\n%s", doc)
	}
}

// TestEdgeStructCarriesEveryEdgePropertyTheContractDeclares is the STRUCTURAL
// half, and it is what makes this module's Edge track the contract instead of
// merely resembling it.
//
// THE GAP IT CLOSES, named because it was measured: this module already
// byte-compares its embedded contract COPY against the client's file, and
// already compares its ADVERTISED schema against that copy — but nothing
// compared the Go struct a collector author actually writes to either of them.
// A field added to the schema and forgotten on the struct left every existing
// gate green while the field it declared was unwritable from this framework.
//
// It is derived from the schema rather than listing fields, so a property added
// later joins this gate by being declared.
func TestEdgeStructCarriesEveryEdgePropertyTheContractDeclares(t *testing.T) {
	var schema struct {
		Properties struct {
			Edges struct {
				Items struct {
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"items"`
			} `json:"edges"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(OutputContractJSON(), &schema); err != nil {
		t.Fatalf("decoding the embedded output contract: %v", err)
	}
	declared := schema.Properties.Edges.Items.Properties
	if len(declared) == 0 {
		t.Fatal("the contract declares no edge properties; this gate would pass vacuously")
	}

	tags := map[string]bool{}
	for f := range reflect.TypeFor[Edge]().Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			tags[name] = true
		}
	}

	for property := range declared {
		if !tags[property] {
			t.Errorf("the contract declares the edge property %q and framework.Edge has no field with that json tag, "+
				"so a collector written on this framework cannot emit it", property)
		}
	}
	// THE REVERSE DIRECTION IS DELIBERATELY NOT ASSERTED, and saying so is the
	// point: the contract schema is a FLOOR, not an enumeration. It declares the
	// three required endpoints plus target_graph and leaves weight, confidence,
	// method and evidence undeclared, because a provider is free to be stricter
	// than the contract and the client validates unknown keys against the Go
	// envelope rather than against the schema. A symmetric assertion here would
	// red on four fields that have always been correct.
}
