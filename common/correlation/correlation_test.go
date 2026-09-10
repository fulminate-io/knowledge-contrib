// SPDX-License-Identifier: Apache-2.0

package correlation

import (
	"testing"
	"time"
)

// correlation_test.go — THE PARITY ARMS, derived from the parity target's own
// suite rather than enumerated by hand.
//
// The target is cmd/knowledge/internal/collector/logs at
// 169fc33a8f71d8a94bce0c5604159940f022b64f: ten arms in
// pipeline_correlation_test.go, one in pipeline_test.go and two in
// materialize_proxies_test.go. Thirteen of its fourteen correlation arms have a
// counterpart here under the target's own name. The fourteenth,
// TestPipeline_Summary_NoCorrelationsSectionWhenEmpty, observes the built-in
// PIPELINE's summary rendering; this module returns a slice and renders no
// summary — each collector renders its own — so there is no behaviour of this
// module for it to observe.

func TestTemporalOverlap_Overlapping(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	a := templateAt("a", SeverityError, base, 5*time.Minute)
	b := templateAt("b", SeverityError, base.Add(2*time.Minute), 5*time.Minute)

	score, ok := temporalOverlap(a, b, testWindow)
	if !ok {
		t.Fatal("expected overlap, got none")
	}
	if score <= 0 || score > 1 {
		t.Errorf("score out of range: %f", score)
	}
}

func TestTemporalOverlap_Disjoint(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	a := templateAt("a", SeverityError, base, time.Second)
	// b is an hour later — half-window padding cannot close the gap.
	b := templateAt("b", SeverityError, base.Add(time.Hour), time.Second)

	if _, ok := temporalOverlap(a, b, testWindow); ok {
		t.Fatal("expected disjoint, got overlap")
	}
}

func TestTemporalOverlap_PointEventsWithinWindow(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	a := templateAt("a", SeverityError, base, 0)
	b := templateAt("b", SeverityError, base.Add(20*time.Second), 0)

	score, ok := temporalOverlap(a, b, testWindow)
	if !ok {
		t.Fatal("expected point events within window to overlap")
	}
	// THE EXPECTATION IS COMPUTED FROM THE FIXTURE, NOT READ BACK. Two point
	// events 20s apart pad to [base-30s, base+30s] and [base-10s, base+50s];
	// the shared span is 40s and the wider range is 60s.
	want := 40.0 / 60.0
	if diff := score - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("score = %f, want %f", score, want)
	}
}

