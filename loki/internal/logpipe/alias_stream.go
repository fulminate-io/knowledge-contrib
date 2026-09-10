// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"sort"
	"strings"
)

// alias_stream.go — the STREAM alias deriver.
//
// THE DISPATCH IS THE TRAP, AND SO IS EACH ARM'S COLLAPSE. The shapes below
// read like a two-component join, and an implementation that writes them that
// way emits an alias beginning or ending with a bare separator on every stream
// whose second component is absent. Every arm is a FALLBACK CHAIN over label
// VALUES, and every arm collapses to its single present component.
//
//   - K8s:         <object>.<reason>            e.g. api-7b6.OOMKilled
//   - Loki:        <app>@<instance>             e.g. checkout@host-3
//   - Stackdriver: <host>.<resource_type>       e.g. api-7b6.k8s_container
//   - CloudWatch:  <log_stream>.<service>       e.g. ecs-task.api-server
//   - Generic:     <key1>=<val1>.<key2>=<val2>  first two labels by key
//
// ALL FOUR FOREIGN ARMS ARE REACHABLE FROM A LOKI COLLECT. Every Loki stream
// label is copied onto each entry verbatim, and the label set is whoever
// configured the shipper's: a Loki instance scraping Kubernetes events carries
// `reason`, one fed by a CloudWatch forwarder carries `log_group`, and a stream
// labeled job/namespace/container with no `app` never reaches the Loki arm at
// all. Dispatching unconditionally to the Loki shape is the defect this comment
// exists to prevent.

// AliasFor derives a stream's readable name. It returns the empty string only
// when the stream carries no labels at all under either label set.
func AliasFor(stream *Stream) string {
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

// provider enumerates the recognized label shapes. The zero value is the
// generic fallback, so an unrecognized set never dispatches to a named arm.
type provider int

const (
	providerGeneric provider = iota
	providerK8s
	providerLoki
	providerStackdriver
	providerCloudWatch
)

// inferProvider classifies a label set by its discriminating keys. THE ORDER IS
// LOAD-BEARING because the sets overlap: a Kubernetes event forwarded through
// Stackdriver carries both `reason` and `resource_type`, and `reason` is the
// more specific claim so it wins. Reordering these checks re-aliases streams
// silently.
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

// aliasK8s is <object>.<reason>, collapsing to the reason alone when the label
// set carries none of the four object keys.
func aliasK8s(labels map[string]string) string {
	object := firstNonEmpty(labels, "pod_name", "service", "related_name", "kind")
	reason := labels["reason"]
	if object == "" {
		return sanitize(reason)
	}
	if reason == "" {
		return sanitize(object)
	}
	return sanitize(object) + "." + sanitize(reason)
}

// aliasLoki is <app>@<instance>, collapsing to the bare app when no instance
// key carries a value. Emitting "<app>@" there is the defect.
func aliasLoki(labels map[string]string) string {
	app := labels["app"]
	instance := firstNonEmpty(labels, "instance", "host", "pod", "pod_name")
	if app == "" {
		return sanitize(instance)
	}
	if instance == "" {
		return sanitize(app)
	}
	return sanitize(app) + "@" + sanitize(instance)
}

// aliasStackdriver is <host>.<resource_type>, collapsing to the resource type
// when no host key carries a value.
func aliasStackdriver(labels map[string]string) string {
	host := firstNonEmpty(labels, "host", "pod_name", "instance_id", "service")
	rt := labels["resource_type"]
	if host == "" {
		return sanitize(rt)
	}
	if rt == "" {
		return sanitize(host)
	}
	return sanitize(host) + "." + sanitize(rt)
}

// aliasCloudWatch is <log_stream>.<service|log_group>. BOTH of its collapses
// are reachable, unlike the other arms': the dispatch admits this arm on either
// discriminator, so a set carrying only log_group and one carrying only
// log_stream both land here.
func aliasCloudWatch(labels map[string]string) string {
	stream := labels["log_stream"]
	svc := firstNonEmpty(labels, "service", "log_group")
	if stream == "" {
		return sanitize(svc)
	}
	if svc == "" {
		return sanitize(stream)
	}
	return sanitize(stream) + "." + sanitize(svc)
}

// aliasGeneric joins the first two labels by key AMONG THOSE WITH A NON-EMPTY
// VALUE — empty-valued labels are dropped BEFORE the sort, so they cannot take
// a slot.
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
