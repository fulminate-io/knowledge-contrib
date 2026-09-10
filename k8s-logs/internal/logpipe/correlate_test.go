// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// correlate_test.go — THE CLOUD HALF OF THE EMITTED GRAPH, and the projection
// that carries this module's objects into the common detector.
//
// TWO THINGS ARE PINNED HERE THAT A SUBJECT MUST NOT SUPPLY ITSELF. The edge's
// METHOD and TYPE are rendered by the common module from its own constants, so
// asserting them against those constants would ask the subject for its answer
// key and go green on a drift; the bytes are written out instead. And a nil
// element in any of the three projections must SURVIVE to the detector, which
// refuses it by name and index — a projection that quietly shortened its slice
// would swallow that refusal.

// TestEmittedProxyAndCorrelationValues — the cloud half's ids and its edge's
// carried values, all expected from literals.
func TestEmittedProxyAndCorrelationValues(t *testing.T) {
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	apiLabels := map[string]string{"container": "api", "container_pod": "api-1", "namespace": "dev"}
	dbLabels := map[string]string{"container": "db", "container_pod": "db-1", "namespace": "data"}
	entries := []Entry{
		{Timestamp: base, Severity: SeverityError, Message: "upstream call to the store timed out", Labels: apiLabels},
		{Timestamp: base, Severity: SeverityError, Message: "refused a connection past the pool limit", Labels: dbLabels},
	}

	res, err := Build(entries, Options{
		ChunkWindow: time.Hour,
		Resolutions: []Resolution{
			{LabelKey: "namespace", LabelValue: "dev", Account: "acct", ResourceID: "r-api", ResourceType: "k8s_namespace"},
			{LabelKey: "namespace", LabelValue: "data", Account: "acct", ResourceID: "r-db"},
		},
		ProxyMap: map[string]string{"dev": "acct:r-api", "data": "acct:r-db"},
		Resolver: testResolver{
			"dev":  {Account: "acct", ID: "r-api"},
			"data": {Account: "acct", ID: "r-db"},
		},
		Oracle: testOracle{{"r-api", "r-db"}: {}, {"r-db", "r-api"}: {}},
	})
	if err != nil {
		t.Fatal(err)
	}

	proxies := map[string]framework.Node{}
	for _, n := range res.Nodes {
		if n.Type == NodeProxy {
			proxies[n.ID] = n
		}
	}
	for _, want := range []struct{ id, account, foreignID, symbol string }{
		{"proxy:cloud:acct:r-api", "acct", "r-api", "namespace=dev"},
		{"proxy:cloud:acct:r-db", "acct", "r-db", "namespace=data"},
	} {
		n, ok := proxies[want.id]
		if !ok {
			t.Errorf("no proxy node with id %s", want.id)
			continue
		}
		if n.Source != "proxy:cloud:"+want.account {
			t.Errorf("%s has Source %q", want.id, n.Source)
		}
		if n.SymbolName != want.symbol {
			t.Errorf("%s has SymbolName %q, want %q", want.id, n.SymbolName, want.symbol)
		}
		if n.Metadata["foreign_graph"] != "cloud" || n.Metadata["foreign_id"] != want.foreignID {
			t.Errorf("%s carries foreign_graph=%q foreign_id=%q", want.id,
				n.Metadata["foreign_graph"], n.Metadata["foreign_id"])
		}
		if n.Metadata["account"] != want.account {
			t.Errorf("%s carries account=%q", want.id, n.Metadata["account"])
		}
	}

	var correlations int
	for _, e := range res.Edges {
		switch e.Type {
		case EdgeEmittedBy:
			if !strings.HasPrefix(e.FromID, "log-label:namespace=") {
				t.Errorf("EMITTED_BY runs from %q, want a namespace label node", e.FromID)
			}
			if _, ok := proxies[e.ToID]; !ok {
				t.Errorf("EMITTED_BY runs to %q, which is no emitted proxy", e.ToID)
			}
		case EdgeCorrelatesWith:
			correlations++
			// LITERALS, NOT THE CONSTANTS THAT PRODUCED THE EDGE. The common
			// module emits this edge using its own two constants, so comparing
			// against them asks the subject for its own answer key: drifting
			// either one would leave this test green. The bytes are the contract
			// a consumer reads, so the bytes are what is written here.
			if e.Method != "temporal+cloud-dependency" {
				t.Errorf("CORRELATES_WITH method is %q", e.Method)
			}
			if e.Confidence <= 0 || e.Confidence > 1 {
				t.Errorf("CORRELATES_WITH confidence is %v, want a score in (0,1]", e.Confidence)
			}
			for _, want := range []string{"services=", "resources=acct:r-api,acct:r-db", "score="} {
				if !strings.Contains(e.Evidence, want) {
					t.Errorf("CORRELATES_WITH evidence %q does not carry %q", e.Evidence, want)
				}
			}
		}
	}
	if correlations != 1 {
		t.Fatalf("%d CORRELATES_WITH edges, want 1", correlations)
	}
}

