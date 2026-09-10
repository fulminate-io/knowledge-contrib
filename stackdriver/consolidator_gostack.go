// SPDX-License-Identifier: Apache-2.0

package main

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// consolidator_gostack.go — the Go goroutine-dump fold.
//
// A Go panic writes one log line per stack frame, so a single crash arrives as
// dozens of templates that mean one thing. This pass groups the fragments by
// temporal proximity and replaces each group of three or more with one CRITICAL
// template, recording which fragments it took.

// goStackWindow is how far apart two fragments may sit and still belong to the
// same crash. A goroutine dump is written in one burst, so the window is about
// bounding a burst rather than about correlating separate crashes.
const goStackWindow = 30 * time.Second

// goStackMinGroup is the group size that constitutes a dump. Two stack-shaped
// lines are ordinary application output; three in a burst are a crash.
const goStackMinGroup = 3

// goStackMergedPattern is the pattern the merged template carries, and it is
// FIXED text rather than derived from the group. Two crashes therefore produce
// two merged templates with the same pattern and so the same id, which
// pipeline.go folds into one template covering both windows — the chunk ids
// keep the two bursts apart, because a chunk folds the window and the template
// does not.
const goStackMergedPattern = "Go runtime crash (goroutine dump)"

type goStackConsolidator struct{}

func (g *goStackConsolidator) Name() string { return "go_stack" }

// Consolidate partitions on fragment shape, groups the fragments by proximity,
// and merges the groups that are large enough.
//
// THE EARLY RETURN IS A REAL ARM, not a guard. Below the minimum the input is
// returned UNCHANGED and nothing is absorbed, so a service that logs the odd
// stack-shaped line keeps its own templates rather than having them rewritten
// into a crash that did not happen.
func (g *goStackConsolidator) Consolidate(templates []*logTemplate) consolidation {
	var fragments, normal []*logTemplate
	for _, t := range templates {
		if isGoStackFragment(t) {
			fragments = append(fragments, t)
		} else {
			normal = append(normal, t)
		}
	}
	if len(fragments) < goStackMinGroup {
		return consolidation{Templates: templates}
	}

	sortTemplatesByLastSeen(fragments)
	absorbed := make(map[string][]string)
	for _, group := range groupByTime(fragments, goStackWindow) {
		if len(group) < goStackMinGroup {
			normal = append(normal, group...)
			continue
		}
		merged := mergeGoStackGroup(group)
		normal = append(normal, merged)
		absorbed[merged.ID] = append(absorbed[merged.ID], templateIDs(group)...)
	}
	return consolidation{Templates: normal, Absorbed: absorbed}
}

// mergeGoStackGroup builds the one template that replaces a group.
//
// IT COMPUTES AN ID, on the same rule as every other template in this module:
// the hash of its pattern. The knowledge client's own merge leaves the id EMPTY
// and nothing downstream fills it, so its merged template lands under whatever
// id the store generates and cannot be reconciled against the same crash in a
// later collect. A collector whose second collect must diff against its first
// cannot inherit that.
func mergeGoStackGroup(group []*logTemplate) *logTemplate {
	merged := &logTemplate{
		Pattern:   goStackMergedPattern,
		Severity:  severityCritical,
		FirstSeen: group[0].FirstSeen,
		LastSeen:  group[0].LastSeen,
	}
	for _, t := range group {
		merged.Count += t.Count
		widenTemplateRange(merged, t)
		if severityIndex(t.Severity) > severityIndex(merged.Severity) {
			merged.Severity = t.Severity
		}
		if len(merged.ExampleVars) < maxExampleVars &&
			(reGoStack.MatchString(t.Pattern) || reExceptionHeader.MatchString(t.Pattern)) {
			ex := t.Pattern
			if len(t.ExampleVars) > 0 {
				ex = strings.Join(t.ExampleVars[0], " ")
			}
			merged.ExampleVars = append(merged.ExampleVars, []string{ex})
		}
	}
	if len(merged.ExampleVars) == 0 {
		if len(group[0].ExampleVars) > 0 {
			merged.ExampleVars = [][]string{group[0].ExampleVars[0]}
		} else {
			merged.ExampleVars = [][]string{{group[0].Pattern}}
		}
	}
	merged.ID = templateID(merged.Pattern)
	merged.Alias = templateAliasFor(merged)
	return merged
}

// widenTemplateRange widens dst's window to cover src's.
func widenTemplateRange(dst, src *logTemplate) {
	if src.FirstSeen.Before(dst.FirstSeen) {
		dst.FirstSeen = src.FirstSeen
	}
	if src.LastSeen.After(dst.LastSeen) {
		dst.LastSeen = src.LastSeen
	}
}

// sortTemplatesByLastSeen orders templates ascending by LastSeen, breaking ties
// on ID so the order is TOTAL. The tie-break is what makes grouping reproducible
// on a burst whose fragments share a timestamp, which is the ordinary case for
// a dump written in one write.
func sortTemplatesByLastSeen(templates []*logTemplate) {
	sort.Slice(templates, func(i, j int) bool {
		if !templates[i].LastSeen.Equal(templates[j].LastSeen) {
			return templates[i].LastSeen.Before(templates[j].LastSeen)
		}
		return templates[i].ID < templates[j].ID
	})
}

// templateIDs projects a group to its ids.
func templateIDs(group []*logTemplate) []string {
	out := make([]string, 0, len(group))
	for _, t := range group {
		out = append(out, t.ID)
	}
	return out
}

