// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"regexp"
	"strings"
	"time"
)

// consolidator_python.go — Python tracebacks.
//
// The Go consolidator merges only when it has enough fragments; this one also
// merges when it has a TRACEBACK HEADER, because that line alone is
// unambiguous. A traceback's frames are otherwise indistinguishable from
// ordinary source-quoting log lines, and requiring three of them would leave a
// short traceback scattered across the graph.

type pythonTracebackConsolidator struct{}

func (p *pythonTracebackConsolidator) Name() string { return "python_traceback" }

// pythonWindow bounds one traceback in time. It is much tighter than the Go
// window because a traceback is emitted by one interpreter in one write, so a
// five-second gap between two frames means two tracebacks.
const pythonWindow = 5 * time.Second

// pythonHeaderPrefix is the line the interpreter writes above every traceback.
const pythonHeaderPrefix = "Traceback (most recent call last)"

// Consolidate merges temporal groups of Python traceback fragments into one
// ERROR template each.
func (p *pythonTracebackConsolidator) Consolidate(templates []*Template) []*Template {
	var fragments, headers, normal []*Template
	for _, t := range templates {
		switch text := effectiveText(t); {
		case strings.HasPrefix(strings.TrimSpace(text), pythonHeaderPrefix):
			headers = append(headers, t)
		case isPythonTracebackFragment(t):
			fragments = append(fragments, t)
		default:
			normal = append(normal, t)
		}
	}
	if len(fragments) < minFragmentsToMerge && len(headers) == 0 {
		return templates
	}

	allPy := make([]*Template, 0, len(headers)+len(fragments))
	allPy = append(allPy, headers...)
	allPy = append(allPy, fragments...)
	sortByLastSeen(allPy)

	for _, group := range groupByTime(allPy, pythonWindow) {
		if len(group) < minFragmentsToMerge {
			normal = append(normal, group...)
			continue
		}
		normal = append(normal, mergePythonGroup(group))
	}
	return normal
}

// mergePythonGroup folds a group into one template, naming it after the best
// header or exception class it saw.
func mergePythonGroup(group []*Template) *Template {
	merged := &Template{
		Severity:  SeverityError,
		FirstSeen: group[0].FirstSeen,
		LastSeen:  group[0].LastSeen,
	}
	var bestHeader, bestException string
	for _, t := range group {
		merged.Count += t.Count
		if t.FirstSeen.Before(merged.FirstSeen) {
			merged.FirstSeen = t.FirstSeen
		}
		if t.LastSeen.After(merged.LastSeen) {
			merged.LastSeen = t.LastSeen
		}
		if SeverityIndex(t.Severity) > SeverityIndex(merged.Severity) {
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
	merged.ID = TemplateID(merged.Pattern)
	merged.Alias = TemplateAliasFor(merged)
	return merged
}

// pythonHeaderLimit bounds the header text folded into the merged pattern. The
// pattern becomes a node's SymbolName fallback, so an unbounded one would put a
// whole traceback into a name.
const pythonHeaderLimit = 120

// buildPythonPattern names the merged template after the header, else the
// exception class, else a bare label.
func buildPythonPattern(header, exception string) string {
	switch {
	case header != "":
		truncated := header
		if len(truncated) > pythonHeaderLimit {
			truncated = truncated[:pythonHeaderLimit-3] + "..."
		}
		return "Python exception: " + truncated
	case exception != "":
		return "Python exception: " + exception
	default:
		return "Python traceback"
	}
}

// buildPythonExamples keeps the header and the exception class as the merged
// template's examples, falling back to the group's first example.
func buildPythonExamples(header, exception string, group []*Template) [][]string {
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

// The Python traceback shapes. Each is anchored per line, because a traceback
// reaches the collector as one multi-line message as often as it does as many.
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

func isPythonTracebackFragment(t *Template) bool {
	return matchesPyTracebackPattern(t.Pattern) || exampleVarsContain(t.ExampleVars, matchesPyTracebackPattern)
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
