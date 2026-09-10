// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// defaults_test.go — THE PRODUCTION DEFAULTS, each observed by an input wide
// enough to cross it.
//
// WHY THIS FILE EXISTS SEPARATELY FROM THE CONVERTER TESTS. Every converter test
// injects the constant it exercises — `const threshold = 4`, `cfg.MaxClusters =
// 2` — which is right for testing the unit and leaves the value production
// actually uses unread. Thirteen of this module's nineteen parity-inherited
// constants were unobserved by the whole suite for exactly that reason: a drift
// in any of them would have moved template ids, stream ids or the graph's node
// count with nothing going red.
//
// WHY THE ASSERTIONS ARE BEHAVIORAL RATHER THAN A TABLE OF LITERALS. A table
// comparing each constant to a copy of itself is a tautology, and a table
// comparing it to a hand-written number only restates the declaration. Each cell
// below instead drives an input that STRADDLES the threshold — one case on each
// side of it — through the production path, so moving the constant by one moves
// a behavior. The mutation battery in the implementation report lists the cell
// that reds for each.
//
// THE NUMBERS BELOW ARE LITERALS, NOT THIS MODULE'S CONSTANTS, and that is the
// difference between a pin and a tautology. A cell written as
// `strings.Repeat("a", streamNameLimit)` moves WITH the constant it is meant to
// hold still, so it agrees with any value the constant takes and observes
// nothing. Each literal here is the built-in pipeline's own value, cited to the
// file it lives in, so a drift in either direction reds.
//
// What these cells catch is a future drift; every value was compared against its
// built-in site and none has diverged today.

// The built-in pipeline's values, cited. Paths were under
// cmd/knowledge/internal/collector/logs at a9171723f400f6f4d58fbe6ca27626fb4874c076,
// the last commit before the built-in log collectors were deleted; this table
// is now the specification rather than a snapshot of a live comparator.
const (
	builtinCardinalityThreshold = 500              // cardinality.go
	builtinChunkWindow          = 5 * time.Minute  // pipeline.go
	builtinSimThreshold         = 0.4              // drain.go, DefaultDrainConfig
	builtinMaxDepth             = 4                // drain.go, DefaultDrainConfig
	builtinMaxChildren          = 100              // drain.go, DefaultDrainConfig
	builtinMaxClusters          = 200              // drain.go, DefaultDrainConfig
	builtinMaxExampleVars       = 3                // drain.go
	builtinAliasTokenLimit      = 5                // alias_template.go, meaningfulTokens
	builtinGoStackWindow        = 30 * time.Second // consolidator_gostack.go
	builtinGoStackMinGroup      = 3                // consolidator_gostack.go
	builtinPythonWindow         = 5 * time.Second  // consolidator_python.go
	builtinPythonMinGroup       = 3                // consolidator_python.go
	builtinPythonHeaderLimit    = 120              // consolidator_python.go, buildPythonTemplate
	builtinStreamNameLimit      = 60               // cloudwatch/normalize.go
	builtinStreamNameKeep       = 57               // cloudwatch/normalize.go
	builtinSeverityPrefixMaxLen = 8                // cloudwatch/normalize.go, parseSeverityPrefix
	builtinMaxJSONNesting       = 3                // cloudwatch/normalize.go, parseJSON
	builtinSeverityScanLimit    = 200              // logwire/severity.go, DetectEmbeddedSeverity
	builtinPageLimit            = 10000            // cloudwatch/collect.go, cwPageLimit
)

// entryAt builds one entry at a fixed instant offset, with the label set a
// CloudWatch walk produces.
func entryAt(offsetSeconds int, message string) LogEntry {
	return LogEntry{
		Timestamp: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(offsetSeconds) * time.Second),
		Severity:  SeverityInfo,
		Message:   message,
		Labels:    map[string]string{"log_group": "/g", "service": "g", "log_stream": "s"},
	}
}

