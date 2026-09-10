// SPDX-License-Identifier: Apache-2.0

package correlation

import (
	"testing"
	"time"
)

// callshapes_test.go — THE TWO CALL SHAPES, compiled and driven here.
//
// WHAT THIS ARM CLAIMS AND WHAT IT DOES NOT. It claims that the exported input
// contract can be BUILT from each importer's own data shape, and it reds with a
// compile error in this module the day a change to the contract makes one of
// them unbuildable. It does NOT claim that either importer is wired correctly:
// no test in this module can, because this module compiles only itself, and a
// test here that reflected over this package's own signature would be an
// identity check with no external expectation — green through exactly the event
// it claims to catch. The importers' fit is proven by THEIR builds and THEIR
// suites, on their own landings.
//
// THE TWO SHAPES ARE THE REAL ONES, read at 169fc33a8 (stackdriver
// pipeline.go:76 with resolve.go's cloudContext) and at k8s-logs f896dbf6c
// (internal/logpipe/pipeline.go:104-132 with correlation.go's Dependencies and
// TemplateResource).

// --- THE STACKDRIVER SHAPE ------------------------------------------------
//
// One interface answers both questions, and its resolver arm takes a label KEY
// as well as a value. The adapter is the pair of methods this module asks for,
// over that one interface.

type stackdriverCloudContext interface {
	ResolveService(stream *Stream, key, value string) (ResolvedResource, bool)
	HasDependency(a, b ResolvedResource) bool
}

// stackdriverAdapter is the shape stackdriver's call site builds: it supplies
// the label key the collector resolves on and forwards the rest.
type stackdriverAdapter struct {
	cloud stackdriverCloudContext
	key   string
}

func (a stackdriverAdapter) ResolveService(stream *Stream, service string) (ResolvedResource, bool) {
	return a.cloud.ResolveService(stream, a.key, service)
}

func (a stackdriverAdapter) HasDependency(x, y ResolvedResource) bool {
	return a.cloud.HasDependency(x, y)
}

// fakeStackdriverCloud stands in for stackdriver's foreignCloudContext: it
// resolves by name and refuses a cross-account pair, which is ITS rule and not
// this module's.
type fakeStackdriverCloud struct {
	byName map[string]ResolvedResource
	edges  map[[2]string]struct{}
}

func (c fakeStackdriverCloud) ResolveService(_ *Stream, _, value string) (ResolvedResource, bool) {
	r, ok := c.byName[value]
	return r, ok
}

func (c fakeStackdriverCloud) HasDependency(a, b ResolvedResource) bool {
	if a.Account != b.Account {
		return false
	}
	_, ok := c.edges[[2]string{a.ID, b.ID}]
	return ok
}

