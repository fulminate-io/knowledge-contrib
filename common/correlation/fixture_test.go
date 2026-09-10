// SPDX-License-Identifier: Apache-2.0

package correlation

import "time"

// fixture_test.go — the shared fixture vocabulary for this module's suite.
//
// THE HELPERS ARE THE PARITY TARGET'S OWN, transcribed rather than invented:
// templateAt, streamFor and chunkFor are the three helpers
// cmd/knowledge/internal/collector/logs/pipeline_correlation_test.go builds every
// correlation fixture from at 169fc33a8, and keeping their shapes is what makes
// this suite's arms comparable to the ones they were derived from.
//
// THE ORACLE AND THE RESOLVER ARE RECORDING, and that is not a convenience:
// three of this suite's arms assert what the detector HANDED the injected
// interface (both accounts unmodified; one resolver call per service; the pair a
// nil-resolver run never asks about), and an outcome-only stub cannot observe
// any of them.

// testWindow is the window the fixtures are written against. It is the module's
// own default, named here so an arm that depends on the sixty seconds says so.
const testWindow = defaultCorrelationWindow

// templateAt is a synthetic template whose range covers [start, start+dur].
func templateAt(id, severity string, start time.Time, dur time.Duration) *Template {
	return &Template{
		ID:        id,
		Severity:  severity,
		FirstSeen: start,
		LastSeen:  start.Add(dur),
	}
}

// streamFor is a stream whose service label names svc.
func streamFor(svc string) *Stream {
	return &Stream{
		ID:     "stream-" + svc,
		Labels: map[string]string{FieldService: svc},
	}
}

// chunkFor wires one template to one stream, which is the only linkage the
// detector reads a chunk for.
func chunkFor(stream *Stream, tmpl *Template) *Chunk {
	return &Chunk{StreamID: stream.ID, TemplateID: tmpl.ID}
}

// recordingOracle confirms a pre-seeded set of ID pairs and REMEMBERS every pair
// it was handed, whole, so an arm can assert what reached it rather than only
// what came back.
type recordingOracle struct {
	known map[string]struct{}
	seen  [][2]ResolvedResource
}

// newOracle seeds a recording oracle with the resource-ID pairs it confirms, in
// both orders — connection is symmetric.
func newOracle(pairs ...[2]string) *recordingOracle {
	m := make(map[string]struct{}, len(pairs)*2)
	for _, p := range pairs {
		m[p[0]+"|"+p[1]] = struct{}{}
		m[p[1]+"|"+p[0]] = struct{}{}
	}
	return &recordingOracle{known: m}
}

func (o *recordingOracle) HasDependency(a, b ResolvedResource) bool {
	o.seen = append(o.seen, [2]ResolvedResource{a, b})
	_, ok := o.known[a.ID+"|"+b.ID]
	return ok
}

// calls is how many pairs the oracle was asked about.
func (o *recordingOracle) calls() int { return len(o.seen) }

// recordingResolver resolves a service name through a map and remembers the
// order it was asked in, which is what the per-service cache arm observes.
type recordingResolver struct {
	account  string
	services map[string]string
	asked    []string
}

// newResolver builds a resolver over a service→resource-ID map, with every hit
// under the one account "acct" every fixture here uses.
func newResolver(services map[string]string) *recordingResolver {
	return &recordingResolver{account: "acct", services: services}
}

func (r *recordingResolver) ResolveService(_ *Stream, service string) (ResolvedResource, bool) {
	r.asked = append(r.asked, service)
	id, ok := r.services[service]
	if !ok {
		return ResolvedResource{}, false
	}
	return ResolvedResource{Account: r.account, ID: id}, true
}

// perAccountResolver resolves each service to its own (account, id) pair, which
// is what the cross-account arm needs and the single-account resolver cannot
// express.
type perAccountResolver struct {
	byService map[string]ResolvedResource
}

func (r perAccountResolver) ResolveService(_ *Stream, service string) (ResolvedResource, bool) {
	res, ok := r.byService[service]
	return res, ok
}
