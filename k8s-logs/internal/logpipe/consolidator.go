// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// consolidator.go — the pass that MERGES OR DROPS templates after clustering.
//
// WHY IT CHANGES THE GRAPH AND NOT JUST THE PRESENTATION. A Go panic or a
// Python traceback arrives as dozens of lines that Drain sees as dozens of
// unrelated shapes, so an unconsolidated graph has one template node per stack
// frame and the actual crash is invisible under them. Consolidation replaces a
// temporal group of fragments with ONE template — which changes the template
// SET, therefore the template IDS, therefore which chunks the CONTAINS edges
// join. Skipping this pass leaves a graph that type-checks and is wrong.

// Consolidator merges a family of structural noise into coherent templates.
type Consolidator interface {
	// Name identifies the consolidator in diagnostics.
	Name() string
	// Consolidate returns the template set after merging. It may return fewer
	// templates than it was given, and the templates it returns may be new.
	Consolidate(templates []*Template) []*Template
}

// DefaultConsolidators returns the standard set in execution order.
func DefaultConsolidators() []Consolidator {
	return []Consolidator{&goStackConsolidator{}, &pythonTracebackConsolidator{}}
}

// RunConsolidators applies each consolidator in turn.
func RunConsolidators(consolidators []Consolidator, templates []*Template) []*Template {
	for _, c := range consolidators {
		templates = c.Consolidate(templates)
	}
	return templates
}

// Shared crash-shape detection used by both consolidators.
var (
	reGoStack         = regexp.MustCompile(`^goroutine\s+\d+`)
	reExceptionHeader = regexp.MustCompile(`(?i)^(exception|error|panic|traceback|caused by|fatal)`)
)

// groupByTime splits a time-sorted template slice wherever the gap between one
// group's end and the next template's start exceeds window.
func groupByTime(sorted []*Template, window time.Duration) [][]*Template {
	if len(sorted) == 0 {
		return nil
	}
	var groups [][]*Template
	current := []*Template{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		prevEnd := current[len(current)-1].LastSeen
		if sorted[i].FirstSeen.Sub(prevEnd) <= window {
			current = append(current, sorted[i])
			continue
		}
		groups = append(groups, current)
		current = []*Template{sorted[i]}
	}
	return append(groups, current)
}

// minFragmentsToMerge is how many fragments a temporal group needs before it is
// merged. Below it the group is passed through unchanged: two lines that happen
// to look like stack frames are more likely to be two log lines than a crash.
const minFragmentsToMerge = 3

// exampleVarsContain reports whether any example row, joined, satisfies fn.
func exampleVarsContain(vars [][]string, fn func(string) bool) bool {
	return slices.ContainsFunc(vars, func(row []string) bool { return fn(strings.Join(row, " ")) })
}

// effectiveText is the best text to match a template on: its first example row
// when it has one, otherwise its pattern. The example is preferred because
// clustering has already replaced the discriminating parts of the pattern with
// wildcards, and the crash shapes below are recognized by exactly those parts.
func effectiveText(t *Template) string {
	if len(t.ExampleVars) > 0 {
		return strings.Join(t.ExampleVars[0], " ")
	}
	return t.Pattern
}

// sortByLastSeen orders templates by their last entry, which is the order the
// temporal grouping walks.
func sortByLastSeen(templates []*Template) {
	sort.Slice(templates, func(i, j int) bool { return templates[i].LastSeen.Before(templates[j].LastSeen) })
}
