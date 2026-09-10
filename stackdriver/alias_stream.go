// SPDX-License-Identifier: Apache-2.0

package main

import (
	"sort"
	"strings"
)

// alias_stream.go — the STREAM ALIAS, which is a dispatch on label KEYS and not
// a Stackdriver-specific format.
//
// THIS IS THE RULE A GREENFIELD MODULE GETS WRONG BY WRITING THE OBVIOUS THING.
// A Cloud Logging stream looks like it should be named `<host>.<resource_type>`
// and usually is — but the deriver picks its shape from which discriminating
// key the label set carries, and a Cloud Logging entry carries OPERATOR-CHOSEN
// entry labels that normalize.go copies in verbatim. An exported Kubernetes
// event carries `reason`, which selects the Kubernetes shape ahead of the
// Stackdriver one; an entry carrying `log_stream` or `log_group` selects the
// CloudWatch shape. Writing the obvious format unconditionally passes every
// single-shape check and still names those streams differently from the reading
// layer, and no id test can see it because the alias enters no id.

// streamShape enumerates the label shapes the deriver recognizes. The zero
// value is the generic one, so an unrecognized set falls through to it.
type streamShape int

const (
	shapeGeneric streamShape = iota
	shapeK8s
	shapeLoki
	shapeStackdriver
	shapeCloudWatch
)

// aliasForStream derives a stream's readable identifier.
//
// It falls back from Labels to LowCardLabels because a stream whose every label
// was classified high-cardinality still has a label set to be named from; both
// empty is the one case that yields the empty string, and the reading layer
// substitutes a hash-derived locator there.
func aliasForStream(stream *logStream) string {
	if stream == nil {
		return ""
	}
	labels := stream.Labels
	if len(labels) == 0 {
		labels = stream.LowCardLabels
	}
	if len(labels) == 0 {
		return ""
	}
	switch inferShape(labels) {
	case shapeK8s:
		return aliasK8s(labels)
	case shapeLoki:
		return aliasLoki(labels)
	case shapeStackdriver:
		return aliasStackdriver(labels)
	case shapeCloudWatch:
		return aliasCloudWatch(labels)
	default:
		return aliasGeneric(labels)
	}
}

// inferShape classifies a label set by its discriminating keys, in a FIXED
// ORDER, returning on the first hit.
//
// THE ORDER IS THE RULE AND IT IS NOT ALPHABETICAL OR ARBITRARY: the shapes
// overlap, because a Kubernetes workload running on GKE carries both `reason`
// and `resource_type`, and the most specific discriminator has to win. A
// Kubernetes event requires `reason`, so it is tested first.
//
// EACH TEST IS FOR A NON-EMPTY VALUE, NOT FOR PRESENCE. A key present with an
// empty value does not select its shape. That distinction is unreachable from a
// collected entry, because normalize.go skips empty-valued entry labels before
// they ever reach a stream, but it is reachable by calling this function
// directly and it is where the rule is stated.
func inferShape(labels map[string]string) streamShape {
	if labels == nil {
		return shapeGeneric
	}
	if labels["reason"] != "" {
		return shapeK8s
	}
	if labels["log_stream"] != "" || labels["log_group"] != "" {
		return shapeCloudWatch
	}
	if labels["resource_type"] != "" {
		return shapeStackdriver
	}
	if labels["app"] != "" {
		return shapeLoki
	}
	return shapeGeneric
}

// aliasK8s derives `<object>.<reason>`, the reason preserved verbatim.
func aliasK8s(labels map[string]string) string {
	object := firstNonEmpty(labels, "pod_name", "service", "related_name", "kind")
	return joinAliasComponents(sanitize(object), sanitize(labels["reason"]), ".")
}

// aliasLoki derives `<app>@<instance>`. The separator is `@` and not `.`, which
// is the shape's own convention.
func aliasLoki(labels map[string]string) string {
	instance := firstNonEmpty(labels, "instance", "host", "pod", "pod_name")
	return joinAliasComponents(sanitize(labels["app"]), sanitize(instance), "@")
}

// aliasStackdriver derives `<host>.<resource_type>`, where the host falls back
// through the identifiers a Cloud Logging entry may carry instead of an
// explicit host: the pod name on GKE, the instance id on GCE, the service name
// last.
func aliasStackdriver(labels map[string]string) string {
	host := firstNonEmpty(labels, "host", "pod_name", "instance_id", "service")
	return joinAliasComponents(sanitize(host), sanitize(labels["resource_type"]), ".")
}

// aliasCloudWatch derives `<log_stream>.<service>`.
//
// NOTE THAT log_group HAS TWO DIFFERENT ROLES HERE and they are easy to
// conflate: it is a TRIGGER for this shape in inferShape, and it is the
// FALLBACK for the service component. An entry carrying a log group and no log
// stream therefore selects this shape and then derives a single bare component.
func aliasCloudWatch(labels map[string]string) string {
	svc := firstNonEmpty(labels, "service", "log_group")
	return joinAliasComponents(sanitize(labels["log_stream"]), sanitize(svc), ".")
}

// aliasGeneric derives `<k1>=<v1>.<k2>=<v2>` from the first two keys
// alphabetically, SKIPPING keys whose value is empty — so a label set that is
// non-empty but all-empty-valued still derives the empty alias, which is the
// second member of the empty-alias class.
func aliasGeneric(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k, v := range labels {
		if v == "" {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	parts := make([]string, 0, 2)
	for i := 0; i < len(keys) && i < 2; i++ {
		parts = append(parts, sanitize(keys[i])+"="+sanitize(labels[keys[i]]))
	}
	return strings.Join(parts, ".")
}

// joinAliasComponents joins two components with sep, emitting a single bare
// component when either is empty rather than a leading or trailing separator.
func joinAliasComponents(first, second, sep string) string {
	switch {
	case first == "":
		return second
	case second == "":
		return first
	default:
		return first + sep + second
	}
}
