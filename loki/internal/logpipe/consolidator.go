// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"regexp"
	"sort"
)

// consolidator.go — the language consolidators and THE SETTLED DIVERGENCE from
// the built-in pipeline's remap.
//
// A consolidator merges the fragments a stack dump or a traceback shatters into
// dozens of one-line templates back into a single template. Skipping them emits
// a DIFFERENT TEMPLATE SET for the same input, and every chunk keyed on a
// template id moves with it.
//
// WHY THE INTERFACE RETURNS AN ABSORPTION MAP, WHICH THE BUILT-IN ONE DOES NOT.
// After consolidation some template ids no longer exist, and every entry that
// pointed at one has to be re-pointed or its chunk is dropped. The built-in
// pipeline rebuilds that mapping by GUESSING — it picks `for afterID := range
// after { best = afterID }`, the last key of a randomized map walk — and its
// two consolidators build the merged template with NO ID, so the merged
// template lands under the empty-string key and the guard that rejects an empty
// replacement rejects the very template that absorbed the fragments. Measured
// on the built-in produce path over 400 runs of one fixed input: an empty-id
// template node on 400 of 400, dropped fragments orphaned on 351 and remapped
// onto an unrelated survivor on 49, and CONTAINS edges emitted from template
// ids with no node in the batch.
//
// This collector diverges in TWO parts, and the divergence is settled rather
// than chosen: a collect has to be reproducible for carry-forward to mean
// anything.
//
//	PART ONE — a consolidator RETURNS which fragment ids each merged template
//	absorbed, so the remap targets the absorber. The relation cannot be
//	reconstructed afterwards, which is why the interface carries it. A pure
//	total order is not a substitute: a merged template takes the MAXIMUM
//	LastSeen of its group, so an unrelated template that fired after the burst
//	would deterministically win.
//
//	PART TWO — with no absorber, the target is the greatest SURVIVOR under
//	LastSeen descending, then FirstSeen descending, then id ascending. The last
//	key is total because the surviving set is keyed by id.
//
// And a merged template carries a real id, so no CONTAINS edge points at a node
// that is not in the batch and no entry is orphaned.

// Consolidation is one consolidator's output: the template set it leaves behind
// and, per merged template id, the fragment ids that merged into it.
type Consolidation struct {
	Templates []*Template
	// Absorbed maps a SURVIVING template's id to the ids it absorbed. A
	// consolidator that merged nothing returns a nil map.
	Absorbed map[string][]string
}

// Consolidator merges a family of structural noise fragments into coherent
// templates.
//
// THE INTERFACE IS ONE METHOD. An earlier revision also declared a Name(), which
// nothing ever called: RunConsolidators does not read it, no error message
// carries it, and neither implementation's value reached a test. A method no
// caller has is a method no test can observe, so it was removed rather than
// left as a shape a reader would assume is load-bearing.
type Consolidator interface {
	Consolidate(templates []*Template) Consolidation
}

// DefaultConsolidators is the standard set, in execution order.
func DefaultConsolidators() []Consolidator {
	return []Consolidator{&goStackConsolidator{}, &pythonTracebackConsolidator{}}
}

// RunConsolidators applies each consolidator in order and COMPOSES their
// absorption maps: a fragment absorbed in an earlier pass whose absorber is
// itself absorbed later resolves to the final surviving template, not to the
// intermediate one that no longer exists.
func RunConsolidators(consolidators []Consolidator, templates []*Template) Consolidation {
	// absorbedBy maps a vanished id to the id that took it, updated in place as
	// later passes absorb earlier absorbers.
	absorbedBy := make(map[string]string)
	for _, c := range consolidators {
		out := c.Consolidate(templates)
		templates = out.Templates
		for absorber, fragments := range out.Absorbed {
			for _, frag := range fragments {
				absorbedBy[frag] = absorber
			}
			// Re-point anything that had been absorbed BY this absorber's own
			// fragments in an earlier pass.
			for old, target := range absorbedBy {
				if target == absorber {
					continue
				}
				for _, frag := range fragments {
					if target == frag {
						absorbedBy[old] = absorber
					}
				}
			}
		}
	}
	if len(absorbedBy) == 0 {
		return Consolidation{Templates: templates}
	}
	out := make(map[string][]string, len(absorbedBy))
	for frag, absorber := range absorbedBy {
		out[absorber] = append(out[absorber], frag)
	}
	for _, frags := range out {
		sort.Strings(frags)
	}
	return Consolidation{Templates: templates, Absorbed: out}
}

