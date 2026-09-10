// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
)

// crd_test.go — R5-e, R5-f and R5-f2: the custom-resource half of the kind
// floor, and the nine API groups whose instances state a relationship.
//
// THE FIXTURE SPEC IS PART OF THE ROW. Eight of the nine extractors return
// nothing when the field they read is absent, so a bare instance of every group
// produces zero edges from all nine — which is the control that proves the
// FIXTURES carry the inputs, not that the extractors work.
//
// FOUR OF THE NINE MAPPINGS ARE NOT WHAT A READER WOULD GUESS, and a fixture
// keyed on a guessed map reds a correct module: external-secrets emits
// REFERENCES_STORE and NO MOUNTS_SECRET, the two Flux workload controllers emit
// TARGETS ONLY, and the Flux SOURCE controller emits MOUNTS_SECRET only.

// crdCase is one group's fixture: an instance carrying the field its extractor
// reads, and the edge that instance must produce.
type crdCase struct {
	group      string
	version    string
	plural     string
	kind       string
	namespaced bool
	spec       map[string]any
	wantType   string
	wantTarget string
}

func crdCases() []crdCase {
	return []crdCase{
		{
			group: "cert-manager.io", version: "v1", plural: "certificates", kind: "Certificate", namespaced: true,
			spec:       map[string]any{"spec": map[string]any{"issuerRef": map[string]any{"name": "letsencrypt"}}},
			wantType:   edgeIssuedBy,
			wantTarget: "prod/Issuer/letsencrypt", // kind DEFAULTS to Issuer
		},
		{
			group: "external-secrets.io", version: "v1beta1", plural: "externalsecrets", kind: "ExternalSecret", namespaced: true,
			spec:       map[string]any{"spec": map[string]any{"secretStoreRef": map[string]any{"name": "vault"}}},
			wantType:   edgeReferencesStore, // NOT MountsSecret
			wantTarget: "prod/SecretStore/vault",
		},
		{
			group: "networking.istio.io", version: "v1", plural: "virtualservices", kind: "VirtualService", namespaced: true,
			spec: map[string]any{"spec": map[string]any{"http": []any{
				map[string]any{"route": []any{
					map[string]any{"destination": map[string]any{"host": "reviews"}}}}}}},
			wantType:   edgeRoutesTo,
			wantTarget: "prod/Service/reviews",
		},
		{
			group: "traefik.io", version: "v1alpha1", plural: "ingressroutes", kind: "IngressRoute", namespaced: true,
			spec: map[string]any{"spec": map[string]any{"routes": []any{
				map[string]any{
					"services":    []any{map[string]any{"name": "api"}},
					"middlewares": []any{map[string]any{"name": "auth"}},
				}}}},
			wantType:   edgeRoutesTo,
			wantTarget: "prod/Service/api",
		},
		{
			group: "argoproj.io", version: "v1alpha1", plural: "applications", kind: "Application", namespaced: true,
			spec:       map[string]any{"spec": map[string]any{"destination": map[string]any{"namespace": "staging"}}},
			wantType:   edgeTargets,
			wantTarget: "Namespace/staging",
		},
		{
			group: "kustomize.toolkit.fluxcd.io", version: "v1", plural: "kustomizations", kind: "Kustomization", namespaced: true,
			spec: map[string]any{"spec": map[string]any{
				"sourceRef": map[string]any{"kind": "GitRepository", "name": "infra"}}},
			wantType:   edgeTargets, // TARGETS ONLY
			wantTarget: "prod/GitRepository/infra",
		},
		{
			group: "helm.toolkit.fluxcd.io", version: "v2", plural: "helmreleases", kind: "HelmRelease", namespaced: true,
			spec: map[string]any{"spec": map[string]any{"chart": map[string]any{"spec": map[string]any{
				"sourceRef": map[string]any{"kind": "HelmRepository", "name": "charts"}}}}},
			wantType:   edgeTargets, // TARGETS ONLY, one level deeper
			wantTarget: "prod/HelmRepository/charts",
		},
		{
			group: "source.toolkit.fluxcd.io", version: "v1", plural: "gitrepositories", kind: "GitRepository", namespaced: true,
			spec:       map[string]any{"spec": map[string]any{"secretRef": map[string]any{"name": "git-creds"}}},
			wantType:   edgeMountsSecret, // MOUNTS_SECRET ONLY, never TARGETS
			wantTarget: "prod/Secret/git-creds",
		},
		{
			group: "keda.sh", version: "v1alpha1", plural: "scaledobjects", kind: "ScaledObject", namespaced: true,
			spec:       map[string]any{"spec": map[string]any{"scaleTargetRef": map[string]any{"name": "api"}}},
			wantType:   edgeScales,
			wantTarget: "prod/Deployment/api", // kind DEFAULTS to Deployment
		},
	}
}

