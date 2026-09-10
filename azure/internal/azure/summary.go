// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"fmt"
	"strings"
)

// summary.go — THE DETERMINISTIC ONE-LINE SUMMARY every node carries.
//
// WHY IT IS NOT OPTIONAL. The summary is what search matches and what the
// embedding is computed from, so a node without one is a node that answers no
// query but its own id. WHY IT IS DETERMINISTIC: the same resource summarized
// twice must produce the same string, or every collect would look like a
// changed subscription to the writer.
//
// EVERY ARM RESOURCE TYPE THIS COLLECTOR EMITS HAS AN EXACT-STRING ENTRY in
// [summarizers] — no prefix matching and no wildcard, so adding a resource type
// without adding its summary is a visible gap rather than a silently generic
// node. The SYNTHETIC proxy types deliberately have none: they fall through to
// [genericSummary], which is the honest shape for a node whose only facts are
// its type and its name.

// summarize renders one resource's summary line.
func summarize(r resource) string {
	if fn, ok := summarizers[r.resourceType]; ok {
		return fn(r)
	}
	return genericSummary(r.resourceType, r)
}

// summaryFunc renders one resource type's summary.
type summaryFunc func(resource) string

// genericSummary is "<what> <name> in <region>", with each absent part
// dropped rather than rendered empty.
func genericSummary(what string, r resource) string {
	parts := []string{what, r.name}
	if r.region != "" {
		parts = append(parts, "in "+r.region)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// withMeta renders the generic summary followed by the named metadata values,
// each as "key=value", skipping any the resource does not carry. The key order
// is the CALLER'S, not the map's, so the output is deterministic.
func withMeta(what string, r resource, keys ...string) string {
	out := genericSummary(what, r)
	var extras []string
	for _, k := range keys {
		if v := r.metadata[k]; v != "" {
			extras = append(extras, fmt.Sprintf("%s=%s", k, v))
		}
	}
	if len(extras) == 0 {
		return out
	}
	return out + " (" + strings.Join(extras, ", ") + ")"
}

// simple returns a summarizer that renders the generic form with a fixed
// human-readable noun.
func simple(what string) summaryFunc {
	return func(r resource) string { return genericSummary(what, r) }
}

// detailed returns a summarizer that renders the generic form plus the named
// metadata keys, in the order given.
func detailed(what string, keys ...string) summaryFunc {
	return func(r resource) string { return withMeta(what, r, keys...) }
}
