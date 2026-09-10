// SPDX-License-Identifier: Apache-2.0

package azure

import "strings"

// resolve_image.go — RESOLVER 3: a site running a container image gets an edge
// to the registry serving that image.
//
// BOTH ENDPOINTS ARE IN THIS COLLECTOR'S OWN GRAPH. It is a site and a registry
// in the same subscription, not a reference into some other family's graph, so
// nothing here crosses a graph boundary.
//
// IT EMITS TO THE REGISTRY, NOT THE REPOSITORY. The registry is the resource
// Azure has an ARM id for; the repository inside it is not enumerated, and an
// edge naming one would point at a node this collector never emits.

// methodImageLineage marks an edge this resolver derived, and its evidence
// carries the image reference the site actually declared.
const methodImageLineage = "azure-image-lineage"

// resolveImageLineage returns one edge per site whose container image is served
// by a registry in this walk.
func resolveImageLineage(sites, functionApps, registries []resource) []edge {
	index := registryIndex(registries)
	if len(index) == 0 {
		return nil
	}

	var out []edge
	seen := map[string]struct{}{}
	for _, r := range append(append([]resource(nil), sites...), functionApps...) {
		if r.containerImage == "" {
			continue
		}
		host := imageRegistryHost(r.containerImage)
		if host == "" {
			continue
		}
		target, ok := index[host]
		if !ok {
			continue
		}
		key := r.id + "|" + target
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, edge{
			from:     r.id,
			to:       target,
			relation: edgeUsesImage,
			metadata: map[string]string{"image": r.containerImage},
			method:   methodImageLineage,
		})
	}
	return out
}

// registryIndex maps each registry's login server host to its id, lowercased
// because a registry hostname is case-insensitive while the site's declared
// image string preserves whatever the operator typed.
func registryIndex(registries []resource) map[string]string {
	index := make(map[string]string, len(registries))
	for _, r := range registries {
		host := strings.ToLower(r.acrLoginServer)
		if host == "" {
			continue
		}
		if _, dup := index[host]; dup {
			continue
		}
		index[host] = r.id
	}
	return index
}

// imageRegistryHost returns the registry host of an image reference, or "" when
// the reference names no host at all.
//
// The reference is "host/repository:tag" or "host/repository@digest"; a
// reference with no host ("nginx:latest") is a Docker Hub image, which this
// walk has no node for.
//
// IT DOES NOT CHECK THE HOST AGAINST A KNOWN AZURE SUFFIX, and the reason is a
// defect that check would cause rather than one it would prevent: a registry
// host is azurecr.io in the public cloud and azurecr.cn or azurecr.us in the
// sovereign ones, so a suffix test would silently refuse to draw lineage for
// every sovereign-cloud subscription. The INDEX is the discriminator instead —
// it holds exactly the registries this walk found, whatever their host — which
// is both narrower and correct in every cloud.
func imageRegistryHost(image string) string {
	host, _, found := strings.Cut(image, "/")
	if !found {
		return ""
	}
	return strings.ToLower(host)
}

// parseDockerFxVersion extracts the image from App Service's own encoding of a
// container image, "DOCKER|<image>". A site running code rather than a
// container carries a runtime stack there instead ("PYTHON|3.12") and yields
// nothing.
func parseDockerFxVersion(fxVersion string) string {
	const prefix = "DOCKER|"
	if len(fxVersion) < len(prefix) || !strings.EqualFold(fxVersion[:len(prefix)], prefix) {
		return ""
	}
	return fxVersion[len(prefix):]
}
