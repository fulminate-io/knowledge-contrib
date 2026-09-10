// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"maps"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// convert.go — the ONE PLACE a Kubernetes object becomes a contract node.
//
// Every converter in this collector funnels through [objectNode], so the id
// scheme, the label and annotation prefixes, the namespace metadata key and the
// JSON body are decided once. A converter that built a node by hand would be
// the place the scheme quietly diverges for one kind, and an edge naming that
// kind's id would then dangle with nothing to catch it.

// objectMeta is the subset of a Kubernetes object's metadata every converter
// reads. Taking it as a value rather than as an interface keeps the typed and
// the dynamic paths on one function.
type objectMeta struct {
	Namespace       string
	Name            string
	Labels          map[string]string
	Annotations     map[string]string
	OwnerReferences []metav1.OwnerReference
}

func metaOf(m metav1.ObjectMeta) objectMeta {
	return objectMeta{
		Namespace:       m.Namespace,
		Name:            m.Name,
		Labels:          m.Labels,
		Annotations:     m.Annotations,
		OwnerReferences: m.OwnerReferences,
	}
}

// objectNode converts one object to a node.
//
// extra carries the per-kind metadata the converter derived from the object's
// spec and status — a phase, a replica count, a selector — and overwrites
// nothing this function sets. body is the object itself, marshaled as the
// node's content.
//
// A MARSHAL FAILURE IS RETURNED, NOT SWALLOWED. A node whose content silently
// became empty reads downstream as a document with no text, and the walk would
// still assert it enumerated the object.
func objectNode(meta objectMeta, kind string, extra map[string]string, body any) (Node, error) {
	content, err := marshalJSON(body)
	if err != nil {
		return Node{}, fmt.Errorf("%s %s: %w", kind, resourceID(meta.Namespace, kind, meta.Name), err)
	}

	m := labelsToMeta(meta.Labels)
	annotationsInto(m, meta.Annotations)
	m["resource_type"] = kind
	if meta.Namespace != "" {
		m["namespace"] = meta.Namespace
	}
	maps.Copy(m, extra)

	return Node{
		ID:         resourceID(meta.Namespace, kind, meta.Name),
		Type:       nodeTypeCloudResource,
		SymbolName: meta.Name,
		Content:    content,
		Metadata:   m,
	}, nil
}

// ownerEdges renders an object's ownerReferences as OWNED_BY edges.
//
// An owner reference names a kind and a name and is ALWAYS in the object's own
// namespace (Kubernetes does not permit a cross-namespace owner), so the target
// id is built with the child's namespace — except for a cluster-scoped owner,
// which carries none.
func ownerEdges(meta objectMeta, kind string) []Edge {
	childID := resourceID(meta.Namespace, kind, meta.Name)
	out := make([]Edge, 0, len(meta.OwnerReferences))
	for _, ref := range meta.OwnerReferences {
		if ref.Kind == "" || ref.Name == "" {
			continue
		}
		ownerNamespace := meta.Namespace
		if clusterScopedKinds[ref.Kind] {
			ownerNamespace = ""
		}
		out = append(out, Edge{
			FromID: childID,
			ToID:   resourceID(ownerNamespace, ref.Kind, ref.Name),
			Type:   edgeOwnedBy,
		})
	}
	return out
}

// itoa renders an int32 for a metadata value. Metadata is a string map, and a
// count that reached it through fmt.Sprint of an interface would render a nil
// pointer as "<nil>".
func itoa(v int32) string { return strconv.FormatInt(int64(v), 10) }

// deref returns the value behind a pointer, or the zero value. Kubernetes
// spells an optional scalar as a pointer, and a converter that dereferenced one
// without checking would panic on an object that simply omitted the field — in
// a process whose whole surface is one tool call.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
