// SPDX-License-Identifier: Apache-2.0

package ghgraph

import (
	"fmt"
	"strings"
)

// summarize.go — the DETERMINISTIC one-line Summary every node carries, one
// registered function per declared resource type.
//
// WHAT THIS IS NOT: it is not input to an LLM summarizer and nothing here caps
// what a summarizer reads. A node's `content` — the resource's full detail — is
// carried whole and is what the entry's summarize_fields names. What is composed
// here is the node's own Summary FIELD: a short, stable, human-readable line that
// downstream keyword search matches on and that a reader sees beside the id. The
// byte cap below is on THAT line and on nothing else.
//
// WHY EVERY DECLARED TYPE HAS ONE. A type with no registered function falls
// through to the generic "<kind> <name>", which is the shape a node gets when
// nobody thought about it. Eleven types are declared and eleven are registered,
// and a test asserts the count and the fall-through both, because a materialized
// node class shipped with a generic summary would be the tell that it was added
// to the vocabulary and nowhere else.
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
	ResourceTypeOrganization: func(res Resource) string {
		return strings.TrimSpace("GitHub organization " + res.Name)
	},
	ResourceTypeRepository: func(res Resource) string {
		return join("GitHub repository", res.Name,
			pair("visibility", res.Metadata["visibility"]),
			pair("default", res.Metadata["default_branch"]))
	},
	ResourceTypeWorkflow: func(res Resource) string {
		return join("GitHub workflow", res.Name,
			pair("path", res.Metadata["path"]),
			parens(res.Metadata["repo"]))
	},
	ResourceTypeWorkflowRun: func(res Resource) string {
		return join("GitHub workflow run", res.Name,
			pair("status", res.Metadata["status"]),
			pair("conclusion", res.Metadata["conclusion"]),
			pair("event", res.Metadata["event"]),
			parens(res.Metadata["repo"]))
	},
	ResourceTypeRunner:      func(res Resource) string { return scoped("GitHub runner", res) },
	ResourceTypeEnvironment: func(res Resource) string { return scoped("GitHub environment", res) },
	ResourceTypeDeployment: func(res Resource) string {
		return join("GitHub deployment", res.Name,
			pair("env", res.Metadata["environment"]),
			pair("ref", res.Metadata["ref"]),
			parens(res.Metadata["repo"]))
	},
	ResourceTypeSecret: func(res Resource) string {
		return join("GitHub secret", res.Name,
			pair("scope", res.Metadata["scope"]),
			parens(res.Metadata["org"]))
	},

	// The three materialized classes.
	//
	// THE USER LINE NAMES NO ROLE. It said "reviewer user" while the only field
	// that minted one was a protection rule; a user node is now minted from a
	// run's actor, a run's triggering actor and a deployment's creator as well, so
	// a line calling every one of them a reviewer would be wrong about most of
	// them. WHICH role a person played is the edge that reaches them, and it is
	// not a property of the person.
	ResourceTypeUser: func(res Resource) string {
		return join("GitHub user", res.Name, parens(res.Metadata["org"]))
	},

	// The team and the label DO say what they are for, because each exists only
	// so an edge resolves and a reader meeting one in a search result would
	// otherwise have no idea why it is there.
	ResourceTypeTeam: func(res Resource) string {
		return join("GitHub reviewer team", res.Name, parens(res.Metadata["org"]))
	},
	ResourceTypeLabel: func(res Resource) string {
		return join("GitHub runner label", res.Name, parens(res.Metadata["org"]))
	},
}

// Summarize returns the deterministic Summary for a resource.
//
// The fall-through exists for a resource type nothing registered, and no
// declared type reaches it — [TestEveryDeclaredTypeHasItsOwnSummarizer] is what
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
// name, and the org or org/repo it lives under.
func scoped(prefix string, res Resource) string {
	org, repo := res.Metadata["org"], res.Metadata["repo"]
	switch {
	case org != "" && repo != "":
		return join(prefix, res.Name, fmt.Sprintf("(%s/%s)", org, repo))
	case org != "":
		return join(prefix, res.Name, fmt.Sprintf("(%s)", org))
	default:
		return join(prefix, res.Name)
	}
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
	return "(" + value + ")"
}

// truncate caps the display line at [summaryMaxLen] BYTES, matching the source
// provider's own len() semantics so the two produce the same string.
func truncate(s string) string {
	if len(s) <= summaryMaxLen {
		return s
	}
	return s[:summaryMaxLen]
}
