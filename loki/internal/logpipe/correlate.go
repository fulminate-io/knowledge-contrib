// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"github.com/fulminate-io/knowledge-contrib/common/correlation"
)

// correlate.go — THE CALL INTO THE COMMON CORRELATION MODULE, and the adapter
// that carries this module's own types across it.
//
// THE DETECTOR IS NOT HERE AND NEITHER IS THE EDGE. Every logs collector needs
// the same cross-service correlation and the same emitted edge, and no
// collector may import a sibling, so both live in
// cmd/collectors/common/correlation and this module requires it the way it
// requires the framework. What stays here is the only part that is genuinely
// this collector's: which of ITS objects answer the detector's questions, and
// how ITS declared cloud context answers the two a collector process cannot.
//
// THE EVIDENCE STRING AND THE CONFIRMATION RULE ARE GONE FROM THIS MODULE. They
// were a local copy of a contract three collectors held independently, which is
// the shape that drifts one spelling at a time; the module that produces the
// numbers now renders them.

// FindCorrelations runs the common detector over this walk's objects.
//
// A NIL CLOUD CONTEXT IS PASSED AS A NIL INTERFACE, NOT AS A TYPED NIL POINTER
// INSIDE ONE, and that distinction is the whole of the nil-safe contract on the
// other side: an interface holding a (*cloudAdapter)(nil) is not nil, so the
// detector would call through it and panic instead of leaving every candidate
// unconfirmed.
func FindCorrelations(g *Graph, resolutions []Resolution, cloud CloudContext) ([]correlation.Result, error) {
	if g == nil {
		return nil, nil
	}
	in := correlation.Input{
		Templates: correlationTemplates(g.Templates),
		Chunks:    correlationChunks(g.Chunks),
		Streams:   correlationStreams(g.Streams),
		ProxyMap:  ProxyDiagnosticMap(resolutions),
	}
	if cloud != nil {
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
// this collector is supposed to be loud.
func correlationTemplates(templates []*Template) []*correlation.Template {
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
func correlationChunks(chunks []*Chunk) []*correlation.Chunk {
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

// correlationStreams projects the streams, whose labels name the owning service.
func correlationStreams(streams []*Stream) []*correlation.Stream {
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

// ProxyDiagnosticMap indexes a resolution set by label value, in the
// "account:resource" form the correlation evidence string carries.
//
// IT IS BUILT FROM THE RESOLUTIONS RATHER THAN FROM THE CONTEXT, so the
// resources named in an edge's evidence are exactly the ones this collect
// emitted proxies for. A pair whose service label is high cardinality resolves
// through the detector and is absent from this map, which leaves its resources
// side empty — that is the parity behaviour and not a defect to paper over.
func ProxyDiagnosticMap(resolutions []Resolution) map[string]string {
	if len(resolutions) == 0 {
		return nil
	}
	out := make(map[string]string, len(resolutions))
	for _, r := range resolutions {
		out[r.LabelValue] = r.Account + ":" + r.ResourceID
	}
	return out
}

// cloudAdapter answers the detector's two questions from this module's cloud
// context.
//
// THE ACCOUNT RULE IS THIS MODULE'S, NOT THE DETECTOR'S. The context refuses a
// cross-account pair because the declared edges are within-graph by the block's
// own shape. The detector hands both resolved resources across whole, accounts
// included, so that refusal stays where its reason is.
type cloudAdapter struct{ cloud CloudContext }

// ResolveService maps a service label value to a cloud resource. This module's
// context resolves on the VALUE alone — its matchable-name index is built once
// over the whole declared slice — so the stream the detector supplies for
// surrounding context is not needed here and is deliberately ignored.
func (a *cloudAdapter) ResolveService(_ *correlation.Stream, service string) (correlation.ResolvedResource, bool) {
	res, ok := a.cloud.ResolveService(service)
	if !ok {
		return correlation.ResolvedResource{}, false
	}
	return correlation.ResolvedResource{Account: res.Account, ID: res.ID}, true
}

// HasDependency asks this module's context whether the two resources are
// connected.
func (a *cloudAdapter) HasDependency(x, y correlation.ResolvedResource) bool {
	return a.cloud.HasDependency(
		Resource{Account: x.Account, ID: x.ID},
		Resource{Account: y.Account, ID: y.ID},
	)
}