func TestTheStackdriverCallShapeBuildsTheInput(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	api, db := streamFor("api"), streamFor("db")
	apiTmpl := templateAt("tpl-api", SeverityError, base, 5*time.Minute)
	dbTmpl := templateAt("tpl-db", SeverityError, base, 5*time.Minute)

	cloud := fakeStackdriverCloud{
		byName: map[string]ResolvedResource{
			"api": {Account: "acct", ID: "res-api"},
			"db":  {Account: "acct", ID: "res-db"},
		},
		edges: map[[2]string]struct{}{
			{"res-api", "res-db"}: {},
			{"res-db", "res-api"}: {},
		},
	}
	adapter := stackdriverAdapter{cloud: cloud, key: FieldService}

	// The proxy diagnostic map, built the way resolve.go:261-270 builds it:
	// label value → "account:resource".
	proxy := map[string]string{"api": "acct:res-api", "db": "acct:res-db"}

	results, err := FindCorrelations(Input{
		Templates: []*Template{apiTmpl, dbTmpl},
		Chunks:    []*Chunk{chunkFor(api, apiTmpl), chunkFor(db, dbTmpl)},
		Streams:   []*Stream{api, db},
		ProxyMap:  proxy,
		Resolver:  adapter,
		Oracle:    adapter,
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}
	if len(results) != 1 || !results[0].StructurallyConfirmed {
		t.Fatalf("expected one confirmed pair, got %+v", results)
	}
	edges := MaterializeCorrelations(results)
	if len(edges) != 1 {
		t.Fatalf("expected one edge, got %d", len(edges))
	}
	if want := "services=api,db resources=acct:res-api,acct:res-db score=1.000"; edges[0].Evidence != want {
		t.Errorf("Evidence = %q, want %q", edges[0].Evidence, want)
	}
}

// --- THE K8S-LOGS SHAPE ---------------------------------------------------
//
// A fused service→resource map and an UNDIRECTED set of resource-id pairs, with
// no resolver interface of its own. The adapter is what turns those two values
// into this module's two interfaces — including the correction the switch
// carries: the account travels to the oracle instead of being discarded.

// k8sTemplateResource is k8s-logs's own fused row (correlation.go's
// TemplateResource) as its call site holds it.
type k8sTemplateResource struct {
	Account    string
	ResourceID string
}

type k8sAdapter struct {
	byService map[string]k8sTemplateResource
	pairs     map[[2]string]struct{}
}

func (a k8sAdapter) ResolveService(_ *Stream, service string) (ResolvedResource, bool) {
	r, ok := a.byService[service]
	if !ok {
		return ResolvedResource{}, false
	}
	return ResolvedResource{Account: r.Account, ID: r.ResourceID}, true
}

// HasDependency reads the ids, and the ACCOUNTS ARRIVE HERE rather than being
// dropped at the call site — the correction axis (d) names.
func (a k8sAdapter) HasDependency(x, y ResolvedResource) bool {
	_, ok := a.pairs[[2]string{x.ID, y.ID}]
	return ok
}

func TestTheK8sLogsCallShapeBuildsTheInput(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	api, db := streamFor("api"), streamFor("db")
	apiTmpl := templateAt("tpl-api", SeverityError, base, 5*time.Minute)
	dbTmpl := templateAt("tpl-db", SeverityError, base, 5*time.Minute)

	adapter := k8sAdapter{
		byService: map[string]k8sTemplateResource{
			"api": {Account: "acct", ResourceID: "r-api"},
			"db":  {Account: "acct", ResourceID: "r-db"},
		},
		pairs: map[[2]string]struct{}{
			{"r-api", "r-db"}: {},
			{"r-db", "r-api"}: {},
		},
	}
	// k8s-logs builds no proxy map today; its resource labels are the same
	// account:resource join, which the switch supplies from the resolution it
	// already has.
	proxy := map[string]string{"api": "acct:r-api", "db": "acct:r-db"}

	results, err := FindCorrelations(Input{
		Templates: []*Template{apiTmpl, dbTmpl},
		Chunks:    []*Chunk{chunkFor(api, apiTmpl), chunkFor(db, dbTmpl)},
		Streams:   []*Stream{api, db},
		ProxyMap:  proxy,
		Resolver:  adapter,
		Oracle:    adapter,
	})
	if err != nil {
		t.Fatalf("FindCorrelations: %v", err)
	}
	if len(results) != 1 || !results[0].StructurallyConfirmed {
		t.Fatalf("expected one confirmed pair, got %+v", results)
	}
	// THE EMITTED EDGE IS THE ONE k8s-logs's emitted_values_test.go pins by
	// substring: the same method and the same resources= rendering.
	edges := MaterializeCorrelations(results)
	if len(edges) != 1 {
		t.Fatalf("expected one edge, got %d", len(edges))
	}
	if edges[0].Method != CorrelationMethod {
		t.Errorf("Method = %q, want %q", edges[0].Method, CorrelationMethod)
	}
	if want := "services=api,db resources=acct:r-api,acct:r-db score=1.000"; edges[0].Evidence != want {
		t.Errorf("Evidence = %q, want %q", edges[0].Evidence, want)
	}
}
