// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// sink.go — WHERE THE WALK PUTS WHAT IT FOUND, and the two properties that make
// a second collect of an unchanged account write nothing.
//
// DETERMINISM IS NOT A TEST CONVENIENCE HERE. The result is a JSON document the
// client diffs against the previous collect's manifest, keyed on the NODE ID, and
// the walk is a bounded-concurrency fan-out over service walks that finish in
// whatever order the API answers. So the sink is the one place ordering is
// decided: every service appends under a mutex, and the whole set is SORTED once
// at the end. Without that, two identical accounts produce two different
// documents and every consumer sees churn that no resource actually had.
//
// AND NOTHING IN A NODE MAY VARY RUN TO RUN. A collect timestamp, a Go map's
// iteration order inside a serialized content blob, or a counter in an id would
// each survive a set-equality comparison and still defeat the diff. Content is
// therefore built from the SDK's own values through marshalContent below, which
// sorts its keys, and never from a map ranged in place.

// sink accumulates the nodes and edges a walk produces. It is safe for
// concurrent use by the service walks running under the fan-out.
type sink struct {
	mu    sync.Mutex
	nodes []framework.Node
	edges []framework.Edge
	// seenNode is how a resource reachable from two service walks lands once.
	// The EC2 walk and the ELB walk can both name a subnet, and a node emitted
	// twice would be two entries with one id in the result document.
	seenNode map[string]bool
	seenEdge map[string]bool
}

func newSink() *sink {
	return &sink{seenNode: map[string]bool{}, seenEdge: map[string]bool{}}
}

// addNode records one node, ignoring a repeat of an id already present.
//
// FIRST WRITER WINS, deliberately: the walk that OWNS a resource type emits it
// with its full detail, and a later walk that merely references the same id
// (through addRef below) must not overwrite that detail with a stub.
func (s *sink) addNode(n framework.Node) {
	if n.ID == "" || n.Type == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seenNode[n.ID] {
		return
	}
	s.seenNode[n.ID] = true
	s.nodes = append(s.nodes, n)
}

// addEdge records one edge, ignoring an exact repeat.
//
// AN EDGE WHOSE ENDPOINT THIS RESULT DOES NOT CARRY IS KEPT. A rule naming
// another account's security group, an IRSA subject in a Kubernetes graph, a
// CIDR that is not a resource: each is a real relationship, and the contract
// passes a dangling edge through deliberately. Dropping them here would be this
// collector deciding what the write path resolves.
func (s *sink) addEdge(from, to, edgeType string, evidence map[string]string) {
	if from == "" || to == "" || edgeType == "" {
		return
	}
	key := from + "\x00" + to + "\x00" + edgeType
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seenEdge[key] {
		return
	}
	s.seenEdge[key] = true
	s.edges = append(s.edges, framework.Edge{
		FromID:   from,
		ToID:     to,
		Type:     edgeType,
		Method:   collectMethod,
		Evidence: marshalContent(evidence),
	})
}

// collectMethod is stamped on every edge so a consumer can tell an edge this
// collector derived from one a person or another tool wrote.
const collectMethod = "aws-collect"

// result returns the accumulated nodes and edges in a STABLE order.
//
// SORTED BY ID, then by the edge triple: the fan-out's completion order is not
// reproducible and neither is a map's, so this is what makes two collects of one
// unchanged account byte-identical.
func (s *sink) result() ([]framework.Node, []framework.Edge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nodes := slices.Clone(s.nodes)
	edges := slices.Clone(s.edges)
	slices.SortFunc(nodes, func(a, b framework.Node) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(edges, func(a, b framework.Edge) int {
		if c := cmp.Compare(a.FromID, b.FromID); c != 0 {
			return c
		}
		if c := cmp.Compare(a.ToID, b.ToID); c != 0 {
			return c
		}
		return cmp.Compare(a.Type, b.Type)
	})
	return nodes, edges
}

// marshalContent renders a string map as a JSON object with its keys in sorted
// order.
//
// IT ENCODES BY HAND RATHER THAN CALLING json.Marshal, and the reason is a
// property this function cannot be allowed to lose. Two collects of an unchanged
// account must produce byte-identical content, and encoding/json delivers that
// only through its DOCUMENTED map-key sort — a property of that package rather
// than of this code, invisible at the call site, and silently gone the day a
// content shape becomes a struct. Sorting here states the requirement where it
// is needed.
//
// AND IT HAS NO FAILURE PATH AT ALL, which is the second reason. json.Marshal
// returns an error this function has no channel to report on: absorbing it into
// the empty string would make an encoding failure indistinguishable from absent
// input, and threading an error result up through forty construction sites would
// buy a branch that a map[string]string can never reach. Encoding directly
// removes the branch instead of choosing how to lose it. A differential test
// against encoding/json is what keeps the two agreeing.
//
// Nil and empty both render as the empty string rather than as `null` or `{}`, so
// an absent detail is an absent field.
func marshalContent(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSONString(&b, k)
		b.WriteByte(':')
		writeJSONString(&b, m[k])
	}
	b.WriteByte('}')
	return b.String()
}

// writeJSONString writes one JSON string literal, escaped EXACTLY as
// encoding/json escapes it.
//
// BYTE-FOR-BYTE AGREEMENT IS THE POINT, not merely producing valid JSON. A
// differential test compares this encoder against encoding/json over a corpus of
// inputs chosen to break a hand-written escaper, and that comparison is only a
// real external expectation if the target is exact bytes — "both parse to the
// same map" would accept an escaper that mangled a quote inside a value.
//
// THREE RULES ARE EASY TO MISS AND THE TEST FOUND TWO OF THEM:
//
//   - encoding/json HTML-ESCAPES `<`, `>` and `&` by default, as \u003c, \u003e
//     and \u0026. Nothing about JSON requires it; the package does it so a
//     document can be embedded in a script tag, and it is on unless a caller
//     turns it off.
//   - AN INVALID UTF-8 BYTE is written as the six-character escape \ufffd, while a
//     VALID U+FFFD already in the input is written through as its three raw
//     bytes. Telling them apart needs the decoded SIZE, which is why this decodes
//     explicitly rather than using a range loop — a range loop yields RuneError
//     for both and cannot distinguish them.
//   - U+2028 and U+2029 are escaped too, because they terminate a line in
//     JavaScript and would break a document embedded in one.
func writeJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == utf8.RuneError && size == 1:
			// An invalid byte, which encoding/json replaces with the escape.
			b.WriteString(`\ufffd`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20, r == '<', r == '>', r == '&', r == '\u2028', r == '\u2029':
			fmt.Fprintf(b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}
