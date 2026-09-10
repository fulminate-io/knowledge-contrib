// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// crd_extractors.go — the NINE API GROUPS whose custom resources state a
// relationship this graph can hold, and the field each one reads.
//
// FOUR OF THE PARITY FLOOR'S EDGE TYPES HAVE NO OTHER PRODUCER ANYWHERE:
// ISSUED_BY, REFERENCES_STORE, USES_MIDDLEWARE and TARGETS exist only here.
// Three more — ROUTES_TO, MOUNTS_SECRET and SCALES — are produced here as well
// as by a fixed kind.
//
// FOUR OF THE NINE MAPPINGS ARE NOT WHAT A READER WOULD GUESS, and each is
// stated at its own extractor: external-secrets emits REFERENCES_STORE and NO
// MOUNTS_SECRET; the two Flux controllers emit TARGETS ONLY; and the Flux
// SOURCE controller emits MOUNTS_SECRET only. A fixture keyed on a guessed map
// reds a correct extractor.
//
// EIGHT OF THE NINE RETURN NOTHING WHEN THE FIELD THEY READ IS ABSENT. The
// exception is argoproj.io, which has TWO INDEPENDENT emission paths: an
// Application with no spec.destination.namespace still emits TARGETS for
// everything in its status.resources. A fixture that drops the destination and
// asserts zero edges reds a correct extractor.
//
// A KIND DEFAULT IS NOT A GUARD. Three of these fields carry an optional kind
// whose absence means a specific default; the default changes the TARGET ID and
// never suppresses the edge.

// crdExtractors is the registry, as a package-level literal.
//
// IT IS A MAP LITERAL RATHER THAN A REGISTER FUNCTION, and that is a deliberate
// divergence from the built-in collector. Its registry panics on an empty
// group, a nil extractor and a duplicate registration; here a duplicate key is
// a COMPILE error, an empty key would be visible on the line, and there is no
// init-time panic left to kill a provider whose whole surface is one tool call.
var crdExtractors = map[string]crdExtractor{
	"cert-manager.io":             extractCertManager,
	"external-secrets.io":         extractExternalSecrets,
	"networking.istio.io":         extractIstioNetworking,
	"traefik.io":                  extractTraefik,
	"argoproj.io":                 extractArgoCD,
	"kustomize.toolkit.fluxcd.io": extractFluxKustomization,
	"helm.toolkit.fluxcd.io":      extractFluxHelmRelease,
	"source.toolkit.fluxcd.io":    extractFluxSource,
	"keda.sh":                     extractKEDA,
}

// extractCertManager: a Certificate is ISSUED_BY the issuer its issuerRef
// names.
//
// THE REFERENCE MAY BE CLUSTER-SCOPED. issuerRef.kind defaults to Issuer, which
// is namespaced; ClusterIssuer is not, so its target id carries no namespace. A
// fixture exercising only the default never sees that branch.
func extractCertManager(item *unstructured.Unstructured, kind string) []Edge {
	name, ok, _ := unstructured.NestedString(item.Object, "spec", "issuerRef", "name")
	if !ok || name == "" {
		return nil
	}
	refKind, _, _ := unstructured.NestedString(item.Object, "spec", "issuerRef", "kind")
	if refKind == "" {
		refKind = "Issuer"
	}
	return []Edge{{
		FromID: resourceID(item.GetNamespace(), kind, item.GetName()),
		ToID:   resourceID(scopedNamespace(refKind, item.GetNamespace()), refKind, name),
		Type:   edgeIssuedBy,
	}}
}

// extractExternalSecrets: an ExternalSecret REFERENCES_STORE.
//
// IT EMITS NO MOUNTS_SECRET. The Secret an ExternalSecret creates does not
// exist until the controller reconciles it, and the relationship the object
// states is to the STORE it reads from; a MOUNTS_SECRET edge here would name a
// Secret the object does not reference.
//
// The same cluster-scoped branch as cert-manager: secretStoreRef.kind defaults
// to SecretStore, and ClusterSecretStore is cluster-scoped.
func extractExternalSecrets(item *unstructured.Unstructured, kind string) []Edge {
	name, ok, _ := unstructured.NestedString(item.Object, "spec", "secretStoreRef", "name")
	if !ok || name == "" {
		return nil
	}
	refKind, _, _ := unstructured.NestedString(item.Object, "spec", "secretStoreRef", "kind")
	if refKind == "" {
		refKind = "SecretStore"
	}
	return []Edge{{
		FromID: resourceID(item.GetNamespace(), kind, item.GetName()),
		ToID:   resourceID(scopedNamespace(refKind, item.GetNamespace()), refKind, name),
		Type:   edgeReferencesStore,
	}}
}

