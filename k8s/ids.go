// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ids.go — the ID SCHEME and the small metadata conventions every converter
// shares.
//
// THE SCHEME IS "namespace/Kind/name" FOR A NAMESPACED OBJECT AND "Kind/name"
// FOR A CLUSTER-SCOPED ONE. It is not decorative: an edge names its endpoints
// by id, and a converter that mints a namespaced id for a cluster-scoped object
// produces edges that resolve to nothing. Every id in this collector is built
// here so there is one place the scheme can be wrong.

// resourceID builds the node id for one Kubernetes object. An empty namespace
// means the object is cluster-scoped and its id carries no namespace segment.
func resourceID(namespace, kind, name string) string {
	if namespace == "" {
		return kind + "/" + name
	}
	return namespace + "/" + kind + "/" + name
}

// labelsToMeta converts an object's labels to metadata keys under the "label/"
// prefix, which is what keeps a label named "namespace" or "phase" from
// colliding with the collector's own metadata keys.
func labelsToMeta(labels map[string]string) map[string]string {
	m := make(map[string]string, len(labels))
	for k, v := range labels {
		m["label/"+k] = v
	}
	return m
}

// annotationsInto adds an object's annotations to meta under the "annotation/"
// prefix, mirroring the label scheme. Annotations carry markers a downstream
// analyzer reads (a static-pod source, a workload-identity binding) that are
// nowhere else in the object's structured fields.
func annotationsInto(meta, annotations map[string]string) {
	for k, v := range annotations {
		meta["annotation/"+k] = v
	}
}

// marshalJSON renders an object as the node's Content.
//
// AN ERROR IS RETURNED, NOT SWALLOWED. A node whose content silently became the
// empty string is a node whose body downstream summarization and embedding
// would read as an empty document, and the walk would still assert a complete
// enumeration over it. The repository's standing rule is that bad input errors
// and nothing degrades silently.
func marshalJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshaling the object body: %w", err)
	}
	return string(b), nil
}

// splitID reverses [resourceID] for a node id this collector minted. It reports
// false for an id that does not carry one of the two shapes, which is what
// keeps a derived pass from inventing a namespace for an id it cannot parse.
func splitID(id string) (namespace, kind, name string, ok bool) {
	parts := strings.Split(id, "/")
	switch len(parts) {
	case 2:
		return "", parts[0], parts[1], parts[0] != "" && parts[1] != ""
	case 3:
		return parts[0], parts[1], parts[2], parts[0] != "" && parts[1] != "" && parts[2] != ""
	default:
		return "", "", "", false
	}
}
