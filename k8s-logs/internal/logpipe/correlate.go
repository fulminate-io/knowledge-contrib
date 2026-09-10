// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"github.com/fulminate-io/knowledge-contrib/common/correlation"
)

// correlate.go — THE CALL INTO THE COMMON CORRELATION MODULE, and the
// projection that carries this module's own types across it.
//
// THE DETECTOR IS NOT HERE ANY MORE, and that is the point. Every logs
// collector needs the same cross-service correlation and no collector may
// import a sibling, so the detector and the edge it emits live in
// cmd/collectors/common/correlation and this module requires it the way it
// requires the framework. Three independent copies of the evidence string used
// to exist, which is the shape that drifts one spelling at a time.
//
// WHAT STAYS HERE is the only part that is genuinely this module's: which of ITS
// objects answer the detector's questions. Nothing about scoring, admission,
// the ERROR filter or the rendered edge is decided in this file, and the
// resolver and oracle a caller supplies are the DETECTOR'S interfaces, passed
// through rather than wrapped.
//
// A NIL ELEMENT IS PROJECTED AS A NIL ELEMENT in all three projections below.
// It is malformed input and the DETECTOR refuses it by name and index; dropping
// it here would swallow that refusal and hand the detector a shorter slice than
// the pipeline produced, which is a silent degrade in the one place this
// collector is meant to be loud.
//
// THAT REFUSAL IS REACHED ON A DIRECT CALL. Through Build a nil template does
// not arrive here at all: AssembleGraph renders the templates into nodes first
// and dereferences it before the pipeline reaches this projection. So the rule
// is worth keeping for the callers that can reach it, and the claim is about
// the direct path rather than about the walk.

// correlationTemplates projects this module's templates onto the four fields
// the detector reads.
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

// correlationStreams projects the streams onto the two fields the detector
// reads.
//
// IT CARRIES THE WHOLE LABEL MAP, the same map rather than a copy of it, so a
// resolver reached through this projection sees every label the collector
// emitted — the cluster and project labels included. What the projection drops
// is the cardinality split, the fingerprint and the alias, none of which a
// resolver has any use for. An earlier shape here handed a resolver the ORIGINAL
// stream through an index, justified by labels this projection was said to drop;
// it drops none, so the index answered a question nothing asked and it is gone.
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

// findCorrelations runs the common detector over this collect's objects.
//
// THE RESOLVER AND THE ORACLE ARE THE DETECTOR'S OWN INTERFACES, passed
// straight through. This module used to wrap them in an adapter over its own
// duplicate pair; the wrapper existed to hand a resolver the original stream,
// which the projection already carries in full, so it converted one interface
// into an identical one and nothing else.
func findCorrelations(
	templates []*Template,
	chunks []*Chunk,
	streams []*Stream,
	opts Options,
) ([]correlation.Result, error) {
	return correlation.FindCorrelations(correlation.Input{
		Templates: correlationTemplates(templates),
		Chunks:    correlationChunks(chunks),
		Streams:   correlationStreams(streams),
		ProxyMap:  opts.ProxyMap,
		Resolver:  opts.Resolver,
		Oracle:    opts.Oracle,
	})
}