func crdBundle(t *testing.T, cases []crdCase, withSpec bool) clientBundle {
	t.Helper()
	var crds []runtime.Object
	var instances []runtime.Object
	listKinds := emptyDynamic()
	for _, tc := range cases {
		crds = append(crds, crdFor(tc.group, tc.version, tc.plural, tc.kind, tc.namespaced))
		gvr := schema.GroupVersionResource{Group: tc.group, Version: tc.version, Resource: tc.plural}
		listKinds[gvr] = tc.kind + "List"
		spec := tc.spec
		if !withSpec {
			spec = nil
		}
		instances = append(instances,
			customResource(tc.group+"/"+tc.version, tc.kind, "prod", "instance-"+tc.plural, spec))
	}
	return newFakeBundle(t, nil, crds, listKinds, instances...)
}

// TestCRD_NineGroupsEmitTheirMappedEdge is R5-f. Each group's instance carries
// the field its extractor reads; each must produce ITS mapped edge and no
// other's.
func TestCRD_NineGroupsEmitTheirMappedEdge(t *testing.T) {
	cases := crdCases()
	_, edges, _ := runWalk(t, crdBundle(t, cases, true))

	for _, tc := range cases {
		t.Run(tc.group, func(t *testing.T) {
			from := resourceID("prod", tc.kind, "instance-"+tc.plural)
			assert.True(t, hasEdge(edges, from, tc.wantTarget, tc.wantType),
				"%s must emit %s to %s", tc.group, tc.wantType, tc.wantTarget)
		})
	}

	// The two edge types that would appear if the guessed map were used.
	assert.False(t, hasEdge(edges, "prod/ExternalSecret/instance-externalsecrets",
		"prod/Secret/vault", edgeMountsSecret),
		"external-secrets emits REFERENCES_STORE, never MOUNTS_SECRET")
	assert.False(t, hasEdge(edges, "prod/GitRepository/instance-gitrepositories",
		"prod/Secret/git-creds", edgeTargets),
		"the Flux source controller emits MOUNTS_SECRET only, never TARGETS")
}

// TestCRD_BareInstancesOfEveryGroupEmitNothing is R5-f's control: it proves the
// FIXTURES above carry the inputs rather than the extractors emitting
// unconditionally.
func TestCRD_BareInstancesOfEveryGroupEmitNothing(t *testing.T) {
	_, edges, _ := runWalk(t, crdBundle(t, crdCases(), false))
	for _, e := range edges {
		assert.Equal(t, edgeOwnedBy, e.Type,
			"a bare instance states no relationship except an owner reference; got %s -> %s (%s)",
			e.FromID, e.ToID, e.Type)
	}
}

// TestCRD_ArgoIsTheExceptionWithTwoIndependentPaths. An Application with NO
// spec.destination.namespace STILL emits TARGETS for its status resources. A
// fixture that drops the destination and asserts zero edges reds a correct
// extractor.
func TestCRD_ArgoIsTheExceptionWithTwoIndependentPaths(t *testing.T) {
	app := customResource("argoproj.io/v1alpha1", "Application", "argocd", "web", map[string]any{
		"status": map[string]any{"resources": []any{
			map[string]any{"kind": "Deployment", "name": "web", "namespace": "prod"},
			// An entry missing its kind names nothing.
			map[string]any{"name": "incomplete"},
		}},
	})
	edges := extractArgoCD(app, "Application")
	assert.True(t, hasEdge(edges, "argocd/Application/web", "prod/Deployment/web", edgeTargets),
		"the status path emits with no spec.destination at all")
	assert.Len(t, edges, 1, "an entry with no kind names nothing")
}

// TestCRD_ClusterScopedEdgeTargets is R5-f2: the two groups whose reference
// kind decides whether the target id carries a namespace.
func TestCRD_ClusterScopedEdgeTargets(t *testing.T) {
	cert := customResource("cert-manager.io/v1", "Certificate", "prod", "tls", map[string]any{
		"spec": map[string]any{"issuerRef": map[string]any{"kind": "ClusterIssuer", "name": "letsencrypt"}},
	})
	assert.True(t, hasEdge(extractCertManager(cert, "Certificate"),
		"prod/Certificate/tls", "ClusterIssuer/letsencrypt", edgeIssuedBy),
		"a ClusterIssuer reference is cluster-scoped and carries no namespace")

	es := customResource("external-secrets.io/v1beta1", "ExternalSecret", "prod", "db", map[string]any{
		"spec": map[string]any{"secretStoreRef": map[string]any{"kind": "ClusterSecretStore", "name": "vault"}},
	})
	assert.True(t, hasEdge(extractExternalSecrets(es, "ExternalSecret"),
		"prod/ExternalSecret/db", "ClusterSecretStore/vault", edgeReferencesStore))

	// SAME-RUN CONTROL: the namespaced default still carries the namespace.
	certDefault := customResource("cert-manager.io/v1", "Certificate", "prod", "tls2", map[string]any{
		"spec": map[string]any{"issuerRef": map[string]any{"name": "internal"}},
	})
	assert.True(t, hasEdge(extractCertManager(certDefault, "Certificate"),
		"prod/Certificate/tls2", "prod/Issuer/internal", edgeIssuedBy))
}

