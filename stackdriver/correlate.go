// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/fulminate-io/knowledge-contrib/common/correlation"
)

// correlate.go — THE CALL INTO THE COMMON CORRELATION MODULE, and the adapter
// that carries this collector's own types across it.
//
// THE DETECTOR ITSELF IS NOT HERE ANY MORE, and that is the point. Every logs
// collector needs the same cross-service correlation, and no collector may
// import a sibling, so the detector lives in cmd/collectors/common/correlation
// and this module requires it the way it requires the framework. What stays here
// is the only part that is genuinely this collector's: which of ITS objects
// answer the detector's questions, and how ITS cloud context answers the two
// questions no collector process can answer for itself.
//
// THE TWO HALVES STILL HAVE DIFFERENT EPISTEMIC STATUS. The temporal half is a
// fact about this collect, computed from the entries alone. The CONFIRMATION
// half asks whether the two owning resources depend on each other, which only
// the operator's cloud graph knows and which arrives as declared context. Only a
// confirmed pair becomes an edge, so the graph never carries a coincidence under
// the same edge type as a dependency.

// findCorrelations runs the common detector over this collect's objects.
//
// A NIL CLOUD CONTEXT IS PASSED AS A NIL INTERFACE, NOT AS A TYPED NIL POINTER
// INSIDE ONE. That distinction is the whole of the nil-safe contract on the
// other side: an interface holding a (*cloudAdapter)(nil) is not nil, so the
// detector would call through it and panic instead of leaving every candidate
// unconfirmed.
func findCorrelations(
	templates []*logTemplate,
	chunks []*logChunk,
	streams []*logStream,
	proxyMap map[string]string,
	cloud cloudContext,
) ([]correlation.Result, error) {
	commonStreams, byID := correlationStreams(streams)
	in := correlation.Input{
		Templates: correlationTemplates(templates),
		Chunks:    correlationChunks(chunks),
		Streams:   commonStreams,
		ProxyMap:  proxyMap,
	}
	if cloud != nil {
		adapter := &cloudAdapter{cloud: cloud, byID: byID}
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
func correlationTemplates(templates []*logTemplate) []*correlation.Template {
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
func correlationChunks(chunks []*logChunk) []*correlation.Chunk {
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

// correlationStreams projects the streams and returns the index back to the
// originals, which is what lets the adapter hand this module's own *logStream to
// this module's own resolver.
func correlationStreams(streams []*logStream) ([]*correlation.Stream, map[string]*logStream) {
	out := make([]*correlation.Stream, 0, len(streams))
	byID := make(map[string]*logStream, len(streams))
	for _, s := range streams {
		if s == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &correlation.Stream{ID: s.ID, Labels: s.Labels})
		byID[s.ID] = s
	}
	return out, byID
}

// cloudAdapter answers the detector's two questions from this module's cloud
// context.
//
// THE ACCOUNT RULE IS THIS MODULE'S, NOT THE DETECTOR'S. foreignCloudContext
// refuses a cross-account pair because the declared edges are within-graph by
// the block's own shape (resolve.go). The detector hands both resolved resources
// across whole, accounts included, so that refusal stays where its reason is.
type cloudAdapter struct {
	cloud cloudContext
	byID  map[string]*logStream
}

// ResolveService maps a service label value to a cloud resource, handing the
// resolver the ORIGINAL stream so the surrounding context labels are still
// available to it.
func (a *cloudAdapter) ResolveService(stream *correlation.Stream, service string) (correlation.ResolvedResource, bool) {
	var original *logStream
	if stream != nil {
		original = a.byID[stream.ID]
	}
	res, ok := a.cloud.ResolveService(original, fieldService, service)
	if !ok {
		return correlation.ResolvedResource{}, false
	}
	return correlation.ResolvedResource{Account: res.Account, ID: res.ID}, true
}

// HasDependency asks this module's cloud context whether the two resources are
// connected.
func (a *cloudAdapter) HasDependency(x, y correlation.ResolvedResource) bool {
	return a.cloud.HasDependency(
		resolvedResource{Account: x.Account, ID: x.ID},
		resolvedResource{Account: y.Account, ID: y.ID},
	)
}
