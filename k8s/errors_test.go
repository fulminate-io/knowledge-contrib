// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// errors_test.go — the ERROR ARMS, one per failure a walk can meet, and the
// same-run success control for each.
//
// THE DIVIDING LINE THESE ROWS PIN: a failure that means "this cluster does not
// have that" is RECORDED and survived; a failure that means "this walk could
// not see" is FATAL. Both are honest and they are not interchangeable. A
// permission refusal treated as an absent API group would return a partial
// graph asserting a complete walk, which hands the server a deletion basis
// built out of an outage.

// failListReactor makes one resource's List fail with the given error.
func failListReactor(t *testing.T, bundle clientBundle, resource string, err error) {
	t.Helper()
	fake, ok := bundle.typed.(interface {
		PrependReactor(verb, resource string, reaction k8stesting.ReactionFunc)
	})
	require.True(t, ok)
	fake.PrependReactor("list", resource, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, err
	})
}

// TestErr_NotServedAPIGroupIsRecordedAndSurvived is the "this cluster does not
// have that" arm.
func TestErr_NotServedAPIGroupIsRecordedAndSurvived(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "i"}}},
	}
	bundle := newFakeBundle(t, []runtime.Object{pod}, nil, emptyDynamic())
	failListReactor(t, bundle, "poddisruptionbudgets",
		apierrors.NewNotFound(schema.GroupResource{Group: "policy", Resource: "poddisruptionbudgets"}, ""))

	res, err := walkCluster(t.Context(), bundle, framework.ForeignContext{})
	require.NoError(t, err, "an API group the cluster does not serve is not a walk failure")

	assert.False(t, res.Complete.IsComplete(),
		"but it IS something this walk did not see, so the walk is not complete")
	assert.Contains(t, res.Complete.Reason(), "PodDisruptionBudget")

	// SAME-RUN CONTROL: the rest of the walk still ran.
	assert.NotEmpty(t, res.Nodes)
	nodeByID(t, res.Nodes, "prod/Pod/api")
}

// TestErr_PermissionRefusalIsFatal is the "this walk could not see" arm, and
// the mutation that matters: swallowing it would return a partial graph.
func TestErr_PermissionRefusalIsFatal(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "i"}}},
	}
	bundle := newFakeBundle(t, []runtime.Object{pod}, nil, emptyDynamic())
	failListReactor(t, bundle, "secrets", apierrors.NewForbidden(
		schema.GroupResource{Resource: "secrets"}, "", errors.New("no list permission")))

	_, err := walkCluster(t.Context(), bundle, framework.ForeignContext{})
	require.Error(t, err, "a credential that cannot list a kind must NOT produce a partial graph")
	assert.Contains(t, err.Error(), "secrets")
}

// TestErr_TransportFailureIsLoud. An operator whose config entry omits a proxy
// value makes this reachable in the field, and a partial graph returned from it
// would be indistinguishable from a cluster that genuinely emptied out.
func TestErr_TransportFailureIsLoud(t *testing.T) {
	bundle := newFakeBundle(t, nil, nil, emptyDynamic())
	failListReactor(t, bundle, "pods", errors.New("dial tcp 10.0.0.1:443: connect: connection refused"))

	_, err := walkCluster(t.Context(), bundle, framework.ForeignContext{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")

	// SAME-RUN CONTROL: the same fixture with a reachable endpoint completes.
	ok := newFakeBundle(t, nil, nil, emptyDynamic())
	res, err := walkCluster(t.Context(), ok, framework.ForeignContext{})
	require.NoError(t, err)
	assert.True(t, res.Complete.IsComplete())
}

// TestErr_CancelledMidWalkReturnsTheCancellation. A cancelled collect must
// return the cancellation, write nothing, and assert no completeness.
func TestErr_CancelledMidWalkReturnsTheCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	bundle := newFakeBundle(t, nil, nil, emptyDynamic())
	// Cancel from inside the walk, between two per-kind listings.
	fake, ok := bundle.typed.(interface {
		PrependReactor(verb, resource string, reaction k8stesting.ReactionFunc)
	})
	require.True(t, ok)
	fake.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		cancel()
		return false, nil, nil
	})

	_, err := walkCluster(ctx, bundle, framework.ForeignContext{})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled, "the cancellation is returned, not swallowed")

	// SAME-RUN CONTROL: the identical fixture with a live context completes.
	live := newFakeBundle(t, nil, nil, emptyDynamic())
	res, err := walkCluster(t.Context(), live, framework.ForeignContext{})
	require.NoError(t, err)
	assert.True(t, res.Complete.IsComplete())
}