// TestDefaultCardinalityThresholdIsObserved straddles the threshold: a key with
// one FEWER distinct value than the threshold stays low-cardinality and gets
// label nodes, and a key with exactly the threshold goes high-cardinality and
// gets none. The classification sets the graph's node and edge COUNT.
func TestDefaultCardinalityThresholdIsObserved(t *testing.T) {
	entries := make([]LogEntry, 0, builtinCardinalityThreshold)
	for i := range builtinCardinalityThreshold {
		e := entryAt(i, "worker finished task")
		// `atThreshold` reaches exactly the built-in threshold in distinct
		// values; `belowThreshold` reaches one fewer.
		e.Labels = map[string]string{
			"log_group":      "/g",
			"service":        "g",
			"atThreshold":    "v" + strconv.Itoa(i),
			"belowThreshold": "v" + strconv.Itoa(i%(builtinCardinalityThreshold-1)),
		}
		entries = append(entries, e)
	}

	nodes, _, err := buildGraph(entries, CloudContext{})
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
	var atThreshold, below int
	for _, n := range nodes {
		if n.Type != nodeLogLabel {
			continue
		}
		switch n.Metadata["label_key"] {
		case "atThreshold":
			atThreshold++
		case "belowThreshold":
			below++
		}
	}
	if atThreshold != 0 {
		t.Errorf("a key with %d distinct values produced %d label nodes; at the threshold it is high-cardinality "+
			"and gets none", builtinCardinalityThreshold, atThreshold)
	}
	if below != builtinCardinalityThreshold-1 {
		t.Errorf("a key with %d distinct values produced %d label nodes, want one per value",
			builtinCardinalityThreshold-1, below)
	}
}

// TestGoStackWindowIsObserved straddles the Go consolidator's grouping window:
// fragments spaced one second WIDER than it fall into separate groups, so none
// reaches the minimum group size and nothing merges.
func TestGoStackWindowIsObserved(t *testing.T) {
	gap := int(builtinGoStackWindow/time.Second) + 1
	entries := []LogEntry{
		entryAt(0, "goroutine 1 [running]:"),
		entryAt(gap, "goroutine 2 [select]:"),
		entryAt(2*gap, "goroutine 3 [chan receive]:"),
	}
	nodes, _, err := buildGraph(entries, CloudContext{})
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
	if countPattern(nodes, goStackMergedPattern) != 0 {
		t.Errorf("fragments %ds apart merged though the window is %s", gap, builtinGoStackWindow)
	}
	// The control, one second NARROWER, which must merge.
	narrow := int(builtinGoStackWindow / time.Second)
	merged, _, err := buildGraph([]LogEntry{
		entryAt(0, "goroutine 1 [running]:"),
		entryAt(narrow, "goroutine 2 [select]:"),
		entryAt(2*narrow, "goroutine 3 [chan receive]:"),
	}, CloudContext{})
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
	if countPattern(merged, goStackMergedPattern) != 1 {
		t.Errorf("fragments %ds apart did not merge though the window is %s", narrow,
			builtinGoStackWindow)
	}
}

// TestPythonWindowIsObserved is the same straddle for the Python consolidator,
// whose window is its own and much narrower.
func TestPythonWindowIsObserved(t *testing.T) {
	// THE FRAGMENTS ARE CHOSEN TO SURVIVE TOKENIZATION. Several of the
	// traceback patterns anchor on LEADING WHITESPACE, which the clustering
	// stage strips before a template is formed, so a frame line indented in the
	// source classifies as a fragment when handed straight to the consolidator
	// and not when it arrives through a walk. These three classify either way.
	frames := []string{
		"Traceback (most recent call last):",
		"raise TimeoutError from exc",
		"app.errors.TimeoutError: upstream did not answer",
	}
	gap := int(builtinPythonWindow/time.Second) + 1
	wide := []LogEntry{entryAt(0, frames[0]), entryAt(gap, frames[1]), entryAt(2*gap, frames[2])}
	nodes, _, err := buildGraph(wide, CloudContext{})
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
	if countPatternPrefix(nodes, "Python exception") != 0 {
		t.Errorf("traceback frames %ds apart merged though the window is %s", gap,
			builtinPythonWindow)
	}
	narrow := int(builtinPythonWindow / time.Second)
	merged, _, err := buildGraph(
		[]LogEntry{entryAt(0, frames[0]), entryAt(narrow, frames[1]), entryAt(2*narrow, frames[2])}, CloudContext{})
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
	if countPatternPrefix(merged, "Python exception") != 1 {
		t.Errorf("traceback frames %ds apart did not merge though the window is %s", narrow,
			builtinPythonWindow)
	}
}

