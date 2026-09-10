// SPDX-License-Identifier: Apache-2.0

package main

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// secret.go — WHAT A SECRET IS ALLOWED TO CONTRIBUTE TO THE GRAPH, as an
// allowlist rather than a list of things to strip.
//
// === WHY AN ALLOWLIST, AND WHY THE STRIP LIST FAILED ===
//
// The first version of this nilled Data and StringData on a copy of the object
// and marshaled the rest. That is a DENY LIST, and it was defeated by the most
// ordinary Secret in any cluster: one created with `kubectl apply`, which
// carries the entire submitted manifest — data block included — inside the
// annotation kubectl.kubernetes.io/last-applied-configuration. The value
// reached the node body AND, through the annotation-to-metadata lift every kind
// shares, a metadata value as well. Both were measured on this module's own
// walk.
//
// A deny list is the wrong shape for this class because it has to enumerate
// every path a value can take, and the API server keeps adding paths: the
// apply annotation, managed-fields entries, a future controller's annotation
// echoing spec back. An allowlist inverts the default, so a path nobody thought
// of contributes NOTHING instead of everything.
//
// === THE CENSUS THIS CLOSES ===
//
// Every route from a Secret object into a node, and what each now carries:
//
//  1. Node.Content, from marshaling the object. NOW BUILT FROM [secretBody]
//     BELOW, a struct with an explicit field set, rather than from the API
//     object. Annotations beyond the allowlist, managed fields, owner
//     references, finalizers and every field added to corev1.Secret in future
//     are absent BY CONSTRUCTION rather than by being stripped.
//  2. metadata annotation/<name>, from the shared annotation lift. NOW FILTERED
//     to [secretAnnotationAllowlist] before the node is built.
//  3. metadata label/<name>, from the shared label lift. KEPT. A label is
//     bounded to 63 characters, is an index key rather than payload, and is the
//     same selector and ownership metadata every other kind contributes; the
//     apply annotation is what carried the manifest, not a label.
//  4. metadata secret_type, key_names, key_count, from this walker. KEPT: a key
//     NAME and a COUNT are non-reversible properties of the secret, which is
//     the form a credential assertion is allowed to take. No value reaches
//     them.
//  5. Node.SymbolName and the node id, from the object's name and namespace.
//     KEPT: identity.
//  6. Edges naming this Secret, from ServiceAccounts and pod templates. They
//     carry the resolved node ID only, never a value.
//
// WHAT EACH ALLOWLISTED FIELD IS FOR is stated on the field.

// secretAnnotationAllowlist is the exhaustive set of annotation names that may
// reach a Secret's node.
//
// BOTH ENTRIES CARRY A NAME OR AN IDENTIFIER, NEVER SECRET MATERIAL. They are
// the binding that says which ServiceAccount a token Secret belongs to, which
// is a real graph relationship an operator asks about. Every other annotation,
// including any a future controller adds, is dropped.
var secretAnnotationAllowlist = map[string]bool{
	"kubernetes.io/service-account.name": true,
	"kubernetes.io/service-account.uid":  true,
}

// secretBody is the ONLY shape of a Secret that is ever marshaled into a node's
// content. Adding a field here is a deliberate decision that it carries no
// credential material.
type secretBody struct {
	// APIVersion and Kind make the body self-describing, as the API object's
	// own serialization would.
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	// Metadata is identity and the two lifted maps, nothing else. In
	// particular there are no managedFields: an apply entry lists the data
	// keys it owns and grows without bound, and it is metadata about writers
	// rather than about the secret.
	Metadata secretBodyMeta `json:"metadata"`
	// Type is what kind of secret this is (Opaque, a TLS pair, a
	// service-account token), which is a property, not a value.
	Type string `json:"type,omitempty"`
	// Immutable is an operational property an operator asks about.
	Immutable *bool `json:"immutable,omitempty"`
	// DataKeys is the KEY NAMES, sorted. The names are what a dependency
	// question needs ("which workload reads the tls.key of this secret") and no
	// credential is recoverable from them.
	DataKeys []string `json:"data_keys,omitempty"`
}

type secretBodyMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	UID               string            `json:"uid,omitempty"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
}

// redactSecret returns the allowlisted body and the allowlisted object metadata
// for one Secret.
//
// THE SECOND RETURN IS WHAT CLOSES THE METADATA PATH. objectNode lifts whatever
// annotations it is handed, so filtering has to happen BEFORE the node is
// built, not after.
func redactSecret(s *corev1.Secret, dataKeys []string) (secretBody, objectMeta) {
	annotations := map[string]string{}
	for name, value := range s.Annotations {
		if secretAnnotationAllowlist[name] {
			annotations[name] = value
		}
	}

	created := ""
	if !s.CreationTimestamp.IsZero() {
		created = s.CreationTimestamp.UTC().Format(metav1.RFC3339Micro)
	}

	body := secretBody{
		APIVersion: "v1",
		Kind:       "Secret",
		Metadata: secretBodyMeta{
			Name:              s.Name,
			Namespace:         s.Namespace,
			UID:               string(s.UID),
			CreationTimestamp: created,
			Labels:            s.Labels,
			Annotations:       annotations,
		},
		Type:      string(s.Type),
		Immutable: s.Immutable,
		DataKeys:  dataKeys,
	}

	meta := objectMeta{
		Namespace:   s.Namespace,
		Name:        s.Name,
		Labels:      s.Labels,
		Annotations: annotations,
		// OWNER REFERENCES ARE DROPPED for a Secret. They name a controller by
		// kind and name and carry no value, but they are also the one remaining
		// path that would put an unfiltered API-server-controlled string on the
		// node, and a Secret's owner relationship is not a question this graph
		// is asked. Dropping them keeps the allowlist total.
	}
	return body, meta
}
