// SPDX-License-Identifier: Apache-2.0

package correlation

import (
	"fmt"
	"testing"
	"time"
)

// axes_test.go — ONE ARM PER DIVERGENCE AXIS, because these four are the places
// two independent rebuilds of this detector already drifted apart, and they are
// where a future author will drift again. Each arm names its axis and cites the
// parity target's line, so a change that "simplifies" one of them reds against
// the behaviour it is departing from rather than against a preference.
//
// The parity floor is cmd/knowledge/internal/collector/logs at 169fc33a8.

// TestAxisB_AnUnresolvedTemplateStillYieldsAnUnconfirmedCandidate.
//
// AXIS (b), CANDIDATE ADMISSION. pipeline_correlation.go:267-271 returns the
// pair UNCONFIRMED when either template's resource is absent; admission at
// :213-241 keys on service resolution only, never on resource resolution. The
// k8s-logs rebuild drops the pair entirely, which is the divergence this arm
// exists to keep out of the common detector: dropping it loses the candidate a
// summary would report, and it makes "no dependency declared" indistinguishable
// from "no resource resolved".
func TestAxisB_AnUnresolvedTemplateStillYieldsAnUnconfirmedCandidate(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	api, db := streamFor("api"), streamFor("db")
	apiTmpl := templateAt("tpl-api", SeverityError, base, 5*time.Minute)
	dbTmpl := templateAt("tpl-db", SeverityError, base.Add(time.Minute), 5*time.Minute)

	// Only api resolves. db is a service the declared block never named.
	oracle := newOracle([2]string{"arn:api", "arn:db"})
	results, err := FindCorrelations(Input{
		Templates: []*Template{apiTmpl, dbTmpl},
		Chunks:    []*Chunk{chunkFor(api, apiTmpl), chunkFor(db, dbTmpl)},
		Streams:   []*Stream{api, db},
		ProxyMap:  map[string]string{"api": "acct:arn:api"},
		Resolver:  newResolver(map[string]string{"api": "arn:api"}),
		Oracle:    oracle,
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("the pair must survive as a candidate, got %d results: %+v", len(results), results)
	}
	if results[0].StructurallyConfirmed {
		t.Errorf("an unresolved template cannot be confirmed: %+v", results[0])
	}
	// AND THE ORACLE IS NEVER ASKED, because there is no second resource to ask
	// about. Without this the arm would pass on a detector that asked about a
	// zero-valued resource.
	if oracle.calls() != 0 {
		t.Errorf("the oracle was asked about an unresolved pair: %+v", oracle.seen)
	}
}

// TestAxisC_AHighCardinalityServiceLabelYieldsAnEmptyResourceSide.
//
// AXIS (c), THE SOURCE OF THE EVIDENCE RESOURCE LABELS. The built-in fills
// ResourceA/ResourceB from the proxy map (pipeline_correlation.go:260-261),
// which is built from the LOW-CARDINALITY labels (pipeline.go:222-225 over
// pipeline_proxy.go:70-93), while the service name itself comes from the FULL
// label set (:182-196). So a service whose label was classified high-cardinality
// resolves, confirms, and renders an EMPTY resources side. That is the parity
// behaviour. The k8s-logs rebuild always renders the confirming resource, which
// cannot produce this and is the divergence being corrected.
func TestAxisC_AHighCardinalityServiceLabelYieldsAnEmptyResourceSide(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	api, db := streamFor("api"), streamFor("db")
	// IDENTICAL RANGES, so the co-occurrence score is exactly 1 and the evidence
	// literal below is arithmetic a reader can check rather than a value copied
	// out of a run.
	apiTmpl := templateAt("tpl-api", SeverityError, base, 5*time.Minute)
	dbTmpl := templateAt("tpl-db", SeverityError, base, 5*time.Minute)

	// The proxy map names api only: db's service label was high-cardinality, so
	// no proxy entry was built for it — but it still RESOLVES and still confirms.
	results, err := FindCorrelations(Input{
		Templates: []*Template{apiTmpl, dbTmpl},
		Chunks:    []*Chunk{chunkFor(api, apiTmpl), chunkFor(db, dbTmpl)},
		Streams:   []*Stream{api, db},
		ProxyMap:  map[string]string{"api": "acct:arn:api"},
		Resolver:  newResolver(map[string]string{"api": "arn:api", "db": "arn:db"}),
		Oracle:    newOracle([2]string{"arn:api", "arn:db"}),
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 correlation, got %d", len(results))
	}
	r := results[0]
	if !r.StructurallyConfirmed {
		t.Fatalf("the pair resolves and is declared, so it confirms: %+v", r)
	}
	if r.ResourceA != "acct:arn:api" || r.ResourceB != "" {
		t.Errorf("resources must come from the proxy map alone: %+v", r)
	}
	// AND THE EMITTED EVIDENCE CARRIES THE EMPTY SIDE, which is what a consumer
	// reads. The expectation is a literal, not a value read back.
	edges := MaterializeCorrelations(results)
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if want := "services=api,db resources=acct:arn:api, score=1.000"; edges[0].Evidence != want {
		t.Errorf("Evidence = %q, want %q", edges[0].Evidence, want)
	}
}

// TestAxisD_BothAccountsReachTheOracleUnmodified.
//
// AXIS (d), CROSS-ACCOUNT PAIRS. The detector applies NO account rule of its
// own: pipeline_correlation.go:272 delegates whole to the oracle with both
// ResolvedResource values carrying their Account, and the built-in oracle this
// module was measured against (cloudresolver/dep_checker.go:73-87, read at
// a9171723f400f6f4d58fbe6ca27626fb4874c076, the last commit before the
// cloudresolver package was deleted) was deliberately cross-account through
// transparent proxy traversals. A collector whose own oracle refuses a
// cross-account pair — stackdriver's does, at resolve.go:146-149 — keeps that
// refusal in ITS oracle, which is why nothing about accounts is decided here.
//
// THE ASSERTION IS ON WHAT THE ORACLE WAS HANDED, not on the outcome: an
// outcome-only assertion passes just as well against a detector that dropped the
// Account before asking, which is exactly the k8s-logs divergence.
func TestAxisD_BothAccountsReachTheOracleUnmodified(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	api, db := streamFor("api"), streamFor("db")
	apiTmpl := templateAt("tpl-api", SeverityError, base, 5*time.Minute)
	dbTmpl := templateAt("tpl-db", SeverityError, base.Add(time.Minute), 5*time.Minute)

	oracle := newOracle([2]string{"arn:api", "arn:db"})
	results, err := FindCorrelations(Input{
		Templates: []*Template{apiTmpl, dbTmpl},
		Chunks:    []*Chunk{chunkFor(api, apiTmpl), chunkFor(db, dbTmpl)},
		Streams:   []*Stream{api, db},
		ProxyMap:  map[string]string{"api": "prod:arn:api", "db": "staging:arn:db"},
		Resolver: perAccountResolver{byService: map[string]ResolvedResource{
			"api": {Account: "prod", ID: "arn:api"},
			"db":  {Account: "staging", ID: "arn:db"},
		}},
		Oracle: oracle,
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 correlation, got %d", len(results))
	}
	if len(oracle.seen) != 1 {
		t.Fatalf("expected exactly one oracle question, got %d: %+v", len(oracle.seen), oracle.seen)
	}
	gotA, gotB := oracle.seen[0][0], oracle.seen[0][1]
	wantA := ResolvedResource{Account: "prod", ID: "arn:api"}
	wantB := ResolvedResource{Account: "staging", ID: "arn:db"}
	if gotA != wantA || gotB != wantB {
		t.Errorf("the oracle was handed (%+v, %+v), want (%+v, %+v)", gotA, gotB, wantA, wantB)
	}
	// AND THE DETECTOR APPLIED NO RULE OF ITS OWN: this oracle confirms the pair
	// across two accounts, and the result says so.
	if !results[0].StructurallyConfirmed {
		t.Errorf("the detector refused a cross-account pair its oracle confirmed: %+v", results[0])
	}
}

// TestTheResolverIsAskedOncePerService is the caching half of axis (a)'s input
// contract: two templates in one service make ONE resolution call, and a service
// that misses is not retried per template (pipeline_correlation.go:150-178).
//
// AND THE ORDER, which is the same arm because the same pass produces it. The
// resolution pass visits SORTED template ids rather than walking the map, so the
// resolver is asked in a fixed order and a resolver whose answer depends on what
// it was asked first cannot make one collect differ from the next. SIX SERVICES
// AND FIVE RUNS is what makes that assertion mean something: one run over a map
// walk would land on the sorted order by luck about once in seven hundred, and
// five independent runs make an accidental pass a one-in-1e14 event rather than
// a coin flip.
func TestTheResolverIsAskedOncePerService(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	// The stream/template names are chosen so SORTED TEMPLATE ID order and the
	// order the slices are written in disagree: a pass that visited the slice
	// would ask in the written order, and one that walked the map would ask in
	// neither.
	names := []string{"foxtrot", "alpha", "echo", "charlie", "bravo", "delta"}
	var templates []*Template
	var chunks []*Chunk
	var streams []*Stream
	resolved := map[string]string{}
	for i, svc := range names {
		st := streamFor(svc)
		// tpl-0 belongs to foxtrot, tpl-1 to alpha, and so on, so sorted template
		// id order is the order of `names` and not alphabetical by service.
		tmpl := templateAt(fmt.Sprintf("tpl-%d", i), SeverityError, base, 5*time.Minute)
		second := templateAt(fmt.Sprintf("tpl-%d-b", i), SeverityError, base, 5*time.Minute)
		templates = append(templates, tmpl, second)
		chunks = append(chunks, chunkFor(st, tmpl), chunkFor(st, second))
		streams = append(streams, st)
		if svc != "delta" { // delta misses, and must not be retried per template
			resolved[svc] = "arn:" + svc
		}
	}

	for run := 1; run <= 5; run++ {
		resolver := newResolver(resolved)
		if _, err := FindCorrelations(Input{
			Templates: templates,
			Chunks:    chunks,
			Streams:   streams,
			Resolver:  resolver,
			Oracle:    newOracle(),
		}); err != nil {
			t.Fatalf("run %d: FindCorrelations: %v", run, err)
		}
		// TWELVE templates, SIX services: six calls, one per service, including
		// the one that missed.
		if got := len(resolver.asked); got != len(names) {
			t.Fatalf("run %d: the resolver was asked %d times for %d services: %v",
				run, got, len(names), resolver.asked)
		}
		want := append([]string(nil), names...)
		for i := range want {
			if resolver.asked[i] != want[i] {
				t.Fatalf("run %d: resolution order was %v, want %v — the pass visits sorted template ids",
					run, resolver.asked, want)
			}
		}
	}
}
