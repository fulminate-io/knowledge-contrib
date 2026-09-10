// SPDX-License-Identifier: Apache-2.0

package logpipe

import "sort"

// process.go — clustering plus consolidation, and the per-entry template ids
// that come out of it.
//
// TEMPLATE IDS ARE RESOLVED AFTER CLUSTERING FINISHES, never as entries arrive.
// Drain recomputes a template's id whenever its pattern broadens, so the id an
// entry saw at insertion is stale the moment another entry joins its cluster.
// Holding the POINTER through clustering and reading .ID afterwards is what
// makes every entry of one cluster resolve to one id.

// ProcessEntries clusters entries into templates, consolidates them, and
// returns the final template set with a per-entry parallel slice of template
// ids. An entry whose message held no tokens gets an empty id, which chunk
// assembly skips.
func ProcessEntries(entries []Entry, drainCfg DrainConfig) ([]*Template, []string) {
	drain := NewDrainEngine(drainCfg)
	entryTemplates := make([]*Template, len(entries))
	for i, e := range entries {
		entryTemplates[i] = drain.AddMessage(e)
	}

	clustered := drain.Templates()
	before := TemplatesByID(clustered)
	consolidated := RunConsolidators(DefaultConsolidators(), clustered)
	remap := buildTemplateRemap(before, TemplatesByID(consolidated))

	entryTemplateIDs := make([]string, len(entries))
	for i, tpl := range entryTemplates {
		if tpl == nil {
			continue
		}
		id := tpl.ID
		if mapped, ok := remap[id]; ok {
			id = mapped
		}
		entryTemplateIDs[i] = id
	}
	return consolidated, entryTemplateIDs
}

// buildTemplateRemap maps every template id consolidation removed onto the
// surviving template that replaced it.
//
// THE REPLACEMENT IS CHOSEN UNDER A STATED TOTAL ORDER, AND THAT IS A
// DELIBERATE DIVERGENCE FROM THE BUILT-IN PIPELINE. The shared implementation
// picks its replacement by walking a Go map and keeping the last key it sees,
// which is randomized per run: two collects of identical input can produce
// different template ids and therefore different chunk ids, which is a
// carry-forward hazard rather than a cosmetic one. Here the surviving template
// whose time range best covers the dropped one wins, ties broken by earliest
// FirstSeen and then by id, so the answer is a function of the input alone.
//
// A dropped template with NO surviving overlap keeps its own id and is absent
// from the map. Its entries then reference a template the result does not
// carry; chunk assembly skips them rather than attaching them to an unrelated
// pattern, which is the honest outcome when consolidation dropped a shape
// without replacing it.
func buildTemplateRemap(before, after map[string]*Template) map[string]string {
	if len(before) == 0 || len(after) == 0 {
		return nil
	}
	survivors := make([]*Template, 0, len(after))
	for _, t := range after {
		survivors = append(survivors, t)
	}
	sort.Slice(survivors, func(i, j int) bool {
		if !survivors[i].FirstSeen.Equal(survivors[j].FirstSeen) {
			return survivors[i].FirstSeen.Before(survivors[j].FirstSeen)
		}
		return survivors[i].ID < survivors[j].ID
	})

	remap := make(map[string]string)
	for id, dropped := range before {
		if _, kept := after[id]; kept {
			continue
		}
		if best := bestSurvivor(dropped, survivors); best != "" {
			remap[id] = best
		}
	}
	return remap
}

// bestSurvivor returns the id of the survivor whose time range overlaps the
// dropped template's by the most, or the empty string when none overlaps.
// survivors must already be in the total order buildTemplateRemap establishes,
// which is what makes ties resolve the same way every run.
func bestSurvivor(dropped *Template, survivors []*Template) string {
	best := ""
	var bestOverlap float64
	for _, s := range survivors {
		overlap, ok := rangeOverlapSeconds(dropped, s)
		if !ok {
			continue
		}
		if best == "" || overlap > bestOverlap {
			best, bestOverlap = s.ID, overlap
		}
	}
	return best
}

// rangeOverlapSeconds returns how many seconds two templates' [FirstSeen,
// LastSeen] ranges share, and whether they touch at all. Two point events at
// the same instant overlap by zero seconds and still report true, which is why
// the boolean is separate from the number.
func rangeOverlapSeconds(a, b *Template) (float64, bool) {
	if a == nil || b == nil {
		return 0, false
	}
	if a.FirstSeen.IsZero() || a.LastSeen.IsZero() || b.FirstSeen.IsZero() || b.LastSeen.IsZero() {
		return 0, false
	}
	if a.LastSeen.Before(b.FirstSeen) || b.LastSeen.Before(a.FirstSeen) {
		return 0, false
	}
	start := a.FirstSeen
	if b.FirstSeen.After(start) {
		start = b.FirstSeen
	}
	end := a.LastSeen
	if b.LastSeen.Before(end) {
		end = b.LastSeen
	}
	return end.Sub(start).Seconds(), true
}
