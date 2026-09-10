// SPDX-License-Identifier: Apache-2.0

package resolve

import (
	"fmt"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// MethodImageLineage is the method discriminator on a container image edge.
const MethodImageLineage = "gcp-image-lineage"

// registryKind classifies the host of a container image reference. Only the two
// kinds this collector can resolve are named; everything else is unknown, which
// is a legitimate answer and not a gap.
type registryKind int

const (
	registryUnknown registryKind = iota
	// registryArtifactRegistry is the current registry:
	// <region>-docker.pkg.dev/<project>/<repo>/<image>
	registryArtifactRegistry
	// registryLegacy is the predecessor Google is migrating INTO Artifact
	// Registry: gcr.io/<project>/<image> and its regional hosts. Its references
	// are resolved against the SAME repository index, which is why a module that
	// implements the current arm alone silently drops every legacy service's
	// edge while the edge type stays in the declared vocabulary.
	registryLegacy
)

// imageRef is a parsed container image reference: the host, the path under it,
// and the original string, which is what the edge's evidence carries.
type imageRef struct {
	registry   string
	repository string
	full       string
}

// CloudRunImages derives container image lineage from each service to the
// repository its image lives in, on BOTH arms of the registry-kind axis.
func CloudRunImages(in gcpgraph.Result) (gcpgraph.Result, error) {
	index := repositoryIndex(in.Resources)
	if len(index) == 0 {
		return gcpgraph.Result{}, nil
	}

	var out gcpgraph.Result
	seen := map[string]bool{}
	for _, res := range in.Resources {
		if res.ResourceType != gcpgraph.ResourceTypeRunService {
			continue
		}
		var spec gcpcontent.RunService
		present, err := gcpcontent.Unmarshal(res.Content, &spec)
		if err != nil {
			return gcpgraph.Result{}, fmt.Errorf(
				"gcp image-lineage resolver: reading service %q (id=%q): %w", res.Name, res.ID, err)
		}
		if !present {
			continue
		}
		for _, image := range spec.Images {
			target := matchRepository(parseImageRef(image), index)
			if target == "" {
				continue
			}
			// One service commonly runs several images out of one repository,
			// and they are one relationship, not several.
			key := res.ID + "|" + target
			if seen[key] {
				continue
			}
			seen[key] = true
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: res.ID, To: target, Type: gcpgraph.EdgeUsesImage,
				Method:   MethodImageLineage,
				Metadata: map[string]string{"image": image},
			})
		}
	}
	return out, nil
}

// repositoryIndex maps "<project>/<repository>" onto the repository's node id.
func repositoryIndex(resources []gcpgraph.Resource) map[string]string {
	index := map[string]string{}
	for _, res := range resources {
		if res.ResourceType != gcpgraph.ResourceTypeArtifactRegistryRepo {
			continue
		}
		project, repo := gcpgraph.ParseArtifactRegistryResourceID(res.ID)
		if project == "" || repo == "" {
			continue
		}
		index[project+"/"+repo] = res.ID
	}
	return index
}

// matchRepository resolves one image reference to a repository node id, or to
// empty when the image is on neither registry or in a repository this walk did
// not enumerate.
func matchRepository(ref imageRef, index map[string]string) string {
	switch ref.kind() {
	case registryArtifactRegistry:
		// <project>/<repo>/<image>: the first two segments name the repository.
		parts := strings.SplitN(ref.repository, "/", 3)
		if len(parts) < 2 {
			return ""
		}
		return index[parts[0]+"/"+parts[1]]
	case registryLegacy:
		// <project>/<image>: the legacy layout has no repository segment, so the
		// image name is matched against a repository of that name.
		parts := strings.SplitN(ref.repository, "/", 2)
		if len(parts) < 2 {
			return ""
		}
		return index[parts[0]+"/"+parts[1]]
	default:
		return ""
	}
}

func (ref imageRef) kind() registryKind {
	switch {
	case strings.HasSuffix(ref.registry, "-docker.pkg.dev"):
		return registryArtifactRegistry
	case ref.registry == "gcr.io" || strings.HasSuffix(ref.registry, ".gcr.io"):
		return registryLegacy
	default:
		return registryUnknown
	}
}

// parseImageRef splits a container image reference into its host and path,
// dropping the tag or digest. A reference with no host — the short form a public
// hub serves — parses with an empty registry, which classifies as unknown.
func parseImageRef(image string) imageRef {
	ref := imageRef{full: image}
	if image == "" {
		return ref
	}
	remaining := image
	if i := strings.Index(remaining, "@"); i >= 0 {
		remaining = remaining[:i]
	} else {
		remaining = trimTag(remaining)
	}
	// The first segment is a host only if it looks like one. A bare first
	// segment is a path component of the short form, not a registry.
	if host, rest, ok := strings.Cut(remaining, "/"); ok && strings.ContainsAny(host, ".:") {
		ref.registry = host
		ref.repository = rest
		return ref
	}
	ref.repository = remaining
	return ref
}

// trimTag removes a trailing :tag. A colon BEFORE the first slash is a port on
// the host and is left alone.
func trimTag(s string) string {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return s
	}
	if slash := strings.Index(s, "/"); slash >= 0 && i < slash {
		return s
	}
	return s[:i]
}