// extractIstioNetworking: a VirtualService ROUTES_TO the Services its
// destinations name, across both the http and the tcp route blocks.
func extractIstioNetworking(item *unstructured.Unstructured, kind string) []Edge {
	sourceID := resourceID(item.GetNamespace(), kind, item.GetName())
	var out []Edge
	seen := map[string]bool{}
	for _, protocol := range []string{"http", "tcp"} {
		routes, ok := nestedSlice(item.Object, "spec", protocol)
		if !ok {
			continue
		}
		for _, route := range routes {
			routeMap, ok := route.(map[string]any)
			if !ok {
				continue
			}
			entries, ok := routeMap["route"].([]any)
			if !ok {
				continue
			}
			for _, entry := range entries {
				entryMap, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				dest, ok := entryMap["destination"].(map[string]any)
				if !ok {
					continue
				}
				host, _ := dest["host"].(string)
				if host == "" {
					continue
				}
				svc, ns := parseIstioHost(host, item.GetNamespace())
				target := resourceID(ns, "Service", svc)
				if seen[target] {
					continue
				}
				seen[target] = true
				out = append(out, Edge{FromID: sourceID, ToID: target, Type: edgeRoutesTo})
			}
		}
	}
	return out
}

// parseIstioHost resolves an Istio destination host to a Service name and
// namespace. Istio admits three spellings and they mean different things:
//
//	"reviews"                              the local namespace
//	"reviews.prod"                         the prod namespace
//	"reviews.prod.svc.cluster.local"       the same, fully qualified
//
// A short name resolved against the wrong namespace points at a Service that
// may well exist elsewhere, so the edge would be wrong rather than absent.
func parseIstioHost(host, defaultNamespace string) (name, namespace string) {
	parts := strings.Split(host, ".")
	switch {
	case len(parts) == 1:
		return parts[0], defaultNamespace
	case len(parts) >= 2:
		return parts[0], parts[1]
	default:
		return host, defaultNamespace
	}
}

// extractTraefik: an IngressRoute ROUTES_TO its services and USES_MIDDLEWARE
// for its middlewares. One object states BOTH relationships, which is why this
// group is the only one in the registry mapped to two edge types.
func extractTraefik(item *unstructured.Unstructured, kind string) []Edge {
	routes, ok := nestedSlice(item.Object, "spec", "routes")
	if !ok {
		return nil
	}
	sourceID := resourceID(item.GetNamespace(), kind, item.GetName())
	var out []Edge
	for _, route := range routes {
		routeMap, ok := route.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, traefikRouteRefs(sourceID, item.GetNamespace(), routeMap, "services", "Service", edgeRoutesTo)...)
		out = append(out, traefikRouteRefs(sourceID, item.GetNamespace(), routeMap, "middlewares", "Middleware", edgeUsesMiddleware)...)
	}
	return out
}

func traefikRouteRefs(sourceID, defaultNS string, routeMap map[string]any, key, kind, edgeType string) []Edge {
	items, ok := routeMap[key].([]any)
	if !ok {
		return nil
	}
	var out []Edge
	for _, raw := range items {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		ns, _ := m["namespace"].(string)
		if ns == "" {
			ns = defaultNS
		}
		out = append(out, Edge{FromID: sourceID, ToID: resourceID(ns, kind, name), Type: edgeType})
	}
	return out
}

// extractArgoCD: an Application TARGETS its destination namespace and every
// resource its status says it manages.
//
// THIS IS THE ONE EXTRACTOR WITH TWO INDEPENDENT PATHS. An Application with no
// spec.destination.namespace still emits TARGETS for its status resources, so a
// fixture that removes the destination and asserts silence reds a correct
// extractor.
func extractArgoCD(item *unstructured.Unstructured, kind string) []Edge {
	sourceID := resourceID(item.GetNamespace(), kind, item.GetName())
	var out []Edge

	destNS, ok, _ := unstructured.NestedString(item.Object, "spec", "destination", "namespace")
	if ok && destNS != "" {
		out = append(out, Edge{FromID: sourceID, ToID: resourceID("", "Namespace", destNS), Type: edgeTargets})
	}

	resources, ok := nestedSlice(item.Object, "status", "resources")
	if !ok {
		return out
	}
	for _, raw := range resources {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		resKind, _ := m["kind"].(string)
		name, _ := m["name"].(string)
		if resKind == "" || name == "" {
			continue
		}
		ns, _ := m["namespace"].(string)
		if ns == "" {
			ns = destNS
		}
		out = append(out, Edge{
			FromID: sourceID,
			ToID:   resourceID(scopedNamespace(resKind, ns), resKind, name),
			Type:   edgeTargets,
		})
	}
	return out
}

