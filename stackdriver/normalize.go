// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/logging"
	"google.golang.org/protobuf/types/known/structpb"
)

// normalize.go — one Cloud Logging entry to one normalized entry, in four
// stages: payload to message, GCP severity to canonical severity with the GKE
// downward reclassification, monitored-resource labels to the label map, and
// entry labels on top of it.
//
// THE LABEL MAP IS NOT COSMETIC. Every key in it enters the stream fingerprint,
// which is the stream's id, which is what makes a second collect over the same
// window reconcile against the first instead of duplicating it. A key added, a
// key dropped, or an empty-valued key admitted all move every stream id in the
// graph. That is why the two rules below that look like details — the
// empty-value skip and the two unconditional provenance labels — carry their
// own tests.

// initialLabelCapacity is the label count a normalized entry starts with: the
// eight resource-derived keys this file may set plus the log name.
const initialLabelCapacity = 9

// normalizeEntry converts one Cloud Logging entry into this module's entry
// shape. projectID is the collect's own project, not the entry's.
func normalizeEntry(entry *logging.Entry, projectID string) logEntry {
	le := logEntry{
		Timestamp: entry.Timestamp,
		Severity:  mapGCPSeverity(entry.Severity),
		Message:   extractMessage(entry),
		Labels:    make(map[string]string, initialLabelCapacity),
	}

	// THE GKE RECLASSIFICATION, AND IT IS DOWNWARD ONLY. The container runtime
	// tags every line a container wrote to stderr as ERROR, so an application's
	// own INFO lines arrive as errors and drown the ones that are real. When the
	// message body declares its own level, that level wins — but only when it is
	// LOWER than what the wrapper claimed. Upgrading here would let any line
	// containing the word "error" promote itself, which is the failure mode this
	// direction exists to avoid.
	if embedded := detectEmbeddedSeverity(le.Message); embedded != "" {
		if !severityAtLeast(embedded, le.Severity) {
			le.Severity = embedded
		}
	}

	populateResourceLabels(&le, entry)
	populateEntryLabels(&le, entry)

	// THE TWO PROVENANCE LABELS ARE UNCONDITIONAL AND THEY RUN LAST. project_id
	// is the COLLECT's project and overwrites whatever the resource labels
	// carried, because an entry read out of project A is provenance-tagged A
	// however its resource is labeled. Both are inputs to the stream id, and
	// both are stable across collects over the same entries, which is what makes
	// the same stream reconcile.
	le.Labels["project_id"] = projectID
	if entry.LogName != "" {
		le.Labels["log_name"] = extractLogID(entry.LogName)
	}

	return le
}

// populateResourceLabels copies the monitored-resource labels into the label
// map. The service and host selections are FIRST-MATCH-WINS chains rather than
// last-write-wins assignments, so a GKE container label is not overwritten by a
// Cloud Run one on an entry that carries both.
func populateResourceLabels(le *logEntry, entry *logging.Entry) {
	if entry.Resource == nil || entry.Resource.Labels == nil {
		return
	}
	labels := entry.Resource.Labels

	// Service: the container name (GKE), then the service name (Cloud Run),
	// then the module id (App Engine).
	switch {
	case labels["container_name"] != "":
		le.Labels["service"] = labels["container_name"]
	case labels["service_name"] != "":
		le.Labels["service"] = labels["service_name"]
	case labels["module_id"] != "":
		le.Labels["service"] = labels["module_id"]
	}

	// Host: the instance id (GCE, Cloud Run), then the pod name (GKE).
	switch {
	case labels["instance_id"] != "":
		le.Labels["host"] = labels["instance_id"]
	case labels["pod_name"] != "":
		le.Labels["host"] = labels["pod_name"]
	}

	for _, key := range []string{"namespace_name", "cluster_name", "pod_name", "container_name", "project_id"} {
		if v, ok := labels[key]; ok && v != "" {
			le.Labels[key] = v
		}
	}
	if rt := entry.Resource.Type; rt != "" {
		le.Labels["resource_type"] = rt
	}
}

// populateEntryLabels copies the entry-level labels the resource pass did not
// already claim, and promotes the Kubernetes app label to a service when no
// service was found.
//
// THE GUARD IS TWO CONDITIONS AND BOTH ARE LOAD-BEARING. `taken` keeps the
// resource labels authoritative, which is the ordinary precedence rule. The
// EMPTY-VALUE half is the one that is easy to drop and expensive to drop: an
// empty-valued key still enters the stream fingerprint's preimage as
// "key=\n", so admitting one produces a stream id no other implementation of
// this pipeline produces, and the second collect no longer reconciles against
// the first.
func populateEntryLabels(le *logEntry, entry *logging.Entry) {
	if entry.Labels == nil {
		return
	}
	for k, v := range entry.Labels {
		if _, taken := le.Labels[k]; taken || v == "" {
			continue
		}
		le.Labels[k] = v
	}
	if _, hasService := le.Labels["service"]; !hasService {
		if app := entry.Labels["k8s-pod/app"]; app != "" {
			le.Labels["service"] = app
		}
	}
}

// extractMessage pulls a message out of the entry's polymorphic payload. The
// SDK hands back a string for a text payload, a proto struct for a JSON one, a
// plain map from some versions, and anything else for a proto payload.
//
// THE DEFAULT ARM MARSHALS RATHER THAN DROPPING. An entry whose payload this
// module does not recognize still carries text an operator wants to find, so it
// is rendered rather than discarded; the %v fallback covers a payload that does
// not marshal at all.
func extractMessage(entry *logging.Entry) string {
	switch p := entry.Payload.(type) {
	case string:
		return p
	case *structpb.Struct:
		if p == nil {
			return ""
		}
		return extractMessageFromMap(p.AsMap())
	case map[string]any:
		return extractMessageFromMap(p)
	default:
		if entry.Payload == nil {
			return ""
		}
		b, err := json.Marshal(entry.Payload)
		if err != nil {
			return fmt.Sprintf("%v", entry.Payload)
		}
		return string(b)
	}
}

// extractLogID strips the "projects/<project>/logs/" prefix from a log name and
// decodes the percent-encoded slash Cloud Logging writes into ids that contain
// one, so `cloudaudit.googleapis.com%2Factivity` reads back as the id an
// operator sees in the console. A name without the infix is returned whole.
func extractLogID(fullName string) string {
	parts := strings.SplitN(fullName, "/logs/", 2)
	if len(parts) != 2 {
		return fullName
	}
	return strings.ReplaceAll(parts[1], "%2F", "/")
}
