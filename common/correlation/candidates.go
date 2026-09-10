// SPDX-License-Identifier: Apache-2.0

package correlation

import "sort"

// candidates.go — FROM TEMPLATES TO SCORED PAIRS: the error filter, the
// template-to-service attribution, the per-service resolution and the pairing.
//
// EACH STAGE DROPS SOMETHING, AND WHAT IT DROPS IS A CLAIM. A non-error template
// is not a failure to correlate. A template no stream attributes to a service
// cannot participate, because a correlation is a statement about two services. A
// same-service pair is one incident rather than a correlation between two. A
// pair whose resources did not resolve stays a CANDIDATE — the pair is real and
// the dependency is unknown, which is not the same as absent.

// filterErrorTemplates keeps templates at ERROR or above. Correlating INFO
// templates would pair every heartbeat in the collect with every other one.
//
// IT CARRIES NO NIL CHECK, deliberately: Input.validate has already refused a
// nil element, so a check here would be a branch no input can reach and no test
// can red — which is worse than none, because the next reader would take it as
// evidence that a nil template is a shape this package tolerates.
func filterErrorTemplates(templates []*Template) []*Template {
	out := make([]*Template, 0, len(templates))
	for _, t := range templates {
		if !SeverityAtLeast(t.Severity, SeverityError) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// templateStreamRef bundles a template's owning stream with the service name
// derived from it.
type templateStreamRef struct {
	stream *Stream
	svc    string
}

// mapTemplateStreams finds each error template's owning stream through the
// chunks, keeping the FIRST chunk that names it.
//
// A template with no owning stream, or one whose stream names no service, is
// omitted: correlation is a statement about two services, so a template that
// cannot be attributed to one cannot participate.
func mapTemplateStreams(templates []*Template, chunks []*Chunk, streams []*Stream) map[string]templateStreamRef {
	errorIDs := make(map[string]struct{}, len(templates))
	for _, t := range templates {
		errorIDs[t.ID] = struct{}{}
	}
	streamByID := make(map[string]*Stream, len(streams))
	for _, s := range streams {
		streamByID[s.ID] = s
	}

	refs := make(map[string]templateStreamRef, len(templates))
	for _, c := range chunks {
		if _, keep := errorIDs[c.TemplateID]; !keep {
			continue
		}
		if _, known := refs[c.TemplateID]; known {
			continue
		}
		s := streamByID[c.StreamID]
		if s == nil {
			continue
		}
		svc := serviceFromStream(s)
		if svc == "" {
			continue
		}
		refs[c.TemplateID] = templateStreamRef{stream: s, svc: svc}
	}
	return refs
}

// resolveTemplateResources maps each template to the cloud resource of its
// owning service, caching per service so two templates in one service make one
// resolution call. A service that does not resolve is remembered as a miss, so
// it is not retried once per template.
//
// THE PASS VISITS SORTED TEMPLATE IDS rather than walking the map, so the
// resolver is asked in a fixed order and a resolver whose answer depends on what
// it was asked first cannot make one collect differ from the next.
func resolveTemplateResources(tmplStreams map[string]templateStreamRef, resolver Resolver) map[string]ResolvedResource {
	out := make(map[string]ResolvedResource, len(tmplStreams))
	if resolver == nil {
		return out
	}
	hits := make(map[string]ResolvedResource)
	misses := make(map[string]struct{})
	for _, tmplID := range sortedRefKeys(tmplStreams) {
		ref := tmplStreams[tmplID]
		if hit, ok := hits[ref.svc]; ok {
			out[tmplID] = hit
			continue
		}
		if _, miss := misses[ref.svc]; miss {
			continue
		}
		resolved, ok := resolver.ResolveService(ref.stream, ref.svc)
		if !ok {
			misses[ref.svc] = struct{}{}
			continue
		}
		hits[ref.svc] = resolved
		out[tmplID] = resolved
	}
	return out
}

// serviceFromStream names the service a stream belongs to, preferring the
// explicit service label and falling back through the other identifiers in a
// fixed order.
//
// THE SET IS SMALL AND CLOSED. Every key here decides which service a template's
// failures are attributed to, so widening it would attribute a template to an
// instance or a pod rather than to the service the correlation is about.
func serviceFromStream(s *Stream) string {
	for _, key := range []string{FieldService, FieldNamespace, FieldDeployment, FieldApp} {
		if v := s.Labels[key]; v != "" {
			return v
		}
	}
	return ""
}

// candidatePair is two overlapping error templates from different services.
type candidatePair struct {
	a, b    *Template
	svcA    string
	svcB    string
	overlap float64
}

// buildCandidatePairs enumerates the overlapping cross-service pairs. Self-pairs
// and same-service pairs are skipped: two errors in one service are one
// incident, not a correlation between two.
func buildCandidatePairs(templates []*Template, tmplStreams map[string]templateStreamRef) []candidatePair {
	pairs := make([]candidatePair, 0)
	for i := range templates {
		a := templates[i]
		refA, okA := tmplStreams[a.ID]
		if !okA {
			continue
		}
		for j := i + 1; j < len(templates); j++ {
			b := templates[j]
			refB, okB := tmplStreams[b.ID]
			if !okB || refA.svc == refB.svc {
				continue
			}
			score, overlaps := temporalOverlap(a, b, defaultCorrelationWindow)
			if !overlaps {
				continue
			}
			pairs = append(pairs, candidatePair{a: a, b: b, svcA: refA.svc, svcB: refB.svc, overlap: score})
		}
	}
	return pairs
}

// scoreCandidatePair turns a candidate into a result, asking the oracle whether
// the two resources are connected. Without an oracle, or without both resources
// resolved, the pair stays unconfirmed — which is a statement that the
// dependency is UNKNOWN, not that it is absent.
//
// THE ORACLE IS HANDED BOTH ResolvedResource VALUES WHOLE, accounts included.
// Nothing about accounts is decided here: a collector whose declared edges are
// within-graph keeps that refusal in its own oracle, where the reason for it
// lives.
func scoreCandidatePair(
	p candidatePair,
	proxyMap map[string]string,
	tmplResources map[string]ResolvedResource,
	oracle DependencyOracle,
) Result {
	res := Result{
		TemplateA:         p.a.ID,
		TemplateB:         p.b.ID,
		ServiceA:          p.svcA,
		ServiceB:          p.svcB,
		ResourceA:         proxyMap[p.svcA],
		ResourceB:         proxyMap[p.svcB],
		CooccurrenceScore: p.overlap,
	}
	if oracle == nil {
		return res
	}
	resA, okA := tmplResources[p.a.ID]
	resB, okB := tmplResources[p.b.ID]
	if !okA || !okB {
		return res
	}
	res.StructurallyConfirmed = oracle.HasDependency(resA, resB)
	return res
}

// sortedRefKeys returns a template-reference map's keys in ascending order.
func sortedRefKeys(m map[string]templateStreamRef) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
