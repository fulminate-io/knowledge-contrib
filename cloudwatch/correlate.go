// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/fulminate-io/knowledge-contrib/common/correlation"
)

// correlate.go — THE CALL INTO THE COMMON CORRELATION MODULE, and the adapter
// that carries this collector's own types across it.
//
// THE DETECTOR IS NOT HERE, AND IT IS NOT MISSING EITHER. An earlier version of
// this module emitted no CORRELATES_WITH edge on any production collect and said
// the inputs were unavailable. That was wrong and a code review corrected it:
// the temporal half is computed entirely from this collect's own templates,
// chunks and streams, all three of which this module builds, and the
// confirmation half needs only a dependency answer between two cloud resource
// ids, which the declared context's edges carry. What was actually missing was a
// shared implementation, since every logs collector needs the same detector and
// no collector may import a sibling. It now lives in
// cmd/collectors/common/correlation, required the way the framework is.
//
// WHAT STAYS HERE is the only part that is genuinely this collector's: which of
// ITS objects answer the detector's questions, and how ITS declared cloud
// context answers the two questions no collector process can answer alone.
//
// THE TWO HALVES HAVE DIFFERENT EPISTEMIC STATUS AND THE EDGE TYPE DEPENDS ON
// IT. The temporal half is a fact about this collect. The confirmation half is
// the operator's cloud graph speaking. Only a confirmed pair becomes an edge, so
// the graph never carries a coincidence under the same edge type as a
// dependency.

// findCorrelations runs the common detector over this collect's objects.
//
// A NIL CLOUD CONTEXT IS PASSED AS A NIL INTERFACE, NOT AS A TYPED NIL POINTER
// INSIDE ONE, which is the whole of the nil-safe contract on the other side: an
// interface holding a typed nil is not nil, so the detector would call through
// it and panic instead of leaving every candidate unconfirmed.
func findCorrelations(
	templates []*LogTemplate,
	chunks []*LogChunk,
	streams []*LogStream,
	proxyMap map[string]string,
	cloud CloudContext,
) ([]correlation.Result, error) {
	in := correlation.Input{
		Templates: correlationTemplates(templates),
		Chunks:    correlationChunks(chunks),
		Streams:   correlationStreams(streams),
		ProxyMap:  proxyMap,
	}
	if !cloud.IsEmpty() {
		adapter := &cloudAdapter{cloud: cloud}
		in.Resolver = adapter
		in.Oracle = adapter
	}
	return correlation.FindCorrelations(in)
}

// correlationTemplates projects this module's templates onto the four fields the
// detector reads. Nothing else about a template is its business.
//
// A NIL ENTRY IS PROJECTED AS A NIL ENTRY, deliberately, in all three
// projections below. It is malformed input and the DETECTOR refuses it by name;
// dropping it here would swallow that refusal and hand the detector a shorter
// slice than the pipeline produced, which is a silent degrade in the one place
// this collector is meant to be loud.
func correlationTemplates(templates []*LogTemplate) []*correlation.Template {
	out := make([]*correlation.Template, 0, len(templates))
	for _, t := range templates {
		if t == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &correlation.Template{
			ID:        t.ID,
			Severity:  t.Severity,
			FirstSeen: t.FirstSeen,
			LastSeen:  t.LastSeen,
		})
	}
	return out
}

// correlationChunks projects the template-to-stream association, which is all a
// chunk contributes to correlation.
func correlationChunks(chunks []*LogChunk) []*correlation.Chunk {
	out := make([]*correlation.Chunk, 0, len(chunks))
	for _, c := range chunks {
		if c == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &correlation.Chunk{StreamID: c.StreamID, TemplateID: c.TemplateID})
	}
	return out
}

// correlationStreams projects the streams onto their identity and labels.
//
// NO INDEX BACK TO THE ORIGINALS IS KEPT, and that is a property of this
// provider rather than a simplification. A CloudWatch walk sets exactly three
// labels — the log group, the service derived from it and the log stream — so
// there are no surrounding context labels for a resolver to disambiguate a graph
// by, and the projected Labels map carries everything this module's resolver
// reads. A provider whose streams carry a project or a cluster label needs that
// index; this one does not.
func correlationStreams(streams []*LogStream) []*correlation.Stream {
	out := make([]*correlation.Stream, 0, len(streams))
	for _, s := range streams {
		if s == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &correlation.Stream{ID: s.ID, Labels: s.Labels})
	}
	return out
}

// proxyMapFrom builds the service-value to resource diagnostic map the detector
// carries into a result's ResourceA and ResourceB, and from there into the
// emitted edge's evidence string.
//
// IT IS KEYED ON THE LABEL VALUE, not the resource, and it is built from THIS
// collect's own resolutions, so the evidence names the same resource the
// EMITTED_BY edge points at.
func proxyMapFrom(resolutions []ResolvedProxy) map[string]string {
	out := make(map[string]string, len(resolutions))
	for _, r := range resolutions {
		out[r.LabelValue] = r.Account + ":" + r.ResourceID
	}
	return out
}

// cloudAdapter answers the detector's two questions from this module's declared
// cloud context.
type cloudAdapter struct {
	cloud CloudContext
}

// ResolveService maps a service label value to a declared cloud resource
// THROUGH THE SAME RULE THE PROXY PATH USES.
//
// That shared rule is what keeps the two halves consistent: the resource named
// in a correlation's evidence is the resource the EMITTED_BY edge for that same
// label points at. Resolving them by two rules would let an edge cite a resource
// the graph never linked the label to.
//
// The stream is ignored, for the reason correlationStreams gives: a CloudWatch
// stream carries no context labels to disambiguate by.
func (a *cloudAdapter) ResolveService(_ *correlation.Stream, service string) (correlation.ResolvedResource, bool) {
	resolved, ok := resolveLabel(correlation.FieldService, service, a.cloud.Resources)
	if !ok {
		return correlation.ResolvedResource{}, false
	}
	return correlation.ResolvedResource{Account: resolved.Account, ID: resolved.ResourceID}, true
}

// HasDependency asks the declared context whether the two resources are
// connected.
func (a *cloudAdapter) HasDependency(x, y correlation.ResolvedResource) bool {
	return a.cloud.hasDependency(
		cloudResourceRef{Account: x.Account, ID: x.ID},
		cloudResourceRef{Account: y.Account, ID: y.ID},
	)
}
