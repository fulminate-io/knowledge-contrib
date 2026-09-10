// SPDX-License-Identifier: Apache-2.0

package correlation

import (
	"testing"
	"time"
)

// collectshape_test.go — THE WHOLE-COLLECT FIXTURE: the four cases the ticket
// names, driven through the detector in ONE run so the positives and the
// negatives are observed against the same input rather than against four
// hand-tuned ones.

// TestFindCorrelations_ConfirmedOverAWholeCollectShape is this module's
// counterpart to the parity target's TestPipeline_CorrelationConfirmed_PureTransform
// (pipeline_test.go:133), which drives the built-in's whole pipeline and asserts
// one confirmed pair. There is no pipeline here, so the arm drives the detector
// over the fixture the ticket names — a known positive, a same-service negative,
// an out-of-window negative and an undeclared-dependency negative in ONE run —
// which is the same claim with the collect's shape supplied as values.
func TestFindCorrelations_ConfirmedOverAWholeCollectShape(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	api, db, worker, cron := streamFor("api"), streamFor("db"), streamFor("worker"), streamFor("cron")

	apiErr := templateAt("tpl-api", SeverityError, base, 5*time.Minute)
	dbErr := templateAt("tpl-db", SeverityError, base.Add(2*time.Minute), 5*time.Minute)
	apiErr2 := templateAt("tpl-api-2", SeverityError, base.Add(time.Minute), 3*time.Minute)
	workerErr := templateAt("tpl-worker", SeverityError, base.Add(time.Minute), 4*time.Minute)
	cronErr := templateAt("tpl-cron", SeverityError, base.Add(3*time.Hour), time.Minute)

	results, err := FindCorrelations(Input{
		Templates: []*Template{apiErr, dbErr, apiErr2, workerErr, cronErr},
		Chunks: []*Chunk{
			chunkFor(api, apiErr), chunkFor(db, dbErr), chunkFor(api, apiErr2),
			chunkFor(worker, workerErr), chunkFor(cron, cronErr),
		},
		Streams: []*Stream{api, db, worker, cron},
		ProxyMap: map[string]string{
			"api": "acct:arn:api", "db": "acct:arn:db",
			"worker": "acct:arn:worker", "cron": "acct:arn:cron",
		},
		Resolver: newResolver(map[string]string{
			"api": "arn:api", "db": "arn:db", "worker": "arn:worker", "cron": "arn:cron",
		}),
		Oracle: newOracle([2]string{"arn:api", "arn:db"}),
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}

	confirmed := map[[2]string]bool{}
	for _, r := range results {
		if r.StructurallyConfirmed {
			confirmed[[2]string{r.TemplateA, r.TemplateB}] = true
		}
		// THE SAME-SERVICE NEGATIVE: api's two templates never pair.
		if r.ServiceA == r.ServiceB {
			t.Errorf("a same-service pair was emitted: %+v", r)
		}
		// THE OUT-OF-WINDOW NEGATIVE: cron burns three hours later.
		if r.TemplateA == cronErr.ID || r.TemplateB == cronErr.ID {
			t.Errorf("an out-of-window template was paired: %+v", r)
		}
	}
	// THE CONFIRMED SET IS ENUMERATED, not counted: api declares a dependency on
	// db and api owns TWO error templates, so both of api's templates correlate
	// with db's and nothing else does. The pair order within a row follows the
	// template slice, which is why the second row reads db first.
	want := map[[2]string]bool{
		{apiErr.ID, dbErr.ID}:  true,
		{dbErr.ID, apiErr2.ID}: true,
	}
	for pair := range want {
		if !confirmed[pair] {
			t.Errorf("the known positive %v is not confirmed; got %+v", pair, results)
		}
	}
	// THE UNDECLARED-DEPENDENCY NEGATIVE: api↔worker overlaps and resolves, and
	// the oracle declares nothing about it, so it stays a candidate.
	for _, r := range results {
		if r.StructurallyConfirmed && !want[[2]string{r.TemplateA, r.TemplateB}] {
			t.Errorf("an undeclared dependency was confirmed: %+v", r)
		}
	}
	if len(confirmed) != len(want) {
		t.Errorf("expected %d confirmed pairs, got %d: %+v", len(want), len(confirmed), results)
	}
}
