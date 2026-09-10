// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
)

// correlate_test.go — the temporal half of correlation, which this module
// computes from the collected entries alone, and the confirmation half, which it
// cannot.

// stubCloud answers the two cloud questions from fixed tables. It stands in for
// the operator's cloud graph in the arms whose subject is what the module does
// WITH an answer, never for the route by which a real answer would arrive.
type stubCloud struct {
	services  map[string]resolvedResource
	dependent map[string]bool
	// streams records the stream each resolution was handed, which is the only
	// way to observe that the adapter hands back THIS module's own stream rather
	// than the projection it built for the detector.
	streams []*logStream
}

func (s *stubCloud) ResolveService(stream *logStream, _, value string) (resolvedResource, bool) {
	s.streams = append(s.streams, stream)
	r, ok := s.services[value]
	return r, ok
}

func (s *stubCloud) HasDependency(a, b resolvedResource) bool {
	return s.dependent[a.ID+"->"+b.ID] || s.dependent[b.ID+"->"+a.ID]
}

// TestCorrelationIsCrossServiceAndErrorOnly covers the two filters that keep the
// candidate set meaningful: pairing every heartbeat with every other one, or
// pairing two errors inside one service, would both make the edge type useless.
func TestCorrelationIsCrossServiceAndErrorOnly(t *testing.T) {
	out := correlationsFor(t, crossServiceErrorEntries(), nil)
	if len(out) != 1 {
		t.Fatalf("got %d candidate pairs, want 1: %+v", len(out), out)
	}
	if out[0].ServiceA == out[0].ServiceB {
		t.Errorf("a same-service pair was produced: %+v", out[0])
	}

	// Same-service errors: no pair at all.
	sameService := []logEntry{
		testEntry(0, severityError, "connect failed here", labels("service", "api")),
		testEntry(time.Second, severityError, "queue drain failed there", labels("service", "api")),
	}
	if got := correlationsFor(t, sameService, nil); len(got) != 0 {
		t.Errorf("two errors in one service produced %d pairs: %+v", len(got), got)
	}

	// Cross-service INFO: no pair either.
	infoOnly := []logEntry{
		testEntry(0, severityInfo, "connect ok here", labels("service", "api")),
		testEntry(time.Second, severityInfo, "queue drained there", labels("service", "worker")),
	}
	if got := correlationsFor(t, infoOnly, nil); len(got) != 0 {
		t.Errorf("two cross-service INFO templates produced %d pairs: %+v", len(got), got)
	}
}

// TestAnUnconfirmedPairIsProducedButEmitsNothing is the split between the two
// halves: the temporal fact is recorded, the edge is not written.
func TestAnUnconfirmedPairIsProducedButEmitsNothing(t *testing.T) {
	out := correlationsFor(t, crossServiceErrorEntries(), nil)
	if len(out) != 1 {
		t.Fatalf("want one candidate, got %d", len(out))
	}
	if out[0].StructurallyConfirmed {
		t.Errorf("a pair was confirmed with no cloud context")
	}
	if edges := correlation.MaterializeCorrelations(out); len(edges) != 0 {
		t.Errorf("an unconfirmed pair emitted %d edges", len(edges))
	}
}

// TestAPairIsConfirmedWhenTheCloudSaysTheResourcesDepend is the positive half,
// with the non-dependent pair as its same-run control.
func TestAPairIsConfirmedWhenTheCloudSaysTheResourcesDepend(t *testing.T) {
	cloud := &stubCloud{
		services: map[string]resolvedResource{
			"api":    {Account: "acct", ID: "res-api"},
			"worker": {Account: "acct", ID: "res-worker"},
		},
		dependent: map[string]bool{"res-api->res-worker": true},
	}
	confirmed := correlationsFor(t, crossServiceErrorEntries(), cloud)
	if len(confirmed) != 1 || !confirmed[0].StructurallyConfirmed {
		t.Fatalf("the dependent pair was not confirmed: %+v", confirmed)
	}
	if edges := correlation.MaterializeCorrelations(confirmed); len(edges) != 1 {
		t.Errorf("a confirmed pair emitted %d edges, want 1", len(edges))
	}

	cloud.dependent = nil
	unconfirmed := correlationsFor(t, crossServiceErrorEntries(), cloud)
	if len(unconfirmed) != 1 || unconfirmed[0].StructurallyConfirmed {
		t.Fatalf("a non-dependent pair was confirmed: %+v", unconfirmed)
	}
}

