// SPDX-License-Identifier: Apache-2.0

package main

import (
	"sort"
	"time"
)

// consolidate.go — MERGING TRACE NOISE INTO ONE TEMPLATE, and the ONE PLACE
// THIS COLLECTOR DIVERGES FROM THE BUILT-IN PIPELINE ON PURPOSE.
//
// A crashing process emits a stack dump one line at a time, so Drain clusters
// it into dozens of templates that mean one event. The consolidators below
// recognize those fragments and merge each temporal group into a single
// template. Every entry that pointed at a dropped fragment must then be
// re-pointed at the survivor.
//
// THE DIVERGENCE, AND WHY IT IS NOT DRIFT. The built-in re-pointing is
// defective in two measured ways: it picks the replacement by taking the LAST
// key of a randomized Go map walk, so the same input re-points differently
// between runs; and its merged template is built without an id, so it lands
// under the empty-string key, is rejected as a replacement, and orphans the
// entries of every fragment it absorbed — leaving CONTAINS edges from template
// ids that have no node. Reproducing that would make this collector's graph
// non-deterministic, which defeats the carry-forward the whole id scheme exists
// for.
//
// WHAT THIS COLLECTOR DOES INSTEAD, in two parts. First, THE ABSORBER WINS: a
// consolidator RETURNS which fragment ids each merged template absorbed, so the
// re-pointing follows the actual merge rather than guessing. That has to be a
// return value — the relation cannot be reconstructed after the fact, which is
// the structural reason the built-in guesses. Second, for a fragment dropped
// with NO absorber, a TOTAL ORDER over the survivors picks the target: LastSeen
// descending, then FirstSeen descending, then ID ascending. The tie-break is
// total, so it cannot depend on map order.

// Consolidator merges one family of noise fragments.
type Consolidator interface {
	// Name identifies the consolidator in diagnostics.
	Name() string
	// Consolidate returns the surviving templates and the absorption relation
	// that produced them. A consolidator that merged nothing returns the input
	// templates and no absorptions.
	Consolidate(templates []*LogTemplate) ConsolidationResult
}

// ConsolidationResult is one consolidator's output.
type ConsolidationResult struct {
	// Templates is the surviving set, replacing the input set.
	Templates []*LogTemplate
	// Absorptions records, per merged survivor, the ids it absorbed. It is
	// what makes the re-pointing exact instead of a guess.
	Absorptions []Absorption
}

// Absorption is one merged survivor and the template ids that went into it.
type Absorption struct {
	Survivor  *LogTemplate
	Fragments []string
}

// DefaultConsolidators is the standard set, in execution order: Go stack dumps
// first, then Python tracebacks. The order is part of the vocabulary — a
// fragment claimed by the first consolidator is not offered to the second.
func DefaultConsolidators() []Consolidator {
	return []Consolidator{&goStackConsolidator{}, &pythonTracebackConsolidator{}}
}

// runConsolidators applies each consolidator in order and returns the surviving
// templates together with the FLATTENED absorption relation: a map from an
// absorbed template id to the id of the survivor that holds it.
//
// A consolidator later in the chain can absorb a survivor produced by an
// earlier one, so each round re-points the relation accumulated so far. Without
// that, a fragment absorbed in round one and re-absorbed in round two would map
// to a template that no longer exists.
func runConsolidators(consolidators []Consolidator, templates []*LogTemplate) ([]*LogTemplate, map[string]string) {
	absorbedBy := make(map[string]string)
	for _, c := range consolidators {
		result := c.Consolidate(templates)
		for _, a := range result.Absorptions {
			if a.Survivor == nil {
				continue
			}
			for _, fragment := range a.Fragments {
				absorbedBy[fragment] = a.Survivor.ID
			}
		}
		// Re-point earlier absorptions whose survivor was itself absorbed.
		for fragment, survivor := range absorbedBy {
			if next, ok := absorbedBy[survivor]; ok && next != survivor {
				absorbedBy[fragment] = next
			}
		}
		templates = result.Templates
	}
	return templates, absorbedBy
}

// buildTemplateRemap maps every template id that did not survive consolidation
// onto the id of a survivor, so no entry is left pointing at a template with no
// node.
//
// The absorption relation answers first. A dropped fragment with no absorber
// falls to the total order, and when NOTHING survived, the remap is empty and
// the caller's entries keep ids with no node — which chunk assembly then skips,
// so the graph carries no dangling edge either way.
func buildTemplateRemap(before []*LogTemplate, after []*LogTemplate, absorbedBy map[string]string) map[string]string {
	surviving := make(map[string]struct{}, len(after))
	for _, t := range after {
		if t != nil {
			surviving[t.ID] = struct{}{}
		}
	}
	fallback := pickRemapFallback(after)
	remap := make(map[string]string)
	for _, t := range before {
		if t == nil {
			continue
		}
		if _, kept := surviving[t.ID]; kept {
			continue
		}
		if absorber, ok := absorbedBy[t.ID]; ok {
			if _, live := surviving[absorber]; live {
				remap[t.ID] = absorber
				continue
			}
		}
		if fallback != "" {
			remap[t.ID] = fallback
		}
	}
	return remap
}