func TestFindCorrelations_WithOracle_Confirmed(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	apiStream, dbStream := streamFor("api"), streamFor("db")
	apiTmpl := templateAt("tpl-api-err", SeverityError, base, 5*time.Minute)
	dbTmpl := templateAt("tpl-db-err", SeverityError, base.Add(2*time.Minute), 5*time.Minute)

	oracle := newOracle([2]string{"arn:api", "arn:db"})
	results, err := FindCorrelations(Input{
		Templates: []*Template{apiTmpl, dbTmpl},
		Chunks:    []*Chunk{chunkFor(apiStream, apiTmpl), chunkFor(dbStream, dbTmpl)},
		Streams:   []*Stream{apiStream, dbStream},
		ProxyMap:  map[string]string{"api": "acct:arn:api", "db": "acct:arn:db"},
		Resolver:  newResolver(map[string]string{"api": "arn:api", "db": "arn:db"}),
		Oracle:    oracle,
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 correlation, got %d", len(results))
	}
	r := results[0]
	if !r.StructurallyConfirmed {
		t.Errorf("expected confirmed, got %+v", r)
	}
	if r.ServiceA == r.ServiceB {
		t.Errorf("expected a cross-service correlation, got %s and %s", r.ServiceA, r.ServiceB)
	}
	if r.ResourceA != "acct:arn:api" || r.ResourceB != "acct:arn:db" {
		t.Errorf("resource labels came from somewhere other than the proxy map: %+v", r)
	}
	if oracle.calls() == 0 {
		t.Error("expected HasDependency to be invoked")
	}
}

func TestFindCorrelations_WithOracle_Unconfirmed(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	apiStream, workerStream := streamFor("api"), streamFor("worker")
	apiTmpl := templateAt("tpl-api-err", SeverityError, base, 5*time.Minute)
	workerTmpl := templateAt("tpl-worker-err", SeverityError, base.Add(time.Minute), 5*time.Minute)

	// The oracle knows api↔db, and nothing about api↔worker.
	results, err := FindCorrelations(Input{
		Templates: []*Template{apiTmpl, workerTmpl},
		Chunks:    []*Chunk{chunkFor(apiStream, apiTmpl), chunkFor(workerStream, workerTmpl)},
		Streams:   []*Stream{apiStream, workerStream},
		ProxyMap:  map[string]string{"api": "acct:arn:api", "worker": "acct:arn:worker"},
		Resolver:  newResolver(map[string]string{"api": "arn:api", "worker": "arn:worker"}),
		Oracle:    newOracle([2]string{"arn:api", "arn:db"}),
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(results))
	}
	if results[0].StructurallyConfirmed {
		t.Errorf("expected unconfirmed for api and worker, got %+v", results[0])
	}
}

func TestFindCorrelations_NilOracle(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	apiStream, dbStream := streamFor("api"), streamFor("db")
	apiTmpl := templateAt("a", SeverityError, base, 5*time.Minute)
	dbTmpl := templateAt("b", SeverityError, base.Add(time.Minute), 5*time.Minute)

	// A NIL ORACLE AND A NIL RESOLVER ARE MEANINGFUL INPUTS, not bad ones: they
	// are the collect that carried no declared cloud context.
	results, err := FindCorrelations(Input{
		Templates: []*Template{apiTmpl, dbTmpl},
		Chunks:    []*Chunk{chunkFor(apiStream, apiTmpl), chunkFor(dbStream, dbTmpl)},
		Streams:   []*Stream{apiStream, dbStream},
		ProxyMap:  map[string]string{"api": "arn:api", "db": "arn:db"},
	})
	if err != nil {
		t.Fatalf("a nil resolver and a nil oracle must not be an error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(results))
	}
	if results[0].StructurallyConfirmed {
		t.Errorf("a nil oracle confirms nothing, got %+v", results[0])
	}

	// AND THE ORACLE GUARD ON ITS OWN, with a resolver that DOES resolve. Without
	// this arm the run above never reaches the oracle at all — the unresolved-pair
	// guard returns first — so the nil-oracle guard would be unobserved and a
	// detector that called through a nil interface would still pass.
	results, err = FindCorrelations(Input{
		Templates: []*Template{apiTmpl, dbTmpl},
		Chunks:    []*Chunk{chunkFor(apiStream, apiTmpl), chunkFor(dbStream, dbTmpl)},
		Streams:   []*Stream{apiStream, dbStream},
		ProxyMap:  map[string]string{"api": "arn:api", "db": "arn:db"},
		Resolver:  newResolver(map[string]string{"api": "arn:api", "db": "arn:db"}),
	})
	if err != nil {
		t.Fatalf("a resolved pair with no oracle must not be an error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(results))
	}
	if results[0].StructurallyConfirmed {
		t.Errorf("a nil oracle confirms nothing even when both resources resolve, got %+v", results[0])
	}
}

func TestFindCorrelations_NoErrorTemplates(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	apiStream, dbStream := streamFor("api"), streamFor("db")
	infoA := templateAt("a", SeverityInfo, base, time.Minute)
	infoB := templateAt("b", SeverityInfo, base, time.Minute)

	results, err := FindCorrelations(Input{
		Templates: []*Template{infoA, infoB},
		Chunks:    []*Chunk{chunkFor(apiStream, infoA), chunkFor(dbStream, infoB)},
		Streams:   []*Stream{apiStream, dbStream},
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("INFO templates must not correlate, got %d", len(results))
	}
}

func TestFindCorrelations_SameServicePairsSkipped(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	apiStream := streamFor("api")
	tplA := templateAt("a", SeverityError, base, time.Minute)
	tplB := templateAt("b", SeverityError, base, time.Minute)

	results, err := FindCorrelations(Input{
		Templates: []*Template{tplA, tplB},
		Chunks:    []*Chunk{chunkFor(apiStream, tplA), chunkFor(apiStream, tplB)},
		Streams:   []*Stream{apiStream},
		ProxyMap:  map[string]string{"api": "arn:api"},
		Resolver:  newResolver(map[string]string{"api": "arn:api"}),
		Oracle:    newOracle(),
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("same-service pairs must not correlate, got %d", len(results))
	}
}

func TestFindCorrelations_NoTemporalOverlap(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	apiStream, dbStream := streamFor("api"), streamFor("db")
	apiTmpl := templateAt("a", SeverityError, base, time.Second)
	dbTmpl := templateAt("b", SeverityError, base.Add(time.Hour), time.Second)

	results, err := FindCorrelations(Input{
		Templates: []*Template{apiTmpl, dbTmpl},
		Chunks:    []*Chunk{chunkFor(apiStream, apiTmpl), chunkFor(dbStream, dbTmpl)},
		Streams:   []*Stream{apiStream, dbStream},
		ProxyMap:  map[string]string{"api": "arn:api", "db": "arn:db"},
		Resolver:  newResolver(map[string]string{"api": "arn:api", "db": "arn:db"}),
		Oracle:    newOracle([2]string{"arn:api", "arn:db"}),
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("disjoint time ranges must not correlate, got %d", len(results))
	}
}

func TestServiceFromStream_Precedence(t *testing.T) {
	// service wins over namespace.
	both := &Stream{Labels: map[string]string{FieldService: "svc-wins", FieldNamespace: "ns-lose"}}
	if got := serviceFromStream(both); got != "svc-wins" {
		t.Errorf("service should take precedence, got %q", got)
	}
	// namespace when service is absent, then deployment, then app.
	for _, tc := range []struct {
		labels map[string]string
		want   string
	}{
		{map[string]string{FieldNamespace: "prod"}, "prod"},
		{map[string]string{FieldDeployment: "web"}, "web"},
		{map[string]string{FieldApp: "cart"}, "cart"},
		{map[string]string{FieldNamespace: "prod", FieldDeployment: "web"}, "prod"},
		{map[string]string{FieldDeployment: "web", FieldApp: "cart"}, "web"},
		{map[string]string{"irrelevant": "x"}, ""},
	} {
		if got := serviceFromStream(&Stream{Labels: tc.labels}); got != tc.want {
			t.Errorf("serviceFromStream(%v) = %q, want %q", tc.labels, got, tc.want)
		}
	}
}
