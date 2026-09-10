// SPDX-License-Identifier: Apache-2.0

package main

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// consolidate_python.go — recognizing Python traceback fragments and merging
// each temporal group of three or more into one ERROR template.
//
// IT DIFFERS FROM THE GO CONSOLIDATOR IN THREE WAYS, and each is behavior. The
// window is five seconds rather than thirty, because a Python traceback is
// emitted in one burst. A TRACEBACK HEADER counts even when there are fewer
// than three other fragments, because the header alone identifies the event.
// And the merged pattern is BUILT FROM the header or exception class rather
// than being a fixed sentence, so two different exceptions do not merge into
// one template.

// pythonWindow is the temporal proximity that groups fragments.
const pythonWindow = 5 * time.Second

// pythonMinGroup is the smallest group that merges.
const pythonMinGroup = 3

// pythonHeaderPrefix is the line Python writes above every traceback.
const pythonHeaderPrefix = "Traceback (most recent call last)"

// pythonHeaderLimit truncates a long header inside the merged pattern. The
// pattern is a node's searchable text, and an unbounded header would carry a
// whole frame list into it.
const pythonHeaderLimit = 120

type pythonTracebackConsolidator struct{}

func (p *pythonTracebackConsolidator) Name() string { return "python_traceback" }

// Consolidate partitions the templates into traceback headers, other fragments
// and everything else, then merges each temporal group of three or more.
func (p *pythonTracebackConsolidator) Consolidate(templates []*LogTemplate) ConsolidationResult {
	var fragments, headers, normal []*LogTemplate
	for _, t := range templates {
		switch {
		case strings.HasPrefix(strings.TrimSpace(effectiveText(t)), pythonHeaderPrefix):
			headers = append(headers, t)
		case isPythonTracebackFragment(t):
			fragments = append(fragments, t)
		default:
			normal = append(normal, t)
		}
	}
	if len(fragments) < pythonMinGroup && len(headers) == 0 {
		return ConsolidationResult{Templates: templates}
	}

	all := append(headers, fragments...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].LastSeen.Before(all[j].LastSeen) })

	var absorptions []Absorption
	for _, group := range groupByTime(all, pythonWindow) {
		if len(group) < pythonMinGroup {
			normal = append(normal, group...)
			continue
		}
		merged := mergePythonGroup(group)
		normal = append(normal, merged)
		absorptions = append(absorptions, Absorption{Survivor: merged, Fragments: fragmentIDs(group)})
	}
	survivors, absorptions := dedupeMergedSurvivors(normal, absorptions)
	return ConsolidationResult{Templates: survivors, Absorptions: absorptions}
}

// effectiveText is the text a fragment is matched on: its first example row
// when it has one, else its pattern. A traceback frame's pattern is mostly
// wildcards, so the examples are where the recognizable text survives.
func effectiveText(t *LogTemplate) string {
	if len(t.ExampleVars) > 0 {
		return strings.Join(t.ExampleVars[0], " ")
	}
	return t.Pattern
}

// mergePythonGroup folds a group into one ERROR template whose pattern names
// the traceback header or the exception class it found.
func mergePythonGroup(group []*LogTemplate) *LogTemplate {
	merged := &LogTemplate{
		Severity:  SeverityError,
		FirstSeen: group[0].FirstSeen,
		LastSeen:  group[0].LastSeen,
	}
	var header, exception string
	for _, t := range group {
		absorbInto(merged, t)
		text := effectiveText(t)
		if header == "" && strings.Contains(text, "Traceback") {
			header = text
		}
		if exception == "" && rePyExceptionClass.MatchString(text) {
			exception = text
		}
	}
	merged.Pattern = buildPythonPattern(header, exception)
	merged.ExampleVars = buildPythonExamples(header, exception, group)
	return finishMerged(merged)
}

// buildPythonPattern names the merged template from the best evidence it found,
// falling back to a bare label when it found neither.
func buildPythonPattern(header, exception string) string {
	switch {
	case header != "":
		if len(header) > pythonHeaderLimit {
			header = header[:pythonHeaderLimit-3] + "..."
		}
		return "Python exception: " + header
	case exception != "":
		return "Python exception: " + exception
	default:
		return "Python traceback"
	}
}

// buildPythonExamples keeps the header and the exception class as examples,
// falling back to the group's first example row.
func buildPythonExamples(header, exception string, group []*LogTemplate) [][]string {
	var examples [][]string
	if header != "" {
		examples = append(examples, []string{header})
	}
	if exception != "" && exception != header {
		examples = append(examples, []string{exception})
	}
	if len(examples) == 0 && len(group[0].ExampleVars) > 0 {
		examples = [][]string{group[0].ExampleVars[0]}
	}
	return examples
}

// The traceback detection patterns, matched multi-line because a captured
// example can carry several frames.
var (
	rePyFileFrame      = regexp.MustCompile(`(?m)^\s+File "`)
	rePyUnderline      = regexp.MustCompile(`(?m)^\s+[\^~]{5,}`)
	rePyRaiseAwait     = regexp.MustCompile(`(?m)^\s*(raise |await |return await |return \w+[\.(]|with \w)`)
	rePySelfCall       = regexp.MustCompile(`(?m)^\s*self\.\w+`)
	rePyAssignAwait    = regexp.MustCompile(`(?m)=\s+await\s+`)
	rePyExceptionClass = regexp.MustCompile(`(?m)^\w+(\.\w+)*\.\w*(Timeout|Error|Exception|Fault)\b`)
	rePyExceptionChain = regexp.MustCompile(`(?i)^(The above exception|During handling of the above)`)
	rePyObjectRepr     = regexp.MustCompile(`(?m)^\w+:\s+<[\w.]+\s+object\s+at\s+0x`)
)

// isPythonTracebackFragment tests the pattern and the examples, for the same
// reason the Go detector does.
func isPythonTracebackFragment(t *LogTemplate) bool {
	return matchesPyTracebackPattern(t.Pattern) || exampleVarsContain(t.ExampleVars, matchesPyTracebackPattern)
}

// matchesPyTracebackPattern reports whether a line looks like part of a Python
// traceback.
func matchesPyTracebackPattern(msg string) bool {
	return rePyUnderline.MatchString(msg) ||
		rePyRaiseAwait.MatchString(msg) ||
		rePySelfCall.MatchString(msg) ||
		rePyAssignAwait.MatchString(msg) ||
		rePyExceptionClass.MatchString(msg) ||
		rePyExceptionChain.MatchString(msg) ||
		rePyFileFrame.MatchString(msg) ||
		rePyObjectRepr.MatchString(msg)
}