// pickRemapFallback returns the survivor a dropped-without-absorber fragment
// re-points at, by the total order LastSeen descending, FirstSeen descending,
// ID ascending. The final ID comparison is what makes the order total: without
// it two survivors sharing both timestamps would tie and the choice would fall
// back to slice order.
func pickRemapFallback(after []*LogTemplate) string {
	candidates := make([]*LogTemplate, 0, len(after))
	for _, t := range after {
		if t != nil {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].LastSeen.Equal(candidates[j].LastSeen) {
			return candidates[i].LastSeen.After(candidates[j].LastSeen)
		}
		if !candidates[i].FirstSeen.Equal(candidates[j].FirstSeen) {
			return candidates[i].FirstSeen.After(candidates[j].FirstSeen)
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates[0].ID
}

// groupByTime splits a slice already sorted by LastSeen into runs whose
// consecutive members start within window of the previous member's end.
func groupByTime(sorted []*LogTemplate, window time.Duration) [][]*LogTemplate {
	if len(sorted) == 0 {
		return nil
	}
	var groups [][]*LogTemplate
	current := []*LogTemplate{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		prevEnd := current[len(current)-1].LastSeen
		if sorted[i].FirstSeen.Sub(prevEnd) <= window {
			current = append(current, sorted[i])
			continue
		}
		groups = append(groups, current)
		current = []*LogTemplate{sorted[i]}
	}
	return append(groups, current)
}

// fragmentIDs lists a group's template ids, which is the Fragments half of an
// Absorption.
func fragmentIDs(group []*LogTemplate) []string {
	out := make([]string, 0, len(group))
	for _, t := range group {
		out = append(out, t.ID)
	}
	return out
}

// finishMerged stamps the id and alias on a merged survivor.
//
// GIVING THE MERGED TEMPLATE AN ID IS HALF THE DIVERGENCE. The built-in merge
// leaves it empty, which is what makes the merged template unusable as a remap
// target and orphans everything it absorbed. Here the id is derived from the
// merged pattern by the same rule every other template id follows, so the
// survivor is an ordinary node.
func finishMerged(merged *LogTemplate) *LogTemplate {
	merged.ID = templateID(merged.Pattern)
	merged.Alias = TemplateAliasFor(merged)
	return merged
}

// absorbInto folds a fragment's aggregates into a merged survivor: counts sum,
// the time range widens, and severity takes the maximum.
func absorbInto(merged, fragment *LogTemplate) {
	merged.Count += fragment.Count
	if fragment.FirstSeen.Before(merged.FirstSeen) {
		merged.FirstSeen = fragment.FirstSeen
	}
	if fragment.LastSeen.After(merged.LastSeen) {
		merged.LastSeen = fragment.LastSeen
	}
	if severityIndex(fragment.Severity) > severityIndex(merged.Severity) {
		merged.Severity = fragment.Severity
	}
}

// dedupeMergedSurvivors folds merged survivors that landed on the SAME id into
// one template, re-pointing the absorptions of the folded copies at the keeper.
//
// TWO GROUPS CAN PRODUCE THE SAME PATTERN — the Go merge names every survivor
// "Go runtime crash (goroutine dump)" — and a template id is a hash of its
// pattern, so two crashes in one collect would otherwise emit two nodes under
// one id. Two templates with one pattern ARE one template, so they are folded
// rather than renamed: renaming would put a value in the pattern that no log
// line contains.
func dedupeMergedSurvivors(templates []*LogTemplate, absorptions []Absorption) ([]*LogTemplate, []Absorption) {
	keeper := make(map[string]*LogTemplate, len(templates))
	out := make([]*LogTemplate, 0, len(templates))
	for _, t := range templates {
		existing, dup := keeper[t.ID]
		if !dup {
			keeper[t.ID] = t
			out = append(out, t)
			continue
		}
		absorbInto(existing, t)
		existing.Alias = TemplateAliasFor(existing)
	}
	for i := range absorptions {
		if k, ok := keeper[absorptions[i].Survivor.ID]; ok {
			absorptions[i].Survivor = k
		}
	}
	return out, absorptions
}