// TestCorrelationResultsAreSorted pins the deterministic order, so the emitted
// edge set does not depend on map or slice iteration.
func TestCorrelationResultsAreSorted(t *testing.T) {
	entries := []logEntry{
		testEntry(0, severityError, "zebra failed badly", labels("service", "zeta")),
		testEntry(time.Second, severityError, "alpha failed badly", labels("service", "alpha")),
		testEntry(2*time.Second, severityError, "middle failed badly", labels("service", "mid")),
	}
	out := correlationsFor(t, entries, nil)
	if len(out) < 2 {
		t.Fatalf("want several pairs, got %d", len(out))
	}
	for i := 1; i < len(out); i++ {
		if out[i-1].ServiceA > out[i].ServiceA {
			t.Errorf("the results are not sorted by ServiceA: %v then %v", out[i-1].ServiceA, out[i].ServiceA)
		}
	}
}

// TestThisModulesSeverityVocabularyIsTheCommonDetectorsToo is the seam pin
// between two copies of one vocabulary.
//
// THE FILTER MOVED AND THE SPELLING DID NOT. The common detector keeps only
// templates at ERROR or above, and it reads the severity string this module
// writes into a Template. Nothing in the compiler relates the two vocabularies,
// so a rename or a re-ordering on either side would silently stop correlating
// this collector's errors while every other test stayed green. This arm relates
// them: same six names, same rank, same verdict at the one comparison the filter
// actually makes.
func TestThisModulesSeverityVocabularyIsTheCommonDetectorsToo(t *testing.T) {
	levels := []string{severityTrace, severityDebug, severityInfo, severityWarn, severityError, severityCritical}
	common := []string{
		correlation.SeverityTrace, correlation.SeverityDebug, correlation.SeverityInfo,
		correlation.SeverityWarn, correlation.SeverityError, correlation.SeverityCritical,
	}
	for i, name := range levels {
		if name != common[i] {
			t.Errorf("severity %d is %q here and %q in the common detector", i, name, common[i])
		}
	}
	for _, level := range levels {
		for _, min := range levels {
			if got, want := correlation.SeverityAtLeast(level, min), severityAtLeast(level, min); got != want {
				t.Errorf("SeverityAtLeast(%q, %q) = %v in the common detector and %v here", level, min, got, want)
			}
		}
	}
	// THE ONE COMPARISON THE FILTER MAKES, stated on its own so a reader sees
	// which way it must go.
	if !correlation.SeverityAtLeast(severityError, correlation.SeverityError) {
		t.Error("this module's ERROR does not satisfy the common detector's ERROR minimum")
	}
	if correlation.SeverityAtLeast(severityWarn, correlation.SeverityError) {
		t.Error("this module's WARN satisfies the common detector's ERROR minimum")
	}
}

// TestServiceLabelPairsAreDeduplicatedByKeyAndValue is the reason a fully
// duplicated EMITTED_BY edge cannot arise: two streams tagged with one service
// are one service and one pair.
func TestServiceLabelPairsAreDeduplicatedByKeyAndValue(t *testing.T) {
	tracker := newCardinalityTracker(0)
	streams := []*logStream{
		newLogStream(labels("service", "api", "pod_name", "a"), tracker),
		newLogStream(labels("service", "api", "pod_name", "b"), tracker),
		newLogStream(labels("service", "worker"), tracker),
	}
	pairs := serviceLabelPairs(streams)
	if len(pairs) != 2 {
		t.Fatalf("got %d pairs, want 2: %+v", len(pairs), pairs)
	}
}

// TestOnlyServiceIdentifyingKeysAreResolved pins the closed key set. `host` and
// `pod_name` are deliberately outside it: an instance is not a service, and
// resolving one would point the correlation pass at the wrong resource.
func TestOnlyServiceIdentifyingKeysAreResolved(t *testing.T) {
	for _, key := range []string{"service", "namespace", "deployment", "app"} {
		if !isServiceIdentifyingKey(key) {
			t.Errorf("%s is not treated as service-identifying", key)
		}
	}
	for _, key := range []string{"host", "pod_name", "project_id", "resource_type", "log_name"} {
		if isServiceIdentifyingKey(key) {
			t.Errorf("%s is treated as service-identifying", key)
		}
	}
}

