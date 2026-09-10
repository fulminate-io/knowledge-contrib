// SPDX-License-Identifier: Apache-2.0

package main

import "regexp"

// consolidator.go — the pass between clustering and stream assembly that folds
// language-specific noise into one template, and the ABSORPTION RECORD that
// makes the fold reversible.
//
// WHY THE INTERFACE RETURNS MORE THAN A TEMPLATE LIST. A consolidator drops
// fragments and mints a replacement, and every entry that belonged to a dropped
// fragment has to be re-pointed at something. If the pass returns only the
// surviving list, the relation "M absorbed F1, F2, F3" is gone and the caller
// can do no better than guess which survivor to re-point at — which is exactly
// what the knowledge client's own pipeline does, by walking a Go map and
// keeping whichever key it visited last. That walk is unordered, so the same
// input produces different graphs on different runs, and entries land on an
// unrelated template. This module returns the absorption relation instead, so
// the re-pointing is decided by the data rather than by map iteration order.

// consolidation is one consolidator's output: the templates that survive, and
// which dropped template ids each survivor absorbed.
type consolidation struct {
	// Templates is the surviving set, in a deterministic order.
	Templates []*logTemplate
	// Absorbed maps a SURVIVING template's id to the ids of the templates it
	// merged in. A consolidator that dropped nothing leaves it empty.
	Absorbed map[string][]string
}

// consolidatorPass folds one family of noise. Implementations are pure with
// respect to the input slice's membership: they may mint new templates and omit
// old ones, but they never mutate a template they pass through.
type consolidatorPass interface {
	// Name identifies the pass in diagnostics.
	Name() string
	// Consolidate returns the surviving templates and the absorption relation.
	Consolidate(templates []*logTemplate) consolidation
}

// defaultConsolidators is the standard set, in execution order. Go stacks run
// before Python tracebacks because a Go panic's own lines can match the
// permissive Python fragment patterns, and consolidating the unambiguous family
// first keeps them out of the Python pass's input.
func defaultConsolidators() []consolidatorPass {
	return []consolidatorPass{
		&goStackConsolidator{},
		&pythonTracebackConsolidator{},
	}
}

// runConsolidators applies each pass in order and composes their absorption
// relations.
//
// COMPOSITION IS NOT CONCATENATION. A later pass can absorb a template an
// earlier pass MINTED, and the fragments the earlier pass folded into it must
// then follow it to the new survivor rather than pointing at a template that is
// no longer in the set. The rewrite below is what carries them.
func runConsolidators(passes []consolidatorPass, templates []*logTemplate) consolidation {
	absorbed := make(map[string][]string)
	current := templates
	for _, pass := range passes {
		out := pass.Consolidate(current)
		current = out.Templates
		for survivor, fragments := range out.Absorbed {
			// Anything that had pointed at one of these fragments now points at
			// the survivor that took it.
			for _, fragment := range fragments {
				if carried, ok := absorbed[fragment]; ok {
					absorbed[survivor] = append(absorbed[survivor], carried...)
					delete(absorbed, fragment)
				}
			}
			absorbed[survivor] = append(absorbed[survivor], fragments...)
		}
	}
	return consolidation{Templates: current, Absorbed: absorbed}
}

// Shared fragment-detection patterns. Both consolidators read them, so they sit
// here rather than in either one.
var (
	reGoStack         = regexp.MustCompile(`^goroutine\s+\d+`)
	reExceptionHeader = regexp.MustCompile(`(?i)^(exception|error|panic|traceback|caused by|fatal)`)
)
