// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// walk.go — THE ENUMERATION ORDER for the 26 fixed kinds the typed clientsets
// serve, and the four rules every one of those walkers obeys.
//
// EVERY LIST IS UNSCOPED: the namespace argument is the empty string, so one
// call per kind covers the whole cluster. No requirement names a namespace or a
// label selector, and adding one would be a design decision nobody made.
//
// RULE 1 — A KIND'S API GROUP MAY NOT BE SERVED. A cluster without the policy
// group has no PodDisruptionBudgets, which is a fact about the cluster rather
// than a failure of the walk. It is still something this walk DID NOT SEE, so
// it is recorded as an incompleteness reason rather than passed over. The
// distinction matters downstream: an incomplete walk does not let the server
// treat what this collect omitted as deleted.
//
// RULE 2 — EVERY OTHER LIST FAILURE IS FATAL. A permission refusal, an
// unreachable API server or a transport failure is a walk that cannot be
// completed. Returning a partial graph from one would hand the server a
// deletion basis built out of an outage.
//
// RULE 3 — A CONTINUATION TOKEN IS A TRUNCATION. These calls pass no page
// limit, so the API server returns everything it means to; a continuation token
// nonetheless means it paginated and the remainder is unread. It is recorded,
// never assumed empty.
//
// RULE 4 — CANCELLATION IS CHECKED BETWEEN KINDS. The per-kind loop is the long
// loop, and a collect cancelled part way through must return the cancellation
// rather than a partial graph asserting anything.

// enumerateTyped runs every typed kind, in a fixed order so two collects of the
// same cluster produce the same node ordering.
func enumerateTyped(ctx context.Context, c clientBundle, w *walkResult) error {
	steps := []struct {
		name string
		run  func(context.Context, clientBundle, *walkResult) error
	}{
		{"namespaces", walkNamespaces},
		{"nodes", walkNodes},
		{"pods", walkPods},
		{"deployments", walkDeployments},
		{"statefulsets", walkStatefulSets},
		{"daemonsets", walkDaemonSets},
		{"replicasets", walkReplicaSets},
		{"jobs", walkJobs},
		{"cronjobs", walkCronJobs},
		{"services", walkServices},
		{"endpointslices", walkEndpointSlices},
		{"ingresses", walkIngresses},
		{"networkpolicies", walkNetworkPolicies},
		{"configmaps", walkConfigMaps},
		{"secrets", walkSecrets},
		{"serviceaccounts", walkServiceAccounts},
		{"roles", walkRoles},
		{"clusterroles", walkClusterRoles},
		{"rolebindings", walkRoleBindings},
		{"clusterrolebindings", walkClusterRoleBindings},
		{"persistentvolumeclaims", walkPersistentVolumeClaims},
		{"persistentvolumes", walkPersistentVolumes},
		{"storageclasses", walkStorageClasses},
		{"horizontalpodautoscalers", walkHorizontalPodAutoscalers},
		{"poddisruptionbudgets", walkPodDisruptionBudgets},
		{"customresourcedefinitions", walkCustomResourceDefinitions},
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("the collect was cancelled before enumerating %s: %w", step.name, err)
		}
		if err := step.run(ctx, c, w); err != nil {
			return fmt.Errorf("enumerating %s: %w", step.name, err)
		}
	}
	return nil
}

// listErr applies rules 1 and 2 to one List failure.
func listErr(w *walkResult, kind string, err error) error {
	if err == nil {
		return nil
	}
	if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
		w.incomplete("the API group serving %s is not available on this cluster", kind)
		return nil
	}
	return err
}

// noteContinue applies rule 3.
func noteContinue(w *walkResult, kind, cont string) {
	if cont != "" {
		w.incomplete("the %s listing came back with a continuation token, so it was truncated by the API server", kind)
	}
}

// add converts one object, files it, and files the OWNED_BY edges it states.
//
// A conversion failure is FATAL: a node this walk cannot render is not a node
// it can omit and still claim to have enumerated.
func add(w *walkResult, meta objectMeta, kind string, extra map[string]string, body any) error {
	n, err := objectNode(meta, kind, extra, body)
	if err != nil {
		return err
	}
	w.addNode(n)
	w.addEdges(ownerEdges(meta, kind)...)
	return nil
}

// workloadMeta is the metadata every pod-template-bearing kind shares.
func workloadMeta(spec corev1.PodSpec, selector *metav1.LabelSelector) (map[string]string, error) {
	m := map[string]string{
		"images": strings.Join(containerImages(spec), ","),
	}
	if spec.ServiceAccountName != "" {
		m["service_account"] = spec.ServiceAccountName
	}
	sel, err := selectorJSON(selector)
	if err != nil {
		return nil, err
	}
	if sel != "" {
		m["selector"] = sel
	}
	return m, nil
}