// TestErr_CancelledBeforeItBegins covers the entry check.
func TestErr_CancelledBeforeItBegins(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	c := &k8sCollector{
		sources: credentialSources{lookupEnv: fakeEnv(nil)},
		newClients: func(credential) (clientBundle, error) {
			t.Fatal("a cancelled collect must not reach the client construction")
			return clientBundle{}, nil
		},
	}
	_, err := c.Walk(ctx, "cluster", Params{}, framework.ForeignContext{})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

// TestErr_NoFailurePathPanics is ERR-panic. A panic inside an MCP tool handler
// takes down the in-process dispatcher and breaks the JSON-RPC framing for the
// caller, who sees a broken frame rather than an error; there is no recover at
// the call site. Every failure path here returns an error instead.
//
// THE INPUTS BELOW ARE THE SHAPES THAT WOULD PANIC A NAIVE IMPLEMENTATION:
// optional scalars Kubernetes spells as pointers, and dynamic bodies whose
// nested fields are the wrong type entirely.
func TestErr_NoFailurePathPanics(t *testing.T) {
	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{"deployment with every optional pointer nil", func(t *testing.T) {
			bundle := newFakeBundle(t, []runtime.Object{
				&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "p"}},
				&corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "v"}},
			}, nil, emptyDynamic())
			_, err := walkCluster(t.Context(), bundle, framework.ForeignContext{})
			assert.NoError(t, err)
		}},
		{"custom resource whose spec is a string", func(t *testing.T) {
			item := &unstructuredOf{apiVersion: "keda.sh/v1alpha1", kind: "ScaledObject"}
			edges := extractKEDA(item.build("prod", "x", map[string]any{"spec": "not-an-object"}), "ScaledObject")
			assert.Empty(t, edges)
		}},
		{"custom resource whose route list holds scalars", func(t *testing.T) {
			item := &unstructuredOf{apiVersion: "traefik.io/v1alpha1", kind: "IngressRoute"}
			edges := extractTraefik(item.build("prod", "x", map[string]any{
				"spec": map[string]any{"routes": []any{"not-a-map", 42}}}), "IngressRoute")
			assert.Empty(t, edges)
		}},
		{"network policy whose content is not JSON", func(t *testing.T) {
			n := namespacedNode("prod", "NetworkPolicy", "broken", map[string]string{"policy_types": "Ingress"})
			n.Content = "{not json"
			assert.Empty(t, buildNetworkPolicyReachabilityEdges([]Node{n}))
		}},
		{"selector metadata that does not decode", func(t *testing.T) {
			svc := namespacedNode("prod", "Service", "api", map[string]string{"selector": "{{{"})
			pod := namespacedNode("prod", "Pod", "p", map[string]string{"label/app": "api"})
			assert.Empty(t, buildSelectsEdges([]Node{svc, pod}),
				"a selector that does not decode selects NOTHING; treating it as empty would "+
					"match every pod in the namespace")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotPanics(t, func() { tc.run(t) })
		})
	}
}

// unstructuredOf is a tiny builder so the panic table above reads as data.
type unstructuredOf struct {
	apiVersion string
	kind       string
}

func (u *unstructuredOf) build(namespace, name string, body map[string]any) *unstructured.Unstructured {
	return customResource(u.apiVersion, u.kind, namespace, name, body)
}

// TestErr_MarshalFailureIsFatalNotSilent. A node whose content silently became
// the empty string reads downstream as a document with no text, and the walk
// would still claim to have enumerated the object.
func TestErr_MarshalFailureIsFatalNotSilent(t *testing.T) {
	_, err := objectNode(objectMeta{Namespace: "prod", Name: "x"}, "Pod", nil, make(chan int))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prod/Pod/x", "the refusal names the object it could not render")
}

// TestErr_WalkFailureNeverBecomesAnEmptySuccess. This is the framework's own
// contract, asserted here because it is what makes every fatal arm above
// meaningful: an error return becomes a tool-call error, never a successful
// empty result that the server would then treat as a cluster with nothing in
// it.
func TestErr_WalkFailureNeverBecomesAnEmptySuccess(t *testing.T) {
	bundle := newFakeBundle(t, nil, nil, emptyDynamic())
	failListReactor(t, bundle, "pods", fmt.Errorf("the API server is unreachable"))

	res, err := walkCluster(t.Context(), bundle, framework.ForeignContext{})
	require.Error(t, err)
	assert.Empty(t, res.Nodes, "a failed walk returns no result at all")
	assert.False(t, res.Complete.IsAsserted(),
		"and asserts nothing about completeness, so no caller can read it as a complete empty walk")
}

// TestErr_CancellationStopsTheWalkRatherThanOnlyBeingReported is the row that
// observes the PER-KIND cancellation check.
//
// WHY IT IS SEPARATE FROM THE ROW ABOVE, and again this is the mutation pass
// speaking: deleting the per-kind check left that row green, because the final
// check in walkCluster still returns the cancellation. What the per-kind check
// decides is not WHETHER the error is returned but HOW MUCH WORK a cancelled
// collect does — a collect cancelled at the second of twenty-six kinds must
// stop, not run twenty-four more listings against an API server the caller has
// already walked away from.
func TestErr_CancellationStopsTheWalkRatherThanOnlyBeingReported(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	bundle := newFakeBundle(t, nil, nil, emptyDynamic())

	fake, ok := bundle.typed.(interface {
		PrependReactor(verb, resource string, reaction k8stesting.ReactionFunc)
	})
	require.True(t, ok)

	listed := 0
	fake.PrependReactor("list", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		listed++
		// "nodes" is the SECOND kind the walk enumerates.
		if listed == 2 {
			cancel()
		}
		return false, nil, nil
	})

	_, err := walkCluster(ctx, bundle, framework.ForeignContext{})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.LessOrEqual(t, listed, 3,
		"a collect cancelled at the second kind stops there; it must not run the "+
			"remaining kinds against an API server the caller has walked away from")

	// SAME-RUN CONTROL: an uncancelled walk over the same fixture reaches every
	// typed kind, so the bound above is a real bound rather than a fixture that
	// never got going.
	control := newFakeBundle(t, nil, nil, emptyDynamic())
	controlFake, ok := control.typed.(interface {
		PrependReactor(verb, resource string, reaction k8stesting.ReactionFunc)
	})
	require.True(t, ok)
	controlListed := 0
	controlFake.PrependReactor("list", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		controlListed++
		return false, nil, nil
	})
	_, err = walkCluster(t.Context(), control, framework.ForeignContext{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, controlListed, 25,
		"the uncancelled walk reaches every typed kind")
}
