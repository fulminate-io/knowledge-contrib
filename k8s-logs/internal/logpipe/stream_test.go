// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// stream_test.go — stream identity, the fingerprint's separate input, the
// label node id's literal form, and the cardinality split.

// TestStreamIDIsTheFullHashOfEveryLabel pins the shape and the input.
func TestStreamIDIsTheFullHashOfEveryLabel(t *testing.T) {
	id := FingerprintLabels(map[string]string{"container": "api", "container_pod": "api-1"})
	if len(id) != 64 {
		t.Fatalf("stream id is %d characters (%q); a stream id is the FULL 64-hex sha256, unlike a template id", len(id), id)
	}
	if strings.Contains(id, ":") {
		t.Fatalf("stream id %q carries a prefix; a stream id is bare", id)
	}
	same := FingerprintLabels(map[string]string{"container_pod": "api-1", "container": "api"})
	if same != id {
		t.Fatal("the stream id depends on map iteration order; keys must be sorted before hashing")
	}
	if FingerprintLabels(map[string]string{"container": "api", "container_pod": "api-2"}) == id {
		t.Fatal("two different label sets hash to one stream id")
	}
}

// TestFingerprintUsesOnlyTheLowCardinalityLabels is the property that
// disappears silently if both hashes are fed the same map.
func TestFingerprintUsesOnlyTheLowCardinalityLabels(t *testing.T) {
	tracker := NewCardinalityTracker(2)
	// `container_pod` crosses the threshold; `container` does not.
	for _, pod := range []string{"api-1", "api-2", "api-3"} {
		tracker.Observe("container_pod", pod)
		tracker.Observe("container", "api")
	}
	a := NewStream(map[string]string{"container": "api", "container_pod": "api-1"}, tracker)
	b := NewStream(map[string]string{"container": "api", "container_pod": "api-2"}, tracker)

	if a.ID == b.ID {
		t.Fatal("two pods share a stream id; the id must hash the COMPLETE label set")
	}
	if a.Fingerprint != b.Fingerprint {
		t.Fatalf("two pods of one container have different fingerprints (%q, %q); "+
			"the fingerprint must hash the LOW-CARDINALITY labels alone, which is what makes it a grouping",
			a.Fingerprint, b.Fingerprint)
	}
	if a.Fingerprint == a.ID {
		t.Fatal("the fingerprint equals the id, so both were hashed over the same map and the grouping is lost")
	}
	if _, high := a.HighCardLabels["container_pod"]; !high {
		t.Fatalf("container_pod was classified low-cardinality at threshold 2 over three values: %v", a.LowCardLabels)
	}
}

// TestLabelNodeIDIsALiteral — not a hash, and readable on an edge.
func TestLabelNodeIDIsALiteral(t *testing.T) {
	if got, want := LabelNodeID("namespace", "dev"), "log-label:namespace=dev"; got != want {
		t.Fatalf("LabelNodeID = %q, want the literal %q", got, want)
	}
}

// TestCardinalityDecidesWhetherALabelBecomesANode is the structural half: the
// same label set produces a different GRAPH on either side of the threshold.
func TestCardinalityDecidesWhetherALabelBecomesANode(t *testing.T) {
	entries := []Entry{
		{Message: "a 1111", Labels: map[string]string{"namespace": "dev", "container_pod": "api-1"}},
		{Message: "a 2222", Labels: map[string]string{"namespace": "dev", "container_pod": "api-2"}},
		{Message: "a 3333", Labels: map[string]string{"namespace": "dev", "container_pod": "api-3"}},
	}

	shared, _ := BuildStreams(entries, 0)
	sharedNodes := buildLabelNodes(shared)
	if !hasLabelNode(sharedNodes, "log-label:namespace=dev") {
		t.Fatal("the shared namespace did not become a label node under the default threshold")
	}
	if !hasLabelNode(sharedNodes, "log-label:container_pod=api-1") {
		t.Fatal("a low-cardinality pod key did not become a label node under the default threshold")
	}

	split, _ := BuildStreams(entries, 2)
	splitNodes := buildLabelNodes(split)
	if !hasLabelNode(splitNodes, "log-label:namespace=dev") {
		t.Fatal("the shared namespace stopped being a label node at threshold 2; it has one value")
	}
	if hasLabelNode(splitNodes, "log-label:container_pod=api-1") {
		t.Fatal("a high-cardinality pod key still became a label node; it must ride inline on the stream instead")
	}
	for _, s := range split {
		if _, inline := s.HighCardLabels["container_pod"]; !inline {
			t.Fatalf("stream %s did not carry the high-cardinality pod key inline", s.ID)
		}
	}
}

func hasLabelNode(nodes []framework.Node, id string) bool {
	for _, n := range nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

// TestBuildStreamsIsOrderStable — the emitted order must be a function of the
// input, not of Go's map iteration.
func TestBuildStreamsIsOrderStable(t *testing.T) {
	entries := []Entry{
		{Message: "a", Labels: map[string]string{"container_pod": "c"}},
		{Message: "b", Labels: map[string]string{"container_pod": "a"}},
		{Message: "c", Labels: map[string]string{"container_pod": "b"}},
	}
	first, _ := BuildStreams(entries, 0)
	for range 20 {
		again, _ := BuildStreams(entries, 0)
		if len(again) != len(first) {
			t.Fatalf("stream count moved between runs: %d then %d", len(first), len(again))
		}
		for j := range first {
			if first[j].ID != again[j].ID {
				t.Fatalf("stream order moved between runs at position %d: %s then %s", j, first[j].ID, again[j].ID)
			}
		}
	}
}