// TestThisModulesSeverityVocabularyIsTheCommonDetectorsToo — the two rank
// identically, at every pair.
//
// WHY THIS PIN EXISTS. The ERROR filter is the common detector's specification
// and it reads the detector's OWN rank, over the six canonical names. This
// module writes those same names into a template's severity metadata and ranks
// them with a copy. Two copies of a vocabulary is exactly the shape that drifts
// one spelling at a time, and the drift would be silent: a template this module
// called ERROR and the detector ranked at zero would simply never correlate, and
// nothing would say why.
func TestThisModulesSeverityVocabularyIsTheCommonDetectorsToo(t *testing.T) {
	mine := []string{
		SeverityTrace, SeverityDebug, SeverityInfo, SeverityWarn, SeverityError, SeverityCritical,
	}
	theirs := []string{
		correlation.SeverityTrace, correlation.SeverityDebug, correlation.SeverityInfo,
		correlation.SeverityWarn, correlation.SeverityError, correlation.SeverityCritical,
	}
	for i := range mine {
		if mine[i] != theirs[i] {
			t.Fatalf("name %d is %q here and %q in the common detector", i, mine[i], theirs[i])
		}
	}

	// EVERY PAIR, not just the ordering of the list: two vocabularies can share
	// their names and still rank a pair differently.
	for _, a := range mine {
		for _, b := range mine {
			if got, want := SeverityAtLeast(a, b), correlation.SeverityAtLeast(a, b); got != want {
				t.Errorf("SeverityAtLeast(%q, %q) is %v here and %v in the common detector", a, b, got, want)
			}
		}
	}

	// THE TWO EMITTED CONSTANTS, pinned here because this is where the two
	// modules are compared. The edge's method and type are an informal contract
	// with every consumer — a golden, an operator reading an edge, a test
	// asserting a substring — and both are now rendered by the common module. A
	// test that compared them against that module's own constants would go green
	// on a drift, so the BYTES are written out.
	if EdgeCorrelatesWith != correlation.EdgeCorrelatesWith {
		t.Errorf("the CORRELATES_WITH edge type is %q here and %q in the common detector",
			EdgeCorrelatesWith, correlation.EdgeCorrelatesWith)
	}
	if correlation.EdgeCorrelatesWith != "CORRELATES_WITH" {
		t.Errorf("the common detector's edge type is %q; the emitted bytes are CORRELATES_WITH",
			correlation.EdgeCorrelatesWith)
	}
	if correlation.CorrelationMethod != "temporal+cloud-dependency" {
		t.Errorf("the common detector's correlation method is %q; the emitted bytes are "+
			"temporal+cloud-dependency", correlation.CorrelationMethod)
	}

	// THE ONE COMPARISON THE FILTER ACTUALLY MAKES, spelled out: which of the
	// six reach the detector at all.
	for _, tc := range []struct {
		severity   string
		correlates bool
	}{
		{SeverityTrace, false}, {SeverityDebug, false}, {SeverityInfo, false},
		{SeverityWarn, false}, {SeverityError, true}, {SeverityCritical, true},
		{"", false}, {"NOTICE", false},
	} {
		if got := correlation.SeverityAtLeast(tc.severity, correlation.SeverityError); got != tc.correlates {
			t.Errorf("a %q template reaches the correlation detector: %v, want %v", tc.severity, got, tc.correlates)
		}
	}
}

// TestANilTemplateIsProjectedRatherThanDropped — malformed input reaches the
// detector, which refuses it by name and index.
//
// DROPPING IT HERE WOULD SWALLOW THAT REFUSAL and hand the detector a shorter
// slice than the pipeline produced, which is a silent degrade in the one place
// this collector is meant to be loud. The projection is not a filter.
func TestANilTemplateIsProjectedRatherThanDropped(t *testing.T) {
	templates := []*Template{
		{ID: "a", Severity: SeverityError},
		nil,
		{ID: "b", Severity: SeverityError},
	}
	projected := correlationTemplates(templates)
	if len(projected) != len(templates) {
		t.Fatalf("the projection returned %d of %d templates; a nil element must survive to the detector, "+
			"which refuses it by index", len(projected), len(templates))
	}
	if projected[1] != nil {
		t.Fatal("the nil template was replaced rather than carried through")
	}

	// AND THE DETECTOR REFUSES IT, which is what makes carrying it worth doing.
	_, err := findCorrelations(templates, nil, nil, Options{})
	if err == nil {
		t.Fatal("a nil template reached the detector and was accepted; bad input must error")
	}
}

// TestANilChunkAndANilStreamAreProjectedRatherThanDropped — the same rule as
// for templates, on the two projections that had no observer.
//
// A projection that silently shortens its slice hands the detector fewer
// elements than the pipeline produced, and the refusal that would have named the
// malformed one never happens.
func TestANilChunkAndANilStreamAreProjectedRatherThanDropped(t *testing.T) {
	t.Run("a nil chunk", func(t *testing.T) {
		chunks := []*Chunk{{StreamID: "s", TemplateID: "a"}, nil, {StreamID: "s", TemplateID: "b"}}
		projected := correlationChunks(chunks)
		if len(projected) != len(chunks) {
			t.Fatalf("the projection returned %d of %d chunks", len(projected), len(chunks))
		}
		if projected[1] != nil {
			t.Fatal("the nil chunk was replaced rather than carried through")
		}
		_, err := findCorrelations(
			[]*Template{{ID: "a", Severity: SeverityError}}, chunks,
			[]*Stream{{ID: "s", Labels: map[string]string{"namespace": "dev"}}}, Options{})
		if err == nil {
			t.Fatal("a nil chunk reached the detector and was accepted; bad input must error")
		}
	})

	t.Run("a nil stream", func(t *testing.T) {
		streams := []*Stream{{ID: "s", Labels: map[string]string{"namespace": "dev"}}, nil}
		projected := correlationStreams(streams)
		if len(projected) != len(streams) {
			t.Fatalf("the projection returned %d of %d streams", len(projected), len(streams))
		}
		if projected[1] != nil {
			t.Fatal("the nil stream was replaced rather than carried through")
		}
		_, err := findCorrelations(
			[]*Template{{ID: "a", Severity: SeverityError}},
			[]*Chunk{{StreamID: "s", TemplateID: "a"}}, streams, Options{})
		if err == nil {
			t.Fatal("a nil stream reached the detector and was accepted; bad input must error")
		}
	})
}
