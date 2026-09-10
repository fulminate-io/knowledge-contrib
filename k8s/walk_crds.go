// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// walk_crds.go — THE DYNAMIC HALF OF THE KIND FLOOR: every custom resource the
// cluster serves, and the page discipline that keeps a truncated listing from
// asserting a complete walk.
//
// THE KIND COMES FROM THE INSTANCE, NOT FROM THE DEFINITION. A CRD's metadata
// name is a plural resource name qualified by its group
// ("certificates.cert-manager.io"); the objects it serves carry the Kind
// ("Certificate"). Using the definition's name as the kind puts every custom
// resource in the graph under an id nothing else in the cluster refers to.
//
// === THE PAGE DISCIPLINE, AND WHY IT IS NOT THE BUILT-IN'S ===
//
// The built-in collector asks for at most 1000 instances per custom resource
// and reads no continuation token. On a cluster with more than that, its walk
// returns a truncated page and asserts a COMPLETE enumeration — which arms the
// server's whole-remainder deletion phase against a graph that saw half the
// objects. This collector does not inherit that shape.
//
// WHAT IT DOES INSTEAD: it pages, following the continuation token, and keeps
// the same 1000-instance bound per custom resource so a runaway CRD cannot
// blow the result size cap. When the bound is reached with a token still
// outstanding, the walk records an INCOMPLETE enumeration naming the resource
// and the count it reached. So the two outcomes are an honest complete walk, or
// a bounded partial walk that says so; a truncated page asserting completeness
// is not among them.
//
// THE BOUND IS PER CUSTOM RESOURCE, NOT PER CLUSTER, matching the built-in's
// unit so the parity comparison is between like quantities.

// maxInstancesPerCRD bounds how many instances of ONE custom resource a walk
// reads. Exceeding it makes the walk incomplete rather than truncating it
// silently.
const maxInstancesPerCRD = 1000

// crdPageSize is how many instances one List asks for. It is smaller than the
// bound so the bound is reached by paging rather than by a single oversized
// request.
const crdPageSize = 250

// enumerateCustomResources discovers every served custom resource and walks it.
func enumerateCustomResources(ctx context.Context, c clientBundle, w *walkResult) error {
	crds, err := c.apiext.ApiextensionsV1().CustomResourceDefinitions().List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
			w.incomplete("the apiextensions API is not available, so no custom resources were enumerated")
			return nil
		}
		return fmt.Errorf("listing custom resource definitions: %w", err)
	}
	noteContinue(w, "CustomResourceDefinition (for instance discovery)", crds.Continue)

	for i := range crds.Items {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("the collect was cancelled while enumerating custom resources: %w", err)
		}
		crd := &crds.Items[i]
		gvr, ok := servedGVR(crd)
		if !ok {
			w.incomplete("the custom resource definition %s serves no version, so its instances were not enumerated", crd.Name)
			continue
		}
		if err := walkCustomResource(ctx, c, w, crd, gvr); err != nil {
			return fmt.Errorf("enumerating instances of %s: %w", crd.Name, err)
		}
	}
	return nil
}

// servedGVR resolves a definition to the version this walk reads.
//
// IT PREFERS THE STORAGE VERSION, falling back to the first served one. The
// storage version is the one every other version is converted from, so reading
// it is what makes two collects of an unchanged cluster produce the same body
// even if a client elsewhere writes through a different version.
func servedGVR(crd *apiextv1.CustomResourceDefinition) (schema.GroupVersionResource, bool) {
	var fallback string
	for i := range crd.Spec.Versions {
		v := &crd.Spec.Versions[i]
		if !v.Served {
			continue
		}
		if v.Storage {
			return schema.GroupVersionResource{
				Group: crd.Spec.Group, Version: v.Name, Resource: crd.Spec.Names.Plural,
			}, true
		}
		if fallback == "" {
			fallback = v.Name
		}
	}
	if fallback == "" {
		return schema.GroupVersionResource{}, false
	}
	return schema.GroupVersionResource{
		Group: crd.Spec.Group, Version: fallback, Resource: crd.Spec.Names.Plural,
	}, true
}

// walkCustomResource pages through one custom resource's instances.
func walkCustomResource(
	ctx context.Context,
	c clientBundle,
	w *walkResult,
	crd *apiextv1.CustomResourceDefinition,
	gvr schema.GroupVersionResource,
) error {
	extractor := crdExtractorFor(crd.Spec.Group)

	seen := 0
	cont := ""
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancelled after %d instances: %w", seen, err)
		}
		list, err := c.dynamic.Resource(gvr).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
			Limit:    crdPageSize,
			Continue: cont,
		})
		if err != nil {
			if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
				// The definition exists but the API is not actually serving it
				// — a CRD mid-deletion, or one whose controller never
				// established it. Not an error, and not silence.
				w.incomplete("the custom resource %s is defined but not served, so its instances were not enumerated", crd.Name)
				return nil
			}
			return err
		}

		for i := range list.Items {
			item := &list.Items[i]
			// THE KIND IS THE INSTANCE'S OWN, falling back to the definition's
			// declared kind for an instance that somehow carries none.
			kind := item.GetKind()
			if kind == "" {
				kind = crd.Spec.Names.Kind
			}
			if err := addUnstructured(w, item, kind, map[string]string{
				"crd_group": crd.Spec.Group,
				"crd_name":  crd.Name,
			}); err != nil {
				return err
			}
			if extractor != nil {
				w.addEdges(extractor(item, kind)...)
			}
			seen++
		}

		cont = list.GetContinue()
		if cont == "" {
			return nil
		}
		if seen >= maxInstancesPerCRD {
			// THE BOUNDED-PARTIAL ARM. The remainder is deliberately unread,
			// and the walk says so rather than asserting it saw everything.
			w.incomplete(
				"the custom resource %s has more than the %d instances this walk reads per resource; "+
					"%d were enumerated and the rest were not",
				crd.Name, maxInstancesPerCRD, seen)
			return nil
		}
	}
}

// crdExtractor turns one custom-resource instance into the edges its own spec
// states. A group with no extractor contributes nodes and owner edges only,
// which is the ordinary case: most custom resources describe something this
// graph has no other node for.
type crdExtractor func(item *unstructured.Unstructured, kind string) []Edge

// crdExtractorFor returns the extractor registered for an API group, or nil.
//
// THIS IS A LOOKUP, NOT A REGISTRY THAT PANICS. The built-in collector's
// equivalent panics on an empty group, a nil extractor and a duplicate
// registration — a reasonable shape inside a long-lived daemon with many
// packages, and the wrong one here: an init-time panic in a process whose whole
// surface is one MCP tool kills the provider before it can answer, and the
// caller sees a broken JSON-RPC frame rather than an error. The table below is
// a package-level literal, so a duplicate key is a COMPILE error and there is
// nothing left to check at runtime.
func crdExtractorFor(group string) crdExtractor {
	return crdExtractors[group]
}
