// SPDX-License-Identifier: Apache-2.0

package main

import (
	"maps"
	"testing"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

// fake_test.go — the FAKE CLIENTSET HARNESS every walk test drives.
//
// A FAKE CLIENTSET IS THE RIGHT INSTRUMENT HERE, and a recorded HTTP response
// is not. The subject of a converter test is what this collector does with an
// object, not how the API server serializes one; the fake takes a typed object
// literal, which is both cheaper to write and more precise about what varies.
//
// THE FAKE HAS ONE MEASURED BLIND SPOT AND IT IS NOT COSMETIC. Neither the
// typed nor the dynamic fake reads Limit or Continue out of ListOptions: the
// fake's option extractor names only the label, field and resourceVersion
// selectors, so every List returns the tracker's whole contents with an empty
// continuation token. A paging test written on the plain fake is therefore
// GREEN WHATEVER THE COLLECTOR DOES. cap_test.go builds the truncation with a
// reactor instead, and says so at its own fixture.

// newFakeBundle builds a client bundle over fake clientsets.
//
// dynamicObjects are handed to the dynamic fake with an explicit list-kind map,
// because the dynamic fake cannot infer the list kind for a resource it has no
// scheme entry for — and a custom resource is exactly that case.
// The bundle's contextName is left empty: no fixture here names a kubeconfig
// context, because the walk reads the context name only to stamp it onto the
// result and nothing in this suite asserts on that stamp.
func newFakeBundle(
	t *testing.T,
	typedObjects []runtime.Object,
	crds []runtime.Object,
	listKinds map[schema.GroupVersionResource]string,
	dynamicObjects ...runtime.Object,
) clientBundle {
	t.Helper()
	scheme := runtime.NewScheme()
	return clientBundle{
		typed:   kubefake.NewSimpleClientset(typedObjects...),
		apiext:  apiextfake.NewSimpleClientset(crds...),
		dynamic: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds, dynamicObjects...),
	}
}

// emptyDynamic is the list-kind map for a cluster serving no Gateway API, no
// AdminNetworkPolicy and no custom resources. It is NOT the empty map: the
// dynamic fake needs a list kind registered for every GVR a caller lists, and
// an unregistered one panics rather than returning a not-found the walk could
// record.
func emptyDynamic() map[schema.GroupVersionResource]string {
	return map[schema.GroupVersionResource]string{
		gatewayClassGVR:               "GatewayClassList",
		gatewayGVR:                    "GatewayList",
		httpRouteGVR:                  "HTTPRouteList",
		grpcRouteGVR:                  "GRPCRouteList",
		adminNetworkPolicyGVR:         "AdminNetworkPolicyList",
		baselineAdminNetworkPolicyGVR: "BaselineAdminNetworkPolicyList",
	}
}

// customResource builds one unstructured instance of a custom resource.
func customResource(apiVersion, kind, namespace, name string, spec map[string]any) *unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name": name,
		},
	}
	if namespace != "" {
		obj["metadata"].(map[string]any)["namespace"] = namespace
	}
	maps.Copy(obj, spec)
	return &unstructured.Unstructured{Object: obj}
}

// crdFor builds a CustomResourceDefinition serving one kind at one version.
func crdFor(group, version, plural, kind string, namespaced bool) *apiextv1.CustomResourceDefinition {
	scope := apiextv1.ClusterScoped
	if namespaced {
		scope = apiextv1.NamespaceScoped
	}
	return &apiextv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: plural + "." + group},
		Spec: apiextv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apiextv1.CustomResourceDefinitionNames{Plural: plural, Kind: kind},
			Scope: scope,
			Versions: []apiextv1.CustomResourceDefinitionVersion{
				{Name: version, Served: true, Storage: true},
			},
		},
	}
}