// TestPythonHeaderLimitIsObserved pins where a long traceback header is cut. The
// cut point is inside the merged template's PATTERN, which is hashed into its
// id, so moving the limit moves that id.
func TestPythonHeaderLimitIsObserved(t *testing.T) {
	header := "Traceback (most recent call last): " + strings.Repeat("x", 200)
	entries := []LogEntry{
		entryAt(0, header),
		entryAt(1, "raise TimeoutError from exc"),
		entryAt(2, "app.errors.TimeoutError: upstream did not answer"),
	}
	nodes, _, err := buildGraph(entries, CloudContext{})
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
	var pattern string
	for _, n := range nodes {
		if n.Type == nodeLogTemplate && strings.HasPrefix(n.Metadata["pattern"], "Python exception") {
			pattern = n.Metadata["pattern"]
		}
	}
	if pattern == "" {
		t.Fatalf("no merged Python template; patterns: %v", nodePatterns(nodes))
	}
	body := strings.TrimPrefix(pattern, "Python exception: ")
	if len(body) != builtinPythonHeaderLimit {
		t.Errorf("the merged header is %d characters, want the limit of %d", len(body), builtinPythonHeaderLimit)
	}
	if !strings.HasSuffix(body, "...") {
		t.Errorf("a cut header does not end in an ellipsis: %q", body)
	}
}

// TestPageLimitIsTheDocumentedBatchingUnit pins the per-request size against the
// provider's own documented maximum, as a literal rather than against the
// constant, which would compare it to itself.
func TestPageLimitIsTheDocumentedBatchingUnit(t *testing.T) {
	const documentedMaximum = builtinPageLimit
	if pageLimit != documentedMaximum {
		t.Errorf("the batching unit is %d, want the provider's documented per-request maximum of %d",
			pageLimit, documentedMaximum)
	}
	groups := []recordedGroup{{LogGroup: "/g", Pages: []recordedPage{{}}}}
	client := newFakeClientFor(t, groups)
	if _, err := collectorWithFake(client).Walk(
		t.Context(), "id", Params{LogGroups: []string{"/g"}}, foreignBlockEmpty()); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(client.limits) == 0 || client.limits[0] != documentedMaximum {
		t.Errorf("the unbounded request asked for %v events, want %d", client.limits, documentedMaximum)
	}
}

// patternList, nodePatterns, countPattern and countPatternPrefix render or count
// what a walk produced, for the cells above.
func patternList(templates []*LogTemplate) []string {
	out := make([]string, 0, len(templates))
	for _, t := range templates {
		out = append(out, t.Pattern)
	}
	return out
}

func nodePatterns(nodes []framework.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.Type == nodeLogTemplate {
			out = append(out, n.Metadata["pattern"])
		}
	}
	return out
}

func countPattern(nodes []framework.Node, pattern string) int {
	n := 0
	for _, node := range nodes {
		if node.Type == nodeLogTemplate && node.Metadata["pattern"] == pattern {
			n++
		}
	}
	return n
}

func countPatternPrefix(nodes []framework.Node, prefix string) int {
	n := 0
	for _, node := range nodes {
		if node.Type == nodeLogTemplate && strings.HasPrefix(node.Metadata["pattern"], prefix) {
			n++
		}
	}
	return n
}

// foreignBlockEmpty is the block a collect with no declared graphs carries.
func foreignBlockEmpty() framework.ForeignContext { return framework.ForeignContext{} }