// groupByTime splits a time-sorted slice wherever the gap to the running
// group's end exceeds window.
func groupByTime(sorted []*logTemplate, window time.Duration) [][]*logTemplate {
	if len(sorted) == 0 {
		return nil
	}
	var groups [][]*logTemplate
	current := []*logTemplate{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		prevEnd := current[len(current)-1].LastSeen
		if sorted[i].FirstSeen.Sub(prevEnd) <= window {
			current = append(current, sorted[i])
			continue
		}
		groups = append(groups, current)
		current = []*logTemplate{sorted[i]}
	}
	return append(groups, current)
}

// Go stack-frame shapes.
var (
	reGoFuncCall     = regexp.MustCompile(`^[\w.*/()\-:]+\.\w+\(`)
	reGoStackArgs    = regexp.MustCompile(`\((0x[0-9a-fA-F]|\.\.\.|\{\}|\)|\{0x|[0-9a-fA-F]+\?|0x)`)
	reGoSourceRef    = regexp.MustCompile(`\.go:\d+`)
	reGoSSourceRef   = regexp.MustCompile(`\.s:\d+`)
	reGoPCOffset     = regexp.MustCompile(`\+0x[0-9a-fA-F]+`)
	reGoRegisterDump = regexp.MustCompile(
		`^(rax|rbx|rcx|rdx|rsi|rdi|rbp|rsp|r[89]|r1[0-5]|eax|ebx|ecx|edx|` +
			`gs|fs|cs|rflags|eflags|rip|eip)\s+0x`)
	reGoCreatedBy     = regexp.MustCompile(`^created by\s+`)
	reGoSignalInfo    = regexp.MustCompile(`^(PC=0x|SIG[A-Z]+[: ]|signal )`)
	reGoAutogenerated = regexp.MustCompile(`^<autogenerated>:\d+`)
)

// goStackProseLimit and goStackLineLimit bound how long a line may be and still
// read as a stack frame. A frame is short; a long line carrying a `.go:` is
// prose that mentions a file.
const (
	goStackProseLimit = 300
	goStackLineLimit  = 200
)

func isGoStackFragment(t *logTemplate) bool {
	return matchesGoStackPattern(t.Pattern) ||
		exampleVarsContain(t.ExampleVars, matchesGoStackPattern)
}

// exampleVarsContain reports whether any recorded variable row matches fn. A
// template whose PATTERN was masked into wildcards can still be a stack frame,
// and its example values are where the evidence survives.
func exampleVarsContain(vars [][]string, fn func(string) bool) bool {
	return slices.ContainsFunc(vars, func(row []string) bool { return fn(strings.Join(row, " ")) })
}

// normalizeStackLine strips the leading indentation and quoting a log shipper
// may have added, so an indented frame still matches the anchored patterns.
func normalizeStackLine(msg string) string {
	msg = strings.TrimLeft(msg, " \t")
	msg = strings.TrimPrefix(msg, ">")
	return strings.TrimLeft(msg, " \t")
}

// matchesGoStackPattern is the fragment test. The four REJECTIONS run first and
// they are the reason this pass does not eat ordinary output: structured JSON,
// a timestamped line, a logfmt line and long prose are all shapes that would
// otherwise reach the permissive function-call test below.
func matchesGoStackPattern(msg string) bool {
	norm := normalizeStackLine(msg)
	if norm == "" {
		return false
	}
	if norm[0] == '{' || norm[0] == '[' {
		return false
	}
	if len(norm) > 4 && norm[0] >= '0' && norm[0] <= '9' && norm[4] == '-' {
		return false
	}
	if strings.HasPrefix(norm, "time=") || strings.HasPrefix(norm, "ts=") {
		return false
	}
	if len(norm) > goStackProseLimit && !reGoSourceRef.MatchString(norm) && !reGoPCOffset.MatchString(norm) {
		return false
	}
	return matchesHighConfidence(norm) || matchesSourceRef(norm) || matchesFuncCall(norm)
}

// matchesHighConfidence covers the shapes only a Go runtime writes.
func matchesHighConfidence(norm string) bool {
	return reGoStack.MatchString(norm) ||
		reGoRegisterDump.MatchString(norm) ||
		reGoCreatedBy.MatchString(norm) ||
		reGoSignalInfo.MatchString(norm) ||
		reGoAutogenerated.MatchString(norm)
}

// matchesSourceRef covers a source reference that also looks like a frame: a
// program-counter offset, or an absolute path. A bare `.go:12` in prose has
// neither.
func matchesSourceRef(norm string) bool {
	if reGoSourceRef.MatchString(norm) && len(norm) < goStackLineLimit && looksLikeFrameTail(norm) {
		return true
	}
	return reGoSSourceRef.MatchString(norm) && len(norm) < goStackLineLimit && looksLikeFrameTail(norm)
}

// looksLikeFrameTail reports the two marks a source-reference line carries when
// it came from a stack rather than from prose.
func looksLikeFrameTail(norm string) bool {
	if reGoPCOffset.MatchString(norm) {
		return true
	}
	trimmed := strings.TrimLeft(norm, " \t")
	return trimmed != "" && trimmed[0] == '/'
}

// matchesFuncCall covers a qualified call with stack-shaped arguments.
func matchesFuncCall(norm string) bool {
	return reGoFuncCall.MatchString(norm) && len(norm) < goStackProseLimit && reGoStackArgs.MatchString(norm)
}
