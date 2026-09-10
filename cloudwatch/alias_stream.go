// SPDX-License-Identifier: Apache-2.0

package main

import (
	"sort"
	"strings"
)

// alias_stream.go — THE STREAM ALIAS DERIVER, the second of this package's two
// derivers. It takes a LABEL SET through a five-arm provider dispatch and
// PRESERVES CASE, where the template deriver lowercases every token.
//
// WHY A CLOUDWATCH COLLECTOR CARRIES ALL FIVE ARMS. The dispatch is part of the
// vocabulary being reproduced, and it is ORDER-SENSITIVE, so the arms this
// collector does not normally take are exactly what makes a misroute
// observable: a CloudWatch collector that lifted a `reason` field out of a JSON
// body into a label — an ordinary thing to do, and this collector's own
// normalization already lifts level, severity, lvl and log_level out of JSON
// bodies — would take the KUBERNETES arm and rename every stream, with no error
// anywhere. Dropping the unreached arms would delete the evidence of that.

// provider enumerates the label shapes the dispatch recognizes. The zero value
// is the generic arm, so an unrecognized shape falls through rather than
// failing.
type provider int

const (
	providerGeneric provider = iota
	providerK8s
	providerLoki
	providerStackdriver
	providerCloudWatch
)

// AliasFor derives a stream's readable alias from its label set.
//
// It reads Labels, FALLING BACK to LowCardLabels when Labels is empty, and
// returns "" when both are empty. The fallback matters for a stream
// reconstructed from its low-card labels alone.
func AliasFor(stream *LogStream) string {
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
	switch inferProvider(labels) {
	case providerK8s:
		return aliasK8s(labels)
	case providerLoki:
		return aliasLoki(labels)
	case providerStackdriver:
		return aliasStackdriver(labels)
	case providerCloudWatch:
		return aliasCloudWatch(labels)
	default:
		return aliasGeneric(labels)
	}
}

// inferProvider classifies a label set by which distinctive key it carries.
//
// THE ORDER OF THE CHECKS IS THE RULE, because the shapes overlap: `reason` is
// the most specific discriminator and wins even when the CloudWatch keys are
// also present. Reordering the first two checks reclassifies every stream that
// carries both.
//
// Note that the CloudWatch arm tests the VALUES: a log_group present with an
// empty value does not select it, and such a stream falls through to
// Stackdriver, then Loki, then generic.
func inferProvider(labels map[string]string) provider {
	if labels == nil {
		return providerGeneric
	}
	if labels["reason"] != "" {
		return providerK8s
	}
	if labels["log_stream"] != "" || labels["log_group"] != "" {
		return providerCloudWatch
	}
	if labels["resource_type"] != "" {
		return providerStackdriver
	}
	if labels["app"] != "" {
		return providerLoki
	}
	return providerGeneric
}

// aliasK8s derives "<object>.<reason>", preferring the most specific object
// label available.
func aliasK8s(labels map[string]string) string {
	return joinComponents(
		firstNonEmpty(labels, "pod_name", "service", "related_name", "kind"),
		labels["reason"], ".")
}

// aliasLoki derives "<app>@<instance>". Job is deliberately dropped: in
// practice app and job are redundant and app is the one humans use.
func aliasLoki(labels map[string]string) string {
	return joinComponents(labels["app"], firstNonEmpty(labels, "instance", "host", "pod", "pod_name"), "@")
}

// aliasStackdriver derives "<host>.<resource_type>". Severity is not used:
// Stackdriver streams group by resource, so the resource type is the clearer
// suffix.
func aliasStackdriver(labels map[string]string) string {
	return joinComponents(firstNonEmpty(labels, "host", "pod_name", "instance_id", "service"), labels["resource_type"], ".")
}

// aliasCloudWatch derives "<log_stream>.<service>", the arm this collector's
// own streams take.
//
// THE TWO COMPONENTS HAVE DIFFERENT RULES. The first is labels["log_stream"]
// with NO fallback — that key or nothing. The second is service, falling back
// to log_group only when service is absent OR EMPTY-VALUED, because
// firstNonEmpty tests the value.
func aliasCloudWatch(labels map[string]string) string {
	return joinComponents(labels["log_stream"], firstNonEmpty(labels, "service", "log_group"), ".")
}

// joinComponents applies the two-component branch rule every dispatch arm above
// shares: an empty FIRST component yields the sanitized second alone with no
// separator, an empty SECOND yields the sanitized first alone, and two present
// components are joined by sep.
//
// THE BRANCHES TEST THE RAW VALUES, NOT THE SANITIZED ONES, and that is
// behavior rather than an oversight. A component that is non-empty but
// sanitizes to empty still takes the both-present branch and emits a DANGLING
// SEPARATOR: a log_stream of "..." beside a service of "api" yields ".api". A
// reimplementation that guards on sanitize(x) == "" instead produces a
// different SymbolName for those streams and raises no error.
func joinComponents(first, second, sep string) string {
	if first == "" {
		return sanitize(second)
	}
	if second == "" {
		return sanitize(first)
	}
	return sanitize(first) + sep + sanitize(second)
}

// aliasGeneric derives "<k1>=<v1>.<k2>=<v2>" from the first two labels by
// sorted key, skipping empty values. Sorting is what makes it deterministic
// across runs, since map iteration is not.
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