// TestCRD_InstanceKindComesFromTheInstance is R5-e. The definition's name is a
// plural resource name qualified by its group; the instances carry the Kind.
func TestCRD_InstanceKindComesFromTheInstance(t *testing.T) {
	crd := crdFor("cert-manager.io", "v1", "certificates", "Certificate", true)
	gvr := schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}
	listKinds := emptyDynamic()
	listKinds[gvr] = "CertificateList"
	instance := customResource("cert-manager.io/v1", "Certificate", "prod", "tls", nil)

	nodes, _, _ := runWalk(t, newFakeBundle(t, nil, []runtime.Object{crd}, listKinds, instance))

	got := nodeByID(t, nodes, "prod/Certificate/tls")
	assert.Equal(t, "Certificate", got.Metadata["resource_type"],
		"the kind is the INSTANCE's Kind, not the definition's name")
	assert.Equal(t, "cert-manager.io", got.Metadata["crd_group"])

	// And the definition itself is a node too, under its own kind.
	def := nodeByID(t, nodes, "CustomResourceDefinition/certificates.cert-manager.io")
	assert.Equal(t, "Certificate", def.Metadata["crd_kind"])
}

// TestCRD_InstanceKindDiffersFromTheDefinitionsDeclaredKind is T3-3.
//
// THE ROW ABOVE SEPARATES THE INSTANCE KIND FROM THE DEFINITION'S NAME AND NOT
// FROM ITS DECLARED KIND, because its fixture spells both "Certificate" —
// substituting crd.Spec.Names.Kind for the instance's own Kind left it green.
// Here the two differ, so only reading the INSTANCE gives the right answer.
//
// THE CASE IS REAL RATHER THAN CONTRIVED: a definition's declared kind is what
// the API server will serve, and a cluster mid-migration, or one whose
// definition was edited after instances existed, serves objects whose Kind is
// not the one the current definition declares.
//
// IT IS STAGED WITH A REACTOR, NOT WITH THE TRACKER. The dynamic fake derives a
// GVR from an object's own apiVersion and kind, so an instance whose kind is
// not the definition's plural cannot be put in the tracker under the resource
// the walk lists. A reactor answers the list the walk actually makes.
func TestCRD_InstanceKindDiffersFromTheDefinitionsDeclaredKind(t *testing.T) {
	nodes := walkOneCustomResource(t, "DeclaredKind", "InstanceKind")

	got := nodeByID(t, nodes, "prod/InstanceKind/w1")
	assert.Equal(t, "InstanceKind", got.Metadata["resource_type"],
		"the kind is the INSTANCE's own, not the definition's declared kind")
	for _, n := range nodes {
		assert.NotEqual(t, "prod/DeclaredKind/w1", n.ID,
			"no node is minted under the definition's declared kind")
	}
}

// TestCRD_InstanceWithNoKindFallsBackToTheDefinition covers the fallback arm,
// which had no test at all. An object still has to become a node under some
// kind, or it vanishes from a walk that claims to have enumerated it.
func TestCRD_InstanceWithNoKindFallsBackToTheDefinition(t *testing.T) {
	nodes := walkOneCustomResource(t, "Widget", "")

	got := nodeByID(t, nodes, "prod/Widget/w1")
	assert.Equal(t, "Widget", got.Metadata["resource_type"],
		"an instance with no kind falls back to the definition's declared kind")
}

// walkOneCustomResource runs a whole walk over one custom resource definition
// declaring declaredKind, served by one instance whose own kind is
// instanceKind, and returns the nodes.
func walkOneCustomResource(t *testing.T, declaredKind, instanceKind string) []Node {
	t.Helper()
	const group, version, plural = "example.com", "v1", "widgets"
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: plural}
	listKinds := emptyDynamic()
	listKinds[gvr] = declaredKind + "List"

	bundle := newFakeBundle(t, nil,
		[]runtime.Object{crdFor(group, version, plural, declaredKind, true)}, listKinds)

	item := customResource(group+"/"+version, instanceKind, "prod", "w1", nil)
	if instanceKind == "" {
		// customResource always writes a kind; remove it so the object really
		// carries none, which is the arm under test.
		delete(item.Object, "kind")
	}

	fake, ok := bundle.dynamic.(interface {
		PrependReactor(verb, resource string, reaction k8stesting.ReactionFunc)
	})
	require.True(t, ok)
	fake.PrependReactor("list", plural, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.UnstructuredList{
			Object: map[string]any{"apiVersion": group + "/" + version, "kind": declaredKind + "List"},
			Items:  []unstructured.Unstructured{*item},
		}, nil
	})

	nodes, _, _ := runWalk(t, bundle)
	return nodes
}

