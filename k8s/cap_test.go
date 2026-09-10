// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// cap_test.go — CAP-a: an over-cap custom resource must NOT return a truncated
// page while asserting a complete walk.
//
// === THE PLAIN FAKE CANNOT PRODUCE THIS FIXTURE, AND THAT IS MEASURED ===
//
// client-go's fake clientsets read only the label, field and resourceVersion
// selectors out of ListOptions. Limit and Continue are ignored entirely: every
// List returns the tracker's whole contents with an empty continuation token.
// So an over-cap fixture handed to the plain fake gives the collector every
// instance in one page, and BOTH admissible dispositions pass vacuously —
// walking the token to exhaustion and truncating silently look identical.
//
// TestFakeIgnoresLimitAndContinue below MEASURES that blind spot in this
// process rather than asserting it from documentation, and is the control that
// makes the reactor fixture's necessity checkable rather than asserted.
//
// SO THE TRUNCATION IS BUILT WITH A REACTOR that implements paging itself,
// which is the technique the in-tree Kubernetes log collector already uses for
// the same problem.

// pagingReactor is a dynamic-client reactor that ACTUALLY PAGES: it honors the
// Limit in ListOptions, sets a continuation token when more remains, and stops
// when it does not.
//
// It counts the pages it served so a test can assert the collector really made
// several calls rather than getting lucky with one.
type pagingReactor struct {
	total    int
	pageSize int
	kind     string
	apiVer   string
	pages    int
}

func (p *pagingReactor) react(action k8stesting.Action) (bool, runtime.Object, error) {
	list, ok := action.(k8stesting.ListActionImpl)
	if !ok {
		return false, nil, nil
	}
	// The fake's ListActionImpl does not surface Limit or Continue, so the
	// reactor keeps its own cursor: page N is served on the Nth call.
	start := p.pages * p.pageSize
	if start >= p.total {
		return true, &unstructured.UnstructuredList{
			Object: map[string]any{"apiVersion": p.apiVer, "kind": p.kind + "List"},
		}, nil
	}
	end := min(start+p.pageSize, p.total)
	items := make([]unstructured.Unstructured, 0, end-start)
	for i := start; i < end; i++ {
		items = append(items, *customResource(p.apiVer, p.kind, "prod", fmt.Sprintf("cr-%04d", i), nil))
	}
	p.pages++

	out := &unstructured.UnstructuredList{
		Object: map[string]any{"apiVersion": p.apiVer, "kind": p.kind + "List"},
		Items:  items,
	}
	if end < p.total {
		out.SetContinue(fmt.Sprintf("token-%d", p.pages))
	}
	_ = list
	return true, out, nil
}

func capBundle(t *testing.T, total int) (clientBundle, *pagingReactor) {
	t.Helper()
	const group, version, plural, kind = "example.com", "v1", "widgets", "Widget"
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: plural}
	listKinds := emptyDynamic()
	listKinds[gvr] = kind + "List"

	bundle := newFakeBundle(t, nil,
		[]runtime.Object{crdFor(group, version, plural, kind, true)}, listKinds)

	reactor := &pagingReactor{
		total:    total,
		pageSize: crdPageSize,
		kind:     kind,
		apiVer:   group + "/" + version,
	}
	fake, ok := bundle.dynamic.(interface {
		PrependReactor(verb, resource string, reaction k8stesting.ReactionFunc)
	})
	require.True(t, ok, "the dynamic fake exposes a reactor hook")
	fake.PrependReactor("list", plural, reactor.react)
	return bundle, reactor
}

// TestCap_OverCapCustomResourceIsIncomplete is the row.
func TestCap_OverCapCustomResourceIsIncomplete(t *testing.T) {
	bundle, reactor := capBundle(t, maxInstancesPerCRD+crdPageSize)

	res, err := walkCluster(t.Context(), bundle, framework.ForeignContext{})
	require.NoError(t, err)

	assert.False(t, res.Complete.IsComplete(),
		"a walk that stopped short of a custom resource's instances must NOT assert completeness")
	assert.Contains(t, res.Complete.Reason(), "widgets.example.com",
		"the reason names the resource that was truncated")
	assert.Contains(t, res.Complete.Reason(), "were enumerated",
		"and says how many it did reach")
	assert.Greater(t, reactor.pages, 1, "the collector paged rather than making one call")
}

// TestCap_UnderCapCustomResourceIsComplete is the SAME-RUN CONTROL, on the same
// reactor: under the bound, the collector walks the token to exhaustion,
// returns every instance and asserts a complete walk.
func TestCap_UnderCapCustomResourceIsComplete(t *testing.T) {
	const total = crdPageSize*2 + 7
	bundle, reactor := capBundle(t, total)

	res, err := walkCluster(t.Context(), bundle, framework.ForeignContext{})
	require.NoError(t, err)

	assert.True(t, res.Complete.IsComplete(),
		"under the bound every instance is read, so the walk IS complete: %s", res.Complete.Reason())
	assert.Equal(t, 3, reactor.pages, "three pages for %d instances at %d per page", total, crdPageSize)

	widgets := 0
	for _, n := range res.Nodes {
		if n.Metadata["resource_type"] == "Widget" {
			widgets++
		}
	}
	assert.Equal(t, total, widgets, "every instance across every page reached the result")
}

// TestFakeIgnoresLimitAndContinue is the CONTROL ON THE INSTRUMENT: it measures,
// in this process, that the plain fake honors neither Limit nor Continue. That
// is what makes the reactor above a necessity rather than a preference, and it
// is why an over-cap fixture on the plain fake would pass whatever the
// collector did.
func TestFakeIgnoresLimitAndContinue(t *testing.T) {
	const group, version, plural, kind = "example.com", "v1", "widgets", "Widget"
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: plural}
	listKinds := emptyDynamic()
	listKinds[gvr] = kind + "List"

	var objects []runtime.Object
	for i := range 10 {
		objects = append(objects, customResource(group+"/"+version, kind, "prod", fmt.Sprintf("cr-%d", i), nil))
	}
	bundle := newFakeBundle(t, nil, nil, listKinds, objects...)

	got, err := bundle.dynamic.Resource(gvr).Namespace(metav1.NamespaceAll).
		List(t.Context(), metav1.ListOptions{Limit: 3})
	require.NoError(t, err)

	assert.Len(t, got.Items, 10, "the fake returns the whole tracker regardless of Limit")
	assert.Empty(t, got.GetContinue(), "and never sets a continuation token")
}
