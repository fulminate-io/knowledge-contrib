// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"regexp"
	"strings"
)

// workflow_parser.go — the two things this collector reads out of a workflow's
// text: the secrets it names and the environments it deploys to.
//
// WHY REGEX AND LINE SCANNING RATHER THAN A YAML DECODE. A workflow definition
// is YAML with a highly variable shape — matrix strategies, reusable workflows,
// composite actions, expressions in almost any position — so a schema that
// decoded one would either be large and brittle or would refuse real files. Both
// readers below want ONE token each, and both are written to under-report rather
// than to guess: a form they do not recognize contributes no edge, which is a
// graph that is short rather than one that asserts a relationship the file does
// not state.
//
// BOTH ARE CARRIED FORWARD VERBATIM from the source provider, recognized forms
// and unrecognized ones alike, because the edges they produce are what a consumer
// already reads. Their measured behavior on every form is pinned by tests in this
// package, including the two forms they do NOT recognize.

// secretRefPattern matches a `${{ secrets.NAME }}` reference. The name is the
// provider's own vocabulary for a secret: upper-case, digits and underscores,
// never starting with a digit.
var secretRefPattern = regexp.MustCompile(`\$\{\{\s*secrets\.([A-Z_][A-Z0-9_]*)\s*\}\}`)

// ParseSecretRefs returns the DISTINCT secret names a workflow definition
// references, in first-appearance order.
//
// ORDER IS FIRST APPEARANCE RATHER THAN SORTED because it is an input to edge
// construction and the graph builder sorts everything it emits; a second sort
// here would decide nothing and hide that.
func ParseSecretRefs(definition string) []string {
	matches := secretRefPattern.FindAllStringSubmatch(definition, -1)
	seen := make(map[string]bool, len(matches))
	var names []string
	for _, match := range matches {
		name := match[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// environmentKey is the key a job names its deployment environment under.
const environmentKey = "environment:"

// ParseEnvironmentRefs returns the DISTINCT environment names a workflow
// definition deploys to, in first-appearance order.
//
// IT READS THE INLINE FORM ONLY: `environment: production`, quoted or not. Two
// other forms are deliberately not read, and each is pinned by a test so the
// boundary is a measured fact rather than a claim:
//
//   - THE BLOCK FORM, `environment:` followed by an indented `name: production`,
//     is NOT read. Recognizing it means tracking indentation across lines, and a
//     `name:` key appears under half a dozen unrelated blocks in a workflow — so
//     the version that guessed would have emitted a deploy edge to every step
//     name in the file.
//   - THE FLOW-MAPPING FORM, `environment: {name: production}`, is NOT read: the
//     guard below drops a value that opens a mapping rather than reading a name
//     out of it.
//
// AN EXPRESSION VALUE IS READ AS WRITTEN. `environment: ${{ inputs.target }}`
// yields the environment name `${{ inputs.target }}`, which resolves to no
// environment node — the value is decided when the workflow RUNS and this
// collector reads the file, not a run. That is the source provider's behavior
// carried forward: the alternative is dropping the edge, which changes the graph
// a consumer already reads, and the alternative to THAT is evaluating the
// expression, which this collector cannot do.
func ParseEnvironmentRefs(definition string) []string {
	seen := make(map[string]bool)
	var envs []string
	for line := range strings.SplitSeq(definition, "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), environmentKey)
		if !ok {
			continue
		}
		// THE MAPPING GUARD IS APPLIED BEFORE THE QUOTES ARE STRIPPED, in that
		// order, because that is the order the source provider applies them and
		// the two orders disagree on one real input: a quoted mapping,
		// `environment: "{name: production}"`, is a NAME to the first order and a
		// skipped mapping to the second.
		value = strings.TrimSpace(value)
		if value == "" || strings.HasPrefix(value, "{") {
			continue
		}
		name := strings.Trim(value, `"'`)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		envs = append(envs, name)
	}
	return envs
}