// selectorJSON encodes a label selector's matchLabels for the metadata map, so
// the derivations that match pods against it read a parsed value rather than
// re-deriving one from the object body.
//
// A selector with only matchExpressions encodes to the empty string, which is
// deliberate: the derivations implement equality matching, and pretending an
// expression selector was an empty equality selector would make it match every
// pod in the namespace.
//
// A MARSHAL FAILURE IS RETURNED, NOT ABSORBED INTO THAT SAME EMPTY STRING.
// Encoding a string map cannot fail in a correct build, which is the argument
// FOR the error result rather than against it: reaching that branch means
// something is wrong, and the empty string it would otherwise return is
// indistinguishable from "this object declares no selector" — under which every
// selector-driven family silently emits nothing for that object and the graph
// looks merely sparse.
func selectorJSON(selector *metav1.LabelSelector) (string, error) {
	if selector == nil || len(selector.MatchLabels) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(selector.MatchLabels)
	if err != nil {
		return "", fmt.Errorf("encoding the label selector: %w", err)
	}
	return string(encoded), nil
}

func containerImages(spec corev1.PodSpec) []string {
	var out []string
	for _, c := range spec.InitContainers {
		if c.Image != "" {
			out = append(out, c.Image)
		}
	}
	for _, c := range spec.Containers {
		if c.Image != "" {
			out = append(out, c.Image)
		}
	}
	return out
}

// podTemplateEdges renders the four edges a pod template states: the service
// account it runs as, and the ConfigMaps, Secrets and claims it mounts.
//
// IT DEDUPES ON (type, target). A container that names the same Secret in both
// an envFrom and a volume states one relationship, not two, and a graph
// carrying it twice reports a false fan-out.
func podTemplateEdges(ownerID, namespace string, spec corev1.PodSpec) []Edge {
	var out []Edge
	seen := map[string]bool{}
	emit := func(kind, name, edgeType, method string) {
		if name == "" {
			return
		}
		target := resourceID(namespace, kind, name)
		key := edgeType + "\x00" + target
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Edge{FromID: ownerID, ToID: target, Type: edgeType, Method: method})
	}

	if spec.ServiceAccountName != "" {
		emit("ServiceAccount", spec.ServiceAccountName, edgeUsesSA, "")
	}
	podVolumeEdges(spec, emit)
	for _, ref := range spec.ImagePullSecrets {
		emit("Secret", ref.Name, edgeMountsSecret, "imagePullSecret")
	}
	podContainerEdges(spec, emit)
	return out
}

// emitEdge is the deduping writer podTemplateEdges hands to its per-volume and
// per-container arms, so both arms dedupe against the SAME (type, target) set
// as the arms that stayed in the caller.
type emitEdge func(kind, name, edgeType, method string)

// podVolumeEdges emits what a pod template's volumes mount, including the
// ConfigMap and Secret sources of a projected volume.
func podVolumeEdges(spec corev1.PodSpec, emit emitEdge) {
	for _, v := range spec.Volumes {
		switch {
		case v.ConfigMap != nil:
			emit("ConfigMap", v.ConfigMap.Name, edgeMountsConfigMap, "volume")
		case v.Secret != nil:
			emit("Secret", v.Secret.SecretName, edgeMountsSecret, "volume")
		case v.PersistentVolumeClaim != nil:
			emit("PersistentVolumeClaim", v.PersistentVolumeClaim.ClaimName, edgeUsesPVC, "volume")
		case v.Projected != nil:
			for _, src := range v.Projected.Sources {
				if src.ConfigMap != nil {
					emit("ConfigMap", src.ConfigMap.Name, edgeMountsConfigMap, "projected")
				}
				if src.Secret != nil {
					emit("Secret", src.Secret.Name, edgeMountsSecret, "projected")
				}
			}
		}
	}
}

// podContainerEdges emits what a pod template's containers read out of the
// environment. INIT CONTAINERS COUNT: a Secret only an init container reads is
// still a relationship the pod states.
func podContainerEdges(spec corev1.PodSpec, emit emitEdge) {
	containers := make([]corev1.Container, 0, len(spec.Containers)+len(spec.InitContainers))
	containers = append(containers, spec.InitContainers...)
	containers = append(containers, spec.Containers...)
	for i := range containers {
		cn := &containers[i]
		for _, ef := range cn.EnvFrom {
			if ef.ConfigMapRef != nil {
				emit("ConfigMap", ef.ConfigMapRef.Name, edgeMountsConfigMap, "envFrom")
			}
			if ef.SecretRef != nil {
				emit("Secret", ef.SecretRef.Name, edgeMountsSecret, "envFrom")
			}
		}
		for _, e := range cn.Env {
			if e.ValueFrom == nil {
				continue
			}
			if e.ValueFrom.ConfigMapKeyRef != nil {
				emit("ConfigMap", e.ValueFrom.ConfigMapKeyRef.Name, edgeMountsConfigMap, "env")
			}
			if e.ValueFrom.SecretKeyRef != nil {
				emit("Secret", e.ValueFrom.SecretKeyRef.Name, edgeMountsSecret, "env")
			}
		}
	}
}

// sortedUnique drops empties and duplicates and sorts, for a metadata value
// that has to be stable across two collects of an unchanged cluster.
func sortedUnique(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
