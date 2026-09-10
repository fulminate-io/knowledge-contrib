// SPDX-License-Identifier: Apache-2.0

// Package correlation detects CROSS-SERVICE ERROR CORRELATION for logs
// collectors: which pairs of error templates burned at the same time in two
// different services, and which of those pairs the operator's cloud graph says
// are actually connected.
//
// IT IS COMMON CODE BECAUSE THE COLLECTORS MAY NOT DEPEND ON EACH OTHER. Every
// logs collector needs this detector and no collector may import a sibling, so
// it lives in its own module that each of them requires — the same shape
// cmd/collectors/framework already has. Nothing here is provider-specific: the
// detector takes the caller's own data as values and returns pairs.
package correlation

import (
	"fmt"
	"sort"
	"time"
)

// correlation.go — THE ENTRY POINT AND THE PARITY FLOOR.
//
// THE FLOOR IS THE BUILT-IN LOG PIPELINE, cmd/knowledge/internal/collector/logs
// at 169fc33a8f71d8a94bce0c5604159940f022b64f, and not either of the two
// collector rebuilds this module replaces. They had drifted from it on FOUR
// AXES, and this module takes the built-in's behaviour on each. The axes are
// named here because they are where the next author will drift again, and one
// test per axis in axes_test.go holds each:
//
//   - (a) INPUT CONTRACT. The same six inputs the built-in takes
//     (pipeline_correlation.go:43-51): templates, chunks, streams, the proxy
//     diagnostic map, a resolver and a dependency oracle — as values and
//     interfaces, never a collector's type.
//   - (b) CANDIDATE ADMISSION. A pair whose template did not resolve to a
//     resource is RETURNED UNCONFIRMED (:267-271), not dropped. Admission keys
//     on service attribution alone (:213-241).
//   - (c) EVIDENCE RESOURCE LABELS. ResourceA and ResourceB come from the
//     caller's proxy map (:260-261), which the built-in builds from the
//     LOW-CARDINALITY labels while the service name comes from the full label
//     set — so a high-cardinality service label yields a confirmed pair whose
//     resources side is empty. That is the parity behaviour, not a defect to
//     paper over here.
//   - (d) CROSS-ACCOUNT PAIRS. The detector applies NO account rule (:272): both
//     ResolvedResource values reach the injected oracle whole. A collector whose
//     declared edges are within-graph keeps that refusal in ITS OWN oracle,
//     where the reason for it can be stated.
//
// NO CAPS. Nothing here bounds a result count and nothing truncates: every
// scored pair is returned. The sixty-second window is a semantic parameter of
// the overlap test (overlap.go), and the dependency oracle's own search bound,
// where it has one, belongs to the oracle.

// FindCorrelations returns one Result per candidate pair of error templates
// owned by DIFFERENT services whose time ranges overlap, each marked confirmed
// or not by the supplied oracle.
//
// The returned slice is sorted by (ServiceA, ServiceB, TemplateA), which is what
// makes a collect's emitted edge set independent of map iteration order.
//
// BAD INPUT ERRORS RATHER THAN DEGRADING, and the boundary is worth stating
// exactly. A REFUSAL is for a shape a well-formed collect cannot produce: a nil
// element in one of the three slices, a template or stream with no id, a range
// that runs backwards. Each of those either crashes the parity target or makes
// it attribute a template to the wrong stream, silently. An EMPTY RESULT is for
// well-formed input with nothing to say: no templates, one error template, no
// resolver, no oracle. The second is never reported as the first.
func FindCorrelations(in Input) ([]Result, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}

	errorTemplates := filterErrorTemplates(in.Templates)
	if len(errorTemplates) < 2 {
		return nil, nil
	}
	tmplStreams := mapTemplateStreams(errorTemplates, in.Chunks, in.Streams)
	if len(tmplStreams) < 2 {
		return nil, nil
	}
	tmplResources := resolveTemplateResources(tmplStreams, in.Resolver)

	pairs := buildCandidatePairs(errorTemplates, tmplStreams)
	results := make([]Result, 0, len(pairs))
	for _, p := range pairs {
		results = append(results, scoreCandidatePair(p, in.ProxyMap, tmplResources, in.Oracle))
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].ServiceA != results[j].ServiceA {
			return results[i].ServiceA < results[j].ServiceA
		}
		if results[i].ServiceB != results[j].ServiceB {
			return results[i].ServiceB < results[j].ServiceB
		}
		return results[i].TemplateA < results[j].TemplateA
	})
	return results, nil
}

// validate refuses the input shapes the detector cannot read truthfully.
//
// IT WALKS EVERY TEMPLATE, not just the error ones. A malformed INFO template is
// malformed input whichever filter would have dropped it, and reporting it only
// when it happens to be severe enough to reach the pairing loop would make the
// refusal depend on a field that has nothing to do with the defect.
func (in Input) validate() error {
	for i, t := range in.Templates {
		switch {
		case t == nil:
			return fmt.Errorf("correlation: Templates[%d] is nil; a template slice carries templates, "+
				"and a nil entry would be silently filtered away as if it had never been supplied", i)
		case t.ID == "":
			return fmt.Errorf("correlation: Templates[%d] has an empty ID; template ids are what a "+
				"correlation names, and an empty one collides with every other empty one", i)
		case rangeRunsBackwards(t.FirstSeen, t.LastSeen):
			return fmt.Errorf("correlation: Templates[%d] (%s) has LastSeen %s before FirstSeen %s; "+
				"a range that runs backwards scores an overlap no collect observed",
				i, t.ID, t.LastSeen.UTC().Format(time.RFC3339), t.FirstSeen.UTC().Format(time.RFC3339))
		}
	}
	for i, c := range in.Chunks {
		if c == nil {
			return fmt.Errorf("correlation: Chunks[%d] is nil; a chunk is how a template is attributed "+
				"to its service, and a nil entry attributes nothing", i)
		}
	}
	for i, s := range in.Streams {
		switch {
		case s == nil:
			return fmt.Errorf("correlation: Streams[%d] is nil; a nil stream cannot name a service", i)
		case s.ID == "":
			return fmt.Errorf("correlation: Streams[%d] has an empty ID; stream ids are what chunks "+
				"reference, and an empty one would claim every chunk that names no stream", i)
		}
	}
	return nil
}

// rangeRunsBackwards reports whether both bounds are known and the last one
// precedes the first. AN UNSET BOUND IS NOT BACKWARDS: an unknown FirstSeen with
// a known LastSeen is the documented unpadded case, not a malformed range.
func rangeRunsBackwards(first, last time.Time) bool {
	return !first.IsZero() && !last.IsZero() && last.Before(first)
}
