// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"maps"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// walk_dynamic.go — the SIX FIXED KINDS THAT ARE THEMSELVES CUSTOM RESOURCES:
// the four Gateway API kinds and the two AdminNetworkPolicy kinds.
//
// THEY ARE FIXED KINDS SERVED THROUGH THE DYNAMIC CLIENT, which sounds like a
// contradiction and is not. Gateway API and AdminNetworkPolicy ship as CRDs, so
// no typed clientset in client-go serves them and the dynamic client is the
// only way to read them — but their kinds and their schemas are known ahead of
// time, so unlike a genuine custom resource they are part of the fixed floor
// and their edges are extracted from named fields rather than discovered.
//
// A CLUSTER WITHOUT THEM IS NOT AN ERROR AND IS NOT SILENCE EITHER. Neither
// group is installed by default. A cluster that does not serve one has no
// objects of that kind, which the walk records as something it did not see, so
// the collect does not assert that it enumerated a kind the cluster never
// offered.

var (
	gatewayClassGVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}
	gatewayGVR      = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	httpRouteGVR    = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	grpcRouteGVR    = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "grpcroutes"}

	adminNetworkPolicyGVR = schema.GroupVersionResource{
		Group: "policy.networking.k8s.io", Version: "v1alpha1", Resource: "adminnetworkpolicies"}
	baselineAdminNetworkPolicyGVR = schema.GroupVersionResource{
		Group: "policy.networking.k8s.io", Version: "v1alpha1", Resource: "baselineadminnetworkpolicies"}
)

// enumerateDynamic runs the six dynamic fixed kinds.
func enumerateDynamic(ctx context.Context, c clientBundle, w *walkResult) error {
	steps := []struct {
		gvr           schema.GroupVersionResource
		kind          string
		clusterScoped bool
		edges         func(*unstructured.Unstructured, string) []Edge
	}{
		{gatewayClassGVR, "GatewayClass", true, nil},
		{gatewayGVR, "Gateway", false, nil},
		{httpRouteGVR, "HTTPRoute", false, routeBackendEdges},
		{grpcRouteGVR, "GRPCRoute", false, routeBackendEdges},
		{adminNetworkPolicyGVR, "AdminNetworkPolicy", true, nil},
		{baselineAdminNetworkPolicyGVR, "BaselineAdminNetworkPolicy", true, nil},
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("the collect was cancelled before enumerating %s: %w", step.kind, err)
		}
		if err := walkDynamicKind(ctx, c.dynamic, w, step.gvr, step.kind, step.clusterScoped, step.edges); err != nil {
			return fmt.Errorf("enumerating %s: %w", step.kind, err)
		}
	}
	return nil
}

// walkDynamicKind lists one dynamic resource and converts every instance.
func walkDynamicKind(
	ctx context.Context,
	client dynamic.Interface,
	w *walkResult,
	gvr schema.GroupVersionResource,
	kind string,
	clusterScoped bool,
	edges func(*unstructured.Unstructured, string) []Edge,
) error {
	var ri dynamic.ResourceInterface = client.Resource(gvr)
	if !clusterScoped {
		ri = client.Resource(gvr).Namespace(metav1.NamespaceAll)
	}
	list, err := ri.List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
			w.incomplete("the %s API (%s) is not served on this cluster", kind, gvr.GroupVersion())
			return nil
		}
		return err
	}
	noteContinue(w, kind, list.GetContinue())

	for i := range list.Items {
		item := &list.Items[i]
		if err := addUnstructured(w, item, kind, nil); err != nil {
			return err
		}
		if edges != nil {
			w.addEdges(edges(item, kind)...)
		}
	}
	return nil
}

// addUnstructured converts one dynamic instance to a node.
//
// THE NAMESPACE COMES FROM THE OBJECT, NOT FROM THE CALLER. A cluster-scoped
// instance reports an empty namespace and gets a "Kind/name" id; a namespaced
// one reports its own. Deciding it at the call site would make every id in a
// kind wrong together the moment the resource's scope was misremembered.
func addUnstructured(w *walkResult, item *unstructured.Unstructured, kind string, extra map[string]string) error {
	meta := objectMeta{
		Namespace:       item.GetNamespace(),
		Name:            item.GetName(),
		Labels:          item.GetLabels(),
		Annotations:     item.GetAnnotations(),
		OwnerReferences: item.GetOwnerReferences(),
	}
	m := map[string]string{
		"api_version": item.GetAPIVersion(),
	}
	maps.Copy(m, extra)
	n, err := objectNode(meta, kind, m, item.Object)
	if err != nil {
		return err
	}
	w.addNode(n)
	w.addEdges(ownerEdges(meta, kind)...)
	return nil
}

// routeBackendEdges renders ROUTES_TO from a Gateway API route's backendRefs.
//
// HTTPRoute AND GRPCRoute SHARE THIS SHAPE: both carry spec.rules[].backendRefs
// [] whose entries name a Service by default. A backendRef with an explicit
// kind that is not Service names something this graph does not hold and gets no
// edge; a backendRef with its own namespace crosses namespaces legally and the
// edge follows it.
func routeBackendEdges(item *unstructured.Unstructured, kind string) []Edge {
	routeID := resourceID(item.GetNamespace(), kind, item.GetName())
	rules, ok := nestedSlice(item.Object, "spec", "rules")
	if !ok {
		return nil
	}

	var out []Edge
	seen := map[string]bool{}
	for _, rule := range rules {
		ruleMap, ok := rule.(map[string]any)
		if !ok {
			continue
		}
		refs, ok := nestedSlice(ruleMap, "backendRefs")
		if !ok {
			continue
		}
		for _, ref := range refs {
			refMap, ok := ref.(map[string]any)
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(refMap, "name")
			if name == "" {
				continue
			}
			refKind, _, _ := unstructured.NestedString(refMap, "kind")
			if refKind != "" && refKind != "Service" {
				continue
			}
			ns, _, _ := unstructured.NestedString(refMap, "namespace")
			if ns == "" {
				ns = item.GetNamespace()
			}
			target := resourceID(ns, "Service", name)
			if seen[target] {
				continue
			}
			seen[target] = true
			out = append(out, Edge{FromID: routeID, ToID: target, Type: edgeRoutesTo})
		}
	}
	return out
}

// nestedSlice reads a nested list WITHOUT DEEP-COPYING IT.
//
// THE STANDARD HELPER PANICS, AND THIS IS NOT A HYPOTHETICAL. unstructured's
// NestedSlice deep-copies what it returns, and its copier PANICS on any value
// outside the JSON type set — "cannot deep copy int". Every failure path in
// this collector returns an error instead, because a panic inside an MCP tool
// handler takes down the in-process dispatcher and breaks the JSON-RPC framing
// for the caller, who then sees a broken frame rather than a message. A reader
// that walks a body it did not decode itself must not carry that risk.
//
// The no-copy read is also the cheaper one: a route list on a large Gateway API
// object is copied for nothing when the caller only reads it.
func nestedSlice(obj map[string]any, fields ...string) ([]any, bool) {
	v, ok, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil || !ok {
		return nil, false
	}
	list, ok := v.([]any)
	return list, ok
}
