// SPDX-License-Identifier: Apache-2.0

package main

import (
	"regexp"
	"strings"
	"time"
)

// consolidator_python.go — the Python traceback fold. Same shape as the Go
// stack pass: partition, group by proximity, merge the groups that are large
// enough, record what each merge absorbed.

// pythonWindow is tighter than the Go one because a Python traceback is written
// by one interpreter in one flush, whereas a Go dump can span many goroutines.
const pythonWindow = 5 * time.Second

// pythonMinGroup is the group size that constitutes a traceback.
const pythonMinGroup = 3

// pythonHeaderPrefix is the line the interpreter opens a traceback with.
const pythonHeaderPrefix = "Traceback (most recent call last)"

// pythonHeaderTruncate bounds how much of a header the merged pattern carries.
// The pattern is hashed into the template id, so an unbounded header would put
// an arbitrary amount of one entry's text into an id.
const pythonHeaderTruncate = 120

type pythonTracebackConsolidator struct{}

func (p *pythonTracebackConsolidator) Name() string { return "python_traceback" }

// Consolidate folds Python traceback fragments.
//
// THE HEADER IS PARTITIONED SEPARATELY FROM THE FRAGMENTS and the early return
// admits a group when EITHER there are enough fragments OR a header is present,
// because a traceback whose frames were masked into wildcards can leave the
// header as the only recognizable line.
func (p *pythonTracebackConsolidator) Consolidate(templates []*logTemplate) consolidation {
	var fragments, headers, normal []*logTemplate
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
		return consolidation{Templates: templates}
	}

	allPy := make([]*logTemplate, 0, len(headers)+len(fragments))
	allPy = append(allPy, headers...)
	allPy = append(allPy, fragments...)
	sortTemplatesByLastSeen(allPy)

	absorbed := make(map[string][]string)
	for _, group := range groupByTime(allPy, pythonWindow) {
		if len(group) < pythonMinGroup {
			normal = append(normal, group...)
			continue
		}
		merged := mergePythonGroup(group)
		normal = append(normal, merged)
		absorbed[merged.ID] = append(absorbed[merged.ID], templateIDs(group)...)
	}
	return consolidation{Templates: normal, Absorbed: absorbed}
}

// mergePythonGroup builds the replacement template, computing its id from its
// own pattern exactly as every other template in this module does.
func mergePythonGroup(group []*logTemplate) *logTemplate {
	merged := &logTemplate{
		Severity:  severityError,
		FirstSeen: group[0].FirstSeen,
		LastSeen:  group[0].LastSeen,
	}
	var bestHeader, bestException string
	for _, t := range group {
		merged.Count += t.Count
		widenTemplateRange(merged, t)
		if severityIndex(t.Severity) > severityIndex(merged.Severity) {
			merged.Severity = t.Severity
		}
		text := effectiveText(t)
		if bestHeader == "" && strings.Contains(text, "Traceback") {
			bestHeader = text
		}
		if bestException == "" && rePyExceptionClass.MatchString(text) {
			bestException = text
		}
	}
	merged.Pattern = buildPythonPattern(bestHeader, bestException)
	merged.ExampleVars = buildPythonExamples(bestHeader, bestException, group)
	merged.ID = templateID(merged.Pattern)
	merged.Alias = templateAliasFor(merged)
	return merged
}

// effectiveText is the text a pattern test should read: the first recorded
// variable row when there is one, because the pattern itself may have been
// masked down to wildcards, and the pattern otherwise.
func effectiveText(t *logTemplate) string {
	if len(t.ExampleVars) > 0 {
		return strings.Join(t.ExampleVars[0], " ")
	}
	return t.Pattern
}

// buildPythonPattern derives the merged pattern from the best evidence in the
// group, falling back to a fixed string when the group carried neither a header
// nor an exception class.
func buildPythonPattern(header, exception string) string {
	switch {
	case header != "":
		truncated := header
		if len(truncated) > pythonHeaderTruncate {
			truncated = truncated[:pythonHeaderTruncate-3] + "..."
		}
		return "Python exception: " + truncated
	case exception != "":
		return "Python exception: " + exception
	default:
		return "Python traceback"
	}
}

// buildPythonExamples keeps the header and the exception class as the merged
// template's examples, so a reader sees what was folded.
func buildPythonExamples(header, exception string, group []*logTemplate) [][]string {
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

// Python traceback shapes.
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

func isPythonTracebackFragment(t *logTemplate) bool {
	return matchesPyTracebackPattern(t.Pattern) ||
		exampleVarsContain(t.ExampleVars, matchesPyTracebackPattern)
}

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
