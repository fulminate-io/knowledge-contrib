// SPDX-License-Identifier: Apache-2.0

package bbgraph

import (
	"fmt"
	"strings"
)

// summarize.go — the one-line summary every node carries, one summarizer per
// declared resource type.
//
// NINE SUMMARIZERS FOR NINE RESOURCE TYPES IS A FLOOR, not a convenience. The
// source provider registers one per type in a provider-scoped registry, so a
// module that reproduced the resource-type and edge-type floors and skipped
// these would ship nodes with an empty summary — and summary is what the
// embedding and the search index are built from for this graph, by the behavior
// block the README's worked entry declares.
//
// THE DISPATCH REFUSES AN UNKNOWN TYPE rather than falling back to a generic
// line. [Build] has already refused an undeclared type by the time this runs, so
// the default arm is unreachable through the walk; it is written as a visible
// marker rather than a plausible sentence so a type added to the vocabulary
// without a summarizer is obvious in the graph instead of silently generic.
//
// ONE DEVIATION FROM THE SOURCE PROVIDER, AND IT IS A FIX. That provider's
// pipeline-run summarizer reads the metadata keys `state` and `result`, and its
// pipeline-run converter writes neither: it writes the flattened status under
// `status`. So every pipeline-run summary it produced silently omitted the run's
// outcome. This module's summarizer reads the key the converter actually writes.
func Summarize(res Resource) string {
	switch res.ResourceType {
	case ResourceTypeWorkspace:
		return generic("Bitbucket workspace", res)
	case ResourceTypeRepository:
		return generic("Bitbucket repository", res)
	case ResourceTypePipeline:
		return summarizePipeline(res)
	case ResourceTypePipelineRun:
		return summarizePipelineRun(res)
	case ResourceTypeRunner:
		return generic("Bitbucket runner", res)
	case ResourceTypeLabel:
		return generic("Bitbucket runner label", res)
	case ResourceTypeEnvironment:
		return generic("Bitbucket environment", res)
	case ResourceTypeApprovalGate:
		return generic("Bitbucket approval gate", res)
	case ResourceTypeVariable:
		return generic("Bitbucket variable", res)
	default:
		return fmt.Sprintf("Bitbucket resource of the undeclared type %q: %s",
			res.ResourceType, res.Name)
	}
}

// generic returns "<prefix> <name>" with a "(workspace/repo)" or "(workspace)"
// suffix from the metadata, which is the source provider's own shape.
func generic(prefix string, res Resource) string {
	parts := []string{prefix, res.Name}
	workspace := res.Metadata["workspace"]
	repo := res.Metadata["repo"]
	switch {
	case workspace != "" && repo != "":
		parts = append(parts, fmt.Sprintf("(%s/%s)", workspace, repo))
	case workspace != "":
		parts = append(parts, fmt.Sprintf("(%s)", workspace))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func summarizePipeline(res Resource) string {
	parts := []string{"Bitbucket pipeline", res.Name}
	if trigger := res.Metadata["trigger_key"]; trigger != "" {
		parts = append(parts, fmt.Sprintf("trigger=%s", trigger))
	}
	return strings.TrimSpace(strings.Join(parts, " ") + " " + scopeSuffix(res))
}

func summarizePipelineRun(res Resource) string {
	parts := []string{"Bitbucket pipeline run", res.Name}
	if status := res.Metadata["status"]; status != "" {
		parts = append(parts, fmt.Sprintf("status=%s", status))
	}
	if branch := res.Metadata["branch"]; branch != "" {
		parts = append(parts, fmt.Sprintf("branch=%s", branch))
	}
	return strings.TrimSpace(strings.Join(parts, " ") + " " + scopeSuffix(res))
}

// scopeSuffix is the "(workspace/repo)" or "(workspace)" tail, or empty.
func scopeSuffix(res Resource) string {
	workspace := res.Metadata["workspace"]
	if workspace == "" {
		return ""
	}
	if repo := res.Metadata["repo"]; repo != "" {
		return fmt.Sprintf("(%s/%s)", workspace, repo)
	}
	return fmt.Sprintf("(%s)", workspace)
}
