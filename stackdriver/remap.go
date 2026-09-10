// SPDX-License-Identifier: Apache-2.0

package main

import "sort"

// remap.go — WHERE AN ENTRY GOES WHEN ITS TEMPLATE WAS CONSOLIDATED AWAY, and
// the fold that keeps one id naming one template.
//
// THIS IS THE MODULE'S LARGEST DELIBERATE DIVERGENCE FROM THE KNOWLEDGE
// CLIENT'S OWN LOG PIPELINE, and the divergence is from three measured defects
// rather than from taste. In that pipeline
// (cmd/knowledge/internal/collector/logs/pipeline_process.go, buildTemplateRemap
// and consolidator_gostack.go, mergeGoStackGroup):
//
//  1. THE REMAP TARGET IS PICKED BY A MAP WALK. For each dropped id it iterates
//     the surviving map and keeps the last key it happened to visit. Go
//     randomizes map iteration, so the same input remaps onto different
//     templates on different runs and the graph is not reproducible.
//  2. THE MERGED TEMPLATE HAS NO ID. Its merge builds the replacement with no
//     ID field and nothing downstream computes one, so it lands under a
//     store-generated id that differs between collects.
//  3. ENTRIES ARE ORPHANED. Because the merged template's id is empty, the walk
//     above can select it and then reject it, writing no remap at all — so
//     entries keep a template id that names no node, and the CONTAINS edges
//     built from those ids dangle.
//
// A collector whose second collect must reconcile against its first cannot
// inherit any of the three. What replaces them is a rule that reads only the
// data: the ABSORBER wins where one exists, and a total order over the
// survivors decides the rest.

// buildTemplateRemap maps each dropped template id to the surviving id its
// entries move to.
//
// PART ONE, THE ABSORBER WINS. When a consolidator merged a fragment into a new
// template, that template is where the fragment's entries belong, and no
// ordering rule can be substituted for knowing it: a merge sets the merged
// template's LastSeen to the MAXIMUM over its group, so an unrelated template
// that fired after the burst has a greater LastSeen and a pure recency order
// deterministically picks the wrong one.
//
// PART TWO, THE TOTAL ORDER, applies only where no absorber exists — a
// consolidator that dropped a template without minting a replacement. The
// survivor greatest under LastSeen descending, then FirstSeen descending, then
// id ascending. It is TOTAL because the survivors are keyed by id and their ids
// are therefore distinct, so the third level always decides.
//
// A nil return means there is nothing to map to, which happens only when
// consolidation left no template at all.
func buildTemplateRemap(before []*logTemplate, after consolidation) map[string]string {
	if len(before) == 0 || len(after.Templates) == 0 {
		return nil
	}
	surviving := make(map[string]*logTemplate, len(after.Templates))
	for _, t := range after.Templates {
		if t != nil {
			surviving[t.ID] = t
		}
	}

	// The absorption relation, inverted: fragment id to the survivor that took
	// it. A fragment naming a survivor that is no longer in the set is skipped
	// rather than trusted, so part two decides it instead.
	absorber := make(map[string]string)
	for survivorID, fragments := range after.Absorbed {
		if _, live := surviving[survivorID]; !live {
			continue
		}
		for _, fragment := range fragments {
			absorber[fragment] = survivorID
		}
	}

	fallback := greatestSurvivor(after.Templates)
	remap := make(map[string]string)
	for _, t := range before {
		if t == nil {
			continue
		}
		if _, kept := surviving[t.ID]; kept {
			continue
		}
		if target, ok := absorber[t.ID]; ok {
			remap[t.ID] = target
			continue
		}
		if fallback != "" {
			remap[t.ID] = fallback
		}
	}
	if len(remap) == 0 {
		return nil
	}
	return remap
}

// greatestSurvivor returns the id greatest under the total order: LastSeen
// descending, FirstSeen descending, id ascending.
func greatestSurvivor(templates []*logTemplate) string {
	var best *logTemplate
	for _, t := range templates {
		if t == nil {
			continue
		}
		if best == nil || survivorPrecedes(t, best) {
			best = t
		}
	}
	if best == nil {
		return ""
	}
	return best.ID
}

// survivorPrecedes reports whether a ranks ahead of b under the total order.
func survivorPrecedes(a, b *logTemplate) bool {
	if !a.LastSeen.Equal(b.LastSeen) {
		return a.LastSeen.After(b.LastSeen)
	}
	if !a.FirstSeen.Equal(b.FirstSeen) {
		return a.FirstSeen.After(b.FirstSeen)
	}
	return a.ID < b.ID
}

// foldDuplicateTemplates collapses templates that share an id into one, and
// returns the set sorted by id.
//
// WHY IT IS NEEDED AT ALL: the Go-stack merge names every crash with the same
// fixed pattern, so two bursts far enough apart to be separate groups produce
// two merged templates with the same pattern and therefore the same id. Emitting
// both would put two nodes under one id in a single batch, which is a graph
// nobody can read back. Folding is lossless at the graph level because the
// temporal separation lives in the CHUNKS, which fold the window, and not in the
// template, which is a pattern-level object.
//
// The knowledge client's pipeline cannot reach this case: its merged templates
// carry no id at all, so its duplicates are invisible rather than absent.
func foldDuplicateTemplates(templates []*logTemplate) []*logTemplate {
	byID := make(map[string]*logTemplate, len(templates))
	order := make([]string, 0, len(templates))
	for _, t := range templates {
		if t == nil {
			continue
		}
		existing, seen := byID[t.ID]
		if !seen {
			byID[t.ID] = t
			order = append(order, t.ID)
			continue
		}
		existing.Count += t.Count
		widenTemplateRange(existing, t)
		if severityIndex(t.Severity) > severityIndex(existing.Severity) {
			existing.Severity = t.Severity
			// Severity is an input to the alias, so a raise re-derives it here
			// for the same reason the clusterer re-derives it on update.
			existing.Alias = templateAliasFor(existing)
		}
		for _, row := range t.ExampleVars {
			if len(existing.ExampleVars) >= maxExampleVars {
				break
			}
			existing.ExampleVars = append(existing.ExampleVars, row)
		}
	}
	sort.Strings(order)
	out := make([]*logTemplate, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}
