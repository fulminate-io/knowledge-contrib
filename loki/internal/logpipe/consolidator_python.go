// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// consolidator_python.go — the Python traceback consolidator.
//
// It differs from the Go one in two ways worth naming: a traceback HEADER is
// admitted even when it is the only fragment of its kind, so the pass runs on a
// header plus two frames; and the window is five seconds rather than thirty,
// because a traceback is emitted in one burst while a goroutine dump is not.

type pythonTracebackConsolidator struct{}

// Consolidate merges Python traceback fragments and reports what each merged
// template absorbed.
func (p *pythonTracebackConsolidator) Consolidate(templates []*Template) Consolidation {
	var fragments, headers, normal []*Template
	for _, t := range templates {
		text := effectiveText(t)
		switch {
		case strings.HasPrefix(strings.TrimSpace(text), "Traceback (most recent call last)"):
			headers = append(headers, t)
		case isPythonTracebackFragment(t):
			fragments = append(fragments, t)
		default:
			normal = append(normal, t)
		}
	}
	if len(fragments) < 3 && len(headers) == 0 {
		return Consolidation{Templates: templates}
	}

	allPy := append(headers, fragments...)
	sort.Slice(allPy, func(i, j int) bool { return allPy[i].LastSeen.Before(allPy[j].LastSeen) })

	absorbed := make(map[string][]string)
	for _, group := range groupByTime(allPy, 5*time.Second) {
		if len(group) < 3 {
			normal = append(normal, group...)
			continue
		}
		merged := mergePythonGroup(group)
		normal = append(normal, merged)
		for _, t := range group {
			absorbed[merged.ID] = append(absorbed[merged.ID], t.ID)
		}
	}
	if len(absorbed) == 0 {
		return Consolidation{Templates: normal}
	}
	return Consolidation{Templates: normal, Absorbed: absorbed}
}

// effectiveText is the text a fragment is matched on: the first example row
// when there is one, because that carries the original line, and the pattern
// otherwise.
func effectiveText(t *Template) string {
	if len(t.ExampleVars) > 0 {
		return strings.Join(t.ExampleVars[0], " ")
	}
	return t.Pattern
}

// mergePythonGroup folds one temporal group into a single ERROR template,
// carrying a real id for the reason mergeGoStackGroup gives.
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
		if strings.Contains(text, "Traceback") && bestHeader == "" {
			bestHeader = text
		}
		if rePyExceptionClass.MatchString(text) && bestException == "" {
			bestException = text
		}
	}
	merged.Pattern = buildPythonTemplate(bestHeader, bestException)
	merged.ExampleVars = buildPythonExamples(bestHeader, bestException, group)
	merged.ID = TemplateID(merged.Pattern)
	merged.Alias = TemplateAliasFor(merged)
	return merged
}

// buildPythonTemplate names the merged template after the best evidence in the
// group, truncating a long header so the pattern stays a name rather than a
// paste of the whole traceback.
func buildPythonTemplate(header, exception string) string {
	switch {
	case header != "":
		truncated := header
		if len(truncated) > 120 {
			truncated = truncated[:117] + "..."
		}
		return "Python exception: " + truncated
	case exception != "":
		return "Python exception: " + exception
	default:
		return "Python traceback"
	}
}

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

// The Python traceback detection patterns.
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
