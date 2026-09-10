// SPDX-License-Identifier: Apache-2.0

package glgraph

import (
	"fmt"
	"strings"
)

// summarize.go — the DETERMINISTIC one-line Summary every node carries, one
// registered function per declared resource type.
//
// WHAT THIS IS NOT: it is not input to an LLM summarizer and nothing here caps
// what a summarizer reads. A node's `content` — the resource's detail — is
// carried whole and is what the entry's summarize_fields names. What is composed
// here is the node's own Summary FIELD: a short, stable, human-readable line that
// downstream keyword search matches on and that a reader sees beside the id. The
// byte cap below is on THAT line and on nothing else.
//
// WHY EVERY DECLARED TYPE HAS ONE. A type with no registered function falls
// through to the generic "<kind> <name>", which is the shape a node gets when
// nobody thought about it. Eleven types are declared and eleven are registered,
// and a test asserts the count and the fall-through both.
//
// SAME INPUT, SAME OUTPUT. No clock, no randomness, and everything read from
// Metadata rather than parsed back out of the marshaled Content.

// summaryMaxLen caps this deterministic display line. It is the cap the source
// provider's own Summary carried, reproduced so a node's summary is byte-identical
// to what a consumer already stored. It bounds no summarizer input: see the file
// comment.
const summaryMaxLen = 500

// summarizeFunc composes one resource type's summary line.
type summarizeFunc func(res Resource) string

// summarizers is the whole registry, one entry per declared resource type. It is
// a package-level map literal rather than a set of init() registrations: this
// module serves ONE provider, so the process-global registry and the duplicate-
// registration panic the source provider needed have nothing left to protect
// against, and a literal is a list a reader can count.
var summarizers = map[string]summarizeFunc{
	ResourceTypeGroup:   func(res Resource) string { return scoped("GitLab group", res) },
	ResourceTypeProject: func(res Resource) string { return scoped("GitLab project", res) },
	ResourceTypePipeline: func(res Resource) string {
		return scoped("GitLab pipeline", res)
	},
	ResourceTypePipelineRun: func(res Resource) string {
		return join("GitLab pipeline run", res.Name,
			pair("status", res.Metadata["status"]),
			pair("ref", res.Metadata["ref"]),
			parens(res.Metadata["project"]))
	},
	ResourceTypeJob:       func(res Resource) string { return scoped("GitLab job", res) },
	ResourceTypeRunner:    func(res Resource) string { return scoped("GitLab runner", res) },
	ResourceTypeRunnerTag: func(res Resource) string { return scoped("GitLab runner tag", res) },
	ResourceTypeEnvironment: func(res Resource) string {
		return scoped("GitLab environment", res)
	},
	ResourceTypeProtectionRule: func(res Resource) string {
		return join("GitLab protection rule", res.Name,
			pair("approvals", res.Metadata["required_approval_count"]),
			pair("env", res.Metadata["environment"]),
			parens(res.Metadata["project"]))
	},
	ResourceTypeDeployment: func(res Resource) string { return scoped("GitLab deployment", res) },
	ResourceTypeVariable:   summarizeVariable,
}

// summarizeVariable names a variable's KEY and its flags and never its value,
// which this collector does not read at all.
func summarizeVariable(res Resource) string {
	parts := []string{"GitLab variable", res.Name, pair("scope", res.Metadata["scope"])}
	if res.Metadata["protected"] == "true" {
		parts = append(parts, "protected")
	}
	if res.Metadata["masked"] == "true" {
		parts = append(parts, "masked")
	}
	parts = append(parts, ownerParens(res))
	return join(parts...)
}

// Summarize returns the deterministic Summary for a resource.
//
// The fall-through exists for a resource type nothing registered, and no
// declared type reaches it — TestEveryDeclaredTypeHasItsOwnSummarizer is what
// keeps that true. It substitutes a placeholder for an empty kind so downstream
// keyword search has something to match rather than a leading space.
func Summarize(res Resource) string {
	if fn, ok := summarizers[res.ResourceType]; ok {
		if s := fn(res); s != "" {
			return truncate(s)
		}
	}
	kind := res.ResourceType
	if kind == "" {
		kind = "<unknown>"
	}
	return truncate(strings.TrimSpace(kind + " " + res.Name))
}

// SummarizedTypes returns the resource types that carry a registered summarizer,
// so a test can compare that set against the declared vocabulary rather than
// counting registrations by eye.
func SummarizedTypes() []string {
	out := make([]string, 0, len(summarizers))
	for rt := range summarizers {
		out = append(out, rt)
	}
	return out
}

// scoped is the shape a resource with no detail worth naming gets: the kind, the
// name, and the project or group it lives under. The project is preferred over
// the group when a resource carries both, which is the source provider's own
// order.
func scoped(prefix string, res Resource) string {
	return join(prefix, res.Name, ownerParens(res))
}

// ownerParens renders the project that owns a resource, or the group when there
// is no project.
func ownerParens(res Resource) string {
	if project := res.Metadata["project"]; project != "" {
		return parens(project)
	}
	return parens(res.Metadata["group"])
}

// join assembles a summary from its parts, dropping the empty ones so an absent
// metadata value leaves no double space and no dangling key.
func join(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}

// pair renders `key=value`, or nothing when the value is absent.
func pair(key, value string) string {
	if value == "" {
		return ""
	}
	return key + "=" + value
}

// parens renders `(value)`, or nothing when the value is absent.
func parens(value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("(%s)", value)
}

// truncate caps the display line at [summaryMaxLen] BYTES, matching the source
// provider's own len() semantics so the two produce the same string.
func truncate(s string) string {
	if len(s) <= summaryMaxLen {
		return s
	}
	return s[:summaryMaxLen]
}