// TestCRD_IstioHostFormats covers the three spellings an Istio destination host
// takes. A short name resolved against the wrong namespace points at a Service
// that may well exist elsewhere, so the edge would be WRONG rather than absent.
func TestCRD_IstioHostFormats(t *testing.T) {
	cases := map[string]string{
		"reviews":                           "prod/Service/reviews",
		"reviews.staging":                   "staging/Service/reviews",
		"reviews.staging.svc.cluster.local": "staging/Service/reviews",
	}
	for host, want := range cases {
		vs := customResource("networking.istio.io/v1", "VirtualService", "prod", "vs", map[string]any{
			"spec": map[string]any{"http": []any{map[string]any{"route": []any{
				map[string]any{"destination": map[string]any{"host": host}}}}}},
		})
		edges := extractIstioNetworking(vs, "VirtualService")
		require.Len(t, edges, 1, "host %q", host)
		assert.Equal(t, want, edges[0].ToID, "host %q", host)
	}
}

// TestCRD_TraefikEmitsBothItsEdgeTypes. This group is the only one in the
// registry mapped to two edge types from one object.
func TestCRD_TraefikEmitsBothItsEdgeTypes(t *testing.T) {
	route := customResource("traefik.io/v1alpha1", "IngressRoute", "prod", "web", map[string]any{
		"spec": map[string]any{"routes": []any{map[string]any{
			"services":    []any{map[string]any{"name": "api"}},
			"middlewares": []any{map[string]any{"name": "auth"}},
		}}},
	})
	edges := extractTraefik(route, "IngressRoute")
	assert.True(t, hasEdge(edges, "prod/IngressRoute/web", "prod/Service/api", edgeRoutesTo))
	assert.True(t, hasEdge(edges, "prod/IngressRoute/web", "prod/Middleware/auth", edgeUsesMiddleware))
}

// TestCRD_UnregisteredGroupContributesNodesOnly. Most custom resources describe
// something this graph has no other node for, and that is not an error.
func TestCRD_UnregisteredGroupContributesNodesOnly(t *testing.T) {
	crd := crdFor("example.com", "v1", "widgets", "Widget", true)
	gvr := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}
	listKinds := emptyDynamic()
	listKinds[gvr] = "WidgetList"
	instance := customResource("example.com/v1", "Widget", "prod", "w1", map[string]any{
		"spec": map[string]any{"anything": "at all"},
	})

	nodes, edges, reason := runWalk(t, newFakeBundle(t, nil, []runtime.Object{crd}, listKinds, instance))
	nodeByID(t, nodes, "prod/Widget/w1")
	assert.Empty(t, edges, "an unregistered group states no relationship this graph holds")
	assert.Empty(t, reason, "and a group with no extractor is not an incomplete walk")
}

// TestCRD_ExtractorRegistryIsALookupNotAPanic. The built-in collector's
// equivalent panics on a duplicate registration; here the table is a map
// literal, so a duplicate key is a COMPILE error and there is nothing to panic
// about at runtime. A lookup for an unknown group returns nil.
func TestCRD_ExtractorRegistryIsALookupNotAPanic(t *testing.T) {
	assert.Nil(t, crdExtractorFor("nobody.example.com"))
	assert.Nil(t, crdExtractorFor(""))
	assert.NotNil(t, crdExtractorFor("keda.sh"))
	assert.Len(t, crdExtractors, 9, "nine groups are registered")
}

// TestCRD_ServedVersionPrefersStorage pins the version this walk reads. The
// storage version is the one every other version is converted from, so reading
// it is what makes two collects of an unchanged cluster produce the same body.
func TestCRD_ServedVersionPrefersStorage(t *testing.T) {
	crd := crdFor("example.com", "v1beta1", "widgets", "Widget", true)
	crd.Spec.Versions = []apiextv1.CustomResourceDefinitionVersion{
		{Name: "v1alpha1", Served: true, Storage: false},
		{Name: "v1", Served: true, Storage: true},
		{Name: "v2", Served: false, Storage: false},
	}
	gvr, ok := servedGVR(crd)
	require.True(t, ok)
	assert.Equal(t, "v1", gvr.Version, "the storage version is preferred over the first served one")

	// A definition whose only version is NOT served resolves to nothing, and
	// the walk records that rather than passing over it.
	crd.Spec.Versions = []apiextv1.CustomResourceDefinitionVersion{
		{Name: "v1", Served: false, Storage: true},
	}
	_, ok = servedGVR(crd)
	assert.False(t, ok)
}