// extractFluxKustomization: a Kustomization TARGETS the source it renders from.
// TARGETS ONLY — no MOUNTS_SECRET, even though the source may itself carry a
// secret reference; that relationship belongs to the source object.
func extractFluxKustomization(item *unstructured.Unstructured, kind string) []Edge {
	return fluxSourceRefEdges(item, kind, "spec", "sourceRef")
}

// extractFluxHelmRelease: a HelmRelease TARGETS the chart source it installs
// from. The reference sits one level deeper than a Kustomization's.
func extractFluxHelmRelease(item *unstructured.Unstructured, kind string) []Edge {
	return fluxSourceRefEdges(item, kind, "spec", "chart", "spec", "sourceRef")
}

// fluxSourceRefEdges renders TARGETS from a Flux sourceRef.
//
// BOTH kind AND name ARE REQUIRED. A sourceRef with only a name names an object
// whose type is unknown, and guessing one would mint an id for a kind the
// cluster may not even serve.
func fluxSourceRefEdges(item *unstructured.Unstructured, kind string, fields ...string) []Edge {
	refKind, _, _ := unstructured.NestedString(item.Object, append(append([]string{}, fields...), "kind")...)
	name, _, _ := unstructured.NestedString(item.Object, append(append([]string{}, fields...), "name")...)
	if refKind == "" || name == "" {
		return nil
	}
	ns, _, _ := unstructured.NestedString(item.Object, append(append([]string{}, fields...), "namespace")...)
	if ns == "" {
		ns = item.GetNamespace()
	}
	return []Edge{{
		FromID: resourceID(item.GetNamespace(), kind, item.GetName()),
		ToID:   resourceID(ns, refKind, name),
		Type:   edgeTargets,
	}}
}

// extractFluxSource: a GitRepository or OCIRepository MOUNTS_SECRET for the
// credentials it authenticates with. MOUNTS_SECRET ONLY — a source object is
// the far end of a Kustomization's TARGETS edge, never the near end of one.
func extractFluxSource(item *unstructured.Unstructured, kind string) []Edge {
	name, ok, _ := unstructured.NestedString(item.Object, "spec", "secretRef", "name")
	if !ok || name == "" {
		return nil
	}
	return []Edge{{
		FromID: resourceID(item.GetNamespace(), kind, item.GetName()),
		ToID:   resourceID(item.GetNamespace(), "Secret", name),
		Type:   edgeMountsSecret,
	}}
}

// extractKEDA: a ScaledObject SCALES its target workload.
//
// scaleTargetRef.kind DEFAULTS TO Deployment. That default changes the target
// id and never suppresses the edge.
func extractKEDA(item *unstructured.Unstructured, kind string) []Edge {
	name, ok, _ := unstructured.NestedString(item.Object, "spec", "scaleTargetRef", "name")
	if !ok || name == "" {
		return nil
	}
	refKind, _, _ := unstructured.NestedString(item.Object, "spec", "scaleTargetRef", "kind")
	if refKind == "" {
		refKind = "Deployment"
	}
	return []Edge{{
		FromID: resourceID(item.GetNamespace(), kind, item.GetName()),
		ToID:   resourceID(item.GetNamespace(), refKind, name),
		Type:   edgeScales,
	}}
}

// scopedNamespace returns the namespace a reference's target id carries: none
// for a cluster-scoped kind, the supplied one otherwise.
//
// THE CLUSTER-SCOPED SET INCLUDES THE CRD KINDS THESE EXTRACTORS REFER TO, not
// only the fixed floor's, because a ClusterIssuer and a ClusterSecretStore are
// custom resources this collector may never have enumerated and still has to
// build a correct id for.
func scopedNamespace(kind, namespace string) string {
	if clusterScopedKinds[kind] || clusterScopedCRDKinds[kind] {
		return ""
	}
	return namespace
}

// clusterScopedCRDKinds is the set of CUSTOM resource kinds these extractors
// reference that are cluster-scoped.
var clusterScopedCRDKinds = map[string]bool{
	"ClusterIssuer":      true,
	"ClusterSecretStore": true,
}