// buildTemplateRemap maps every id that vanished during consolidation to the
// surviving template that takes its entries. It is the whole of the divergence
// described at the top of this file.
//
// It returns an error when a vanished id has no target at all, because that is
// the orphan case: an entry pointing at it would produce a chunk keyed on a
// template with no node, which is precisely the defect being avoided. With at
// least one survivor there is always a target, so the error is unreachable on a
// non-empty surviving set and is returned rather than ignored so a future change
// that breaks the invariant is loud.
func buildTemplateRemap(before, after map[string]*Template, absorbed map[string][]string) (map[string]string, error) {
	if len(before) == 0 {
		return nil, nil
	}
	// absorberOf inverts the absorption map: fragment id -> absorbing id.
	absorberOf := make(map[string]string, len(absorbed))
	for absorber, fragments := range absorbed {
		for _, frag := range fragments {
			absorberOf[frag] = absorber
		}
	}

	fallback := greatestSurvivor(after)
	remap := make(map[string]string)
	for id := range before {
		if _, kept := after[id]; kept {
			continue
		}
		if absorber, ok := absorberOf[id]; ok {
			if _, alive := after[absorber]; alive {
				remap[id] = absorber
				continue
			}
		}
		if fallback == "" {
			return nil, errNoSurvivingTemplate{dropped: id}
		}
		remap[id] = fallback
	}
	if len(remap) == 0 {
		return nil, nil
	}
	return remap, nil
}

// errNoSurvivingTemplate reports a dropped template with nothing to remap onto.
type errNoSurvivingTemplate struct{ dropped string }

func (e errNoSurvivingTemplate) Error() string {
	return "logpipe: template " + e.dropped +
		" was dropped by consolidation and no template survived to take its entries; " +
		"every entry pointing at it would produce a chunk keyed on a template with no node"
}

// greatestSurvivor is part two's total order: LastSeen descending, then
// FirstSeen descending, then id ascending. Ties break on the id, which is
// unique because `after` is keyed by it, so the order is total.
func greatestSurvivor(after map[string]*Template) string {
	best := ""
	var bestTpl *Template
	for id, tpl := range after {
		if tpl == nil {
			continue
		}
		if bestTpl == nil || survivorGreater(tpl, id, bestTpl, best) {
			best, bestTpl = id, tpl
		}
	}
	return best
}

func survivorGreater(a *Template, aID string, b *Template, bID string) bool {
	if !a.LastSeen.Equal(b.LastSeen) {
		return a.LastSeen.After(b.LastSeen)
	}
	if !a.FirstSeen.Equal(b.FirstSeen) {
		return a.FirstSeen.After(b.FirstSeen)
	}
	return aID < bID
}

// templatesByID indexes a template slice by id, dropping nils.
func templatesByID(templates []*Template) map[string]*Template {
	m := make(map[string]*Template, len(templates))
	for _, t := range templates {
		if t == nil {
			continue
		}
		m[t.ID] = t
	}
	return m
}

// Shared fragment-detection patterns used by more than one consolidator.
var (
	reGoStack         = regexp.MustCompile(`^goroutine\s+\d+`)
	reExceptionHeader = regexp.MustCompile(`(?i)^(exception|error|panic|traceback|caused by|fatal)`)
)