// TestAnEmptyValuedServiceLabelIsNotResolved is the guard against asking the
// cloud graph about the empty string.
func TestAnEmptyValuedServiceLabelIsNotResolved(t *testing.T) {
	tracker := newCardinalityTracker(0)
	streams := []*logStream{newLogStream(labels("service", "", "app", "web"), tracker)}
	pairs := serviceLabelPairs(streams)
	if len(pairs) != 1 || pairs[0].key != "app" {
		t.Fatalf("got %+v, want only the app pair", pairs)
	}
}

// TestComputeStreamResolutionsSkipsWhatTheCloudDoesNotResolve covers the miss
// arm, with the hit in the same run: a label the context cannot place must be
// skipped rather than emitted with an empty account.
func TestComputeStreamResolutionsSkipsWhatTheCloudDoesNotResolve(t *testing.T) {
	tracker := newCardinalityTracker(0)
	streams := []*logStream{
		newLogStream(labels("service", "api"), tracker),
		newLogStream(labels("service", "unknown"), tracker),
	}
	cloud := &stubCloud{services: map[string]resolvedResource{"api": {Account: "acct", ID: "res-api"}}}
	got := computeStreamResolutions(streams, cloud)
	if len(got) != 1 {
		t.Fatalf("got %d resolutions, want 1: %+v", len(got), got)
	}
	if got[0].LabelValue != "api" {
		t.Errorf("the unresolved label was emitted: %+v", got[0])
	}
	if resolutions := computeStreamResolutions(streams, nil); resolutions != nil {
		t.Errorf("a nil cloud context produced %d resolutions", len(resolutions))
	}
}

// crossServiceErrorEntries is one error in each of two services, close enough in
// time to overlap.
func crossServiceErrorEntries() []logEntry {
	return []logEntry{
		testEntry(0, severityError, "connect failed to upstream", labels("service", "api")),
		testEntry(time.Second, severityError, "queue drain failed here", labels("service", "worker")),
	}
}

// correlationsFor runs the real pipeline and returns its correlation candidates.
func correlationsFor(t *testing.T, entries []logEntry, cloud cloudContext) []correlation.Result {
	t.Helper()
	out, err := runPipeline(entries, defaultPipelineConfig(), cloud)
	if err != nil {
		t.Fatalf("running the pipeline: %v", err)
	}
	return out.Correlations
}

// TestTheResolverIsHandedThisModulesOwnStream is the adapter's mapping, which
// nothing else observes.
//
// THE DETECTOR SEES A PROJECTION, NOT A STREAM. It is handed
// correlation.Stream values carrying an id and a label set, because it may not
// know this module's types — and this module's resolver signature takes a
// *logStream, whose surrounding context labels its own doc comment says are
// load-bearing. The adapter closes that gap by indexing back from the projection
// to the original. Hand it a nil instead and every resolver that reads the
// stream silently loses its context, with no other test the poorer.
func TestTheResolverIsHandedThisModulesOwnStream(t *testing.T) {
	cloud := &stubCloud{services: map[string]resolvedResource{
		"api":    {Account: "acct", ID: "res-api"},
		"worker": {Account: "acct", ID: "res-worker"},
	}}
	if got := correlationsFor(t, crossServiceErrorEntries(), cloud); len(got) != 1 {
		t.Fatalf("want one candidate, got %d", len(got))
	}
	if len(cloud.streams) == 0 {
		t.Fatal("the resolver was never called, so this arm observes nothing")
	}
	for i, s := range cloud.streams {
		if s == nil {
			t.Fatalf("resolution %d was handed a nil stream", i)
		}
		if len(s.Labels) == 0 || s.LowCardLabels == nil {
			t.Errorf("resolution %d was handed something other than this module's own stream: %+v", i, s)
		}
	}
}

// TestMalformedTemplatesReachTheDetectorAndAreRefused is the bad-input arm at
// this module's boundary. The projections carry no guard of their own, so a
// malformed template arrives at the detector, which names it — rather than being
// dropped here and correlating a set the pipeline never produced.
func TestMalformedTemplatesReachTheDetectorAndAreRefused(t *testing.T) {
	good := &logTemplate{ID: "tpl-good", Severity: severityError, FirstSeen: baseTime, LastSeen: baseTime}
	_, err := findCorrelations([]*logTemplate{good, nil}, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("a nil template was not refused")
	}
	if !strings.Contains(err.Error(), "Templates[1] is nil") {
		t.Errorf("the refusal does not name the condition: %v", err)
	}
	empty := &logTemplate{Severity: severityError}
	if _, err := findCorrelations([]*logTemplate{good, empty}, nil, nil, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "has an empty ID") {
		t.Errorf("a template with no id was not refused by name: %v", err)
	}
}
