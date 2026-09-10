// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// node.go — HOW A RESOURCE BECOMES A NODE. One constructor, used by every
// service walk, so the metadata keys a consumer selects on are written once.

// resource describes one AWS resource a service walk found. It is the walk's own
// vocabulary: the walk fills it from the SDK's types and hands it to newNode,
// which owns the mapping onto the contract's node shape.
type resource struct {
	// id is the node id: the real ARN wherever the API returns one. See arn.go.
	id string
	// resourceType is one of the values in vocab.go.
	resourceType string
	// name is the operator-facing name — an instance's Name tag, a bucket's
	// name, a role's role name. It rides in symbol_name, which is the field the
	// graph's own renderers and searches read as "what is this called".
	name string
	// summary is one line about the resource, written for retrieval rather than
	// for display: it is what search matches on.
	summary string
	// detail is everything else worth carrying, rendered into content as sorted
	// JSON. It holds the SDK's own values verbatim; nothing derived and nothing
	// that varies between two collects of an unchanged account.
	detail map[string]string
	// region is the region the resource lives in. Empty for a global service
	// (IAM, S3 buckets, CloudFront, Route 53), which is a fact about the
	// resource and not a missing value.
	region string
}

// newNode maps one resource onto the contract's node shape.
//
// EVERY NODE CARRIES resource_type AND account; region only when the resource
// has one. An empty-string region metadata value would read to a consumer as a
// region named "", which is a different claim from a global resource having no
// region at all, so the key is omitted instead.
func newNode(r resource, account string) framework.Node {
	meta := map[string]string{
		MetaResourceType: r.resourceType,
		MetaAccount:      account,
	}
	if r.region != "" {
		meta[MetaRegion] = r.region
	}
	return framework.Node{
		ID:         r.id,
		Type:       NodeTypeCloudResource,
		SymbolName: r.name,
		Summary:    r.summary,
		Content:    marshalContent(r.detail),
		Metadata:   meta,
		Source:     "aws",
	}
}

// deref returns the value a pointer holds, or the zero value when it is nil.
//
// THE AWS SDK MODELS ALMOST EVERY FIELD AS A POINTER, including ones the API
// always populates, so a walk that dereferenced them directly would panic on the
// first response missing an optional field — which is the "resource missing every
// optional field" class each service's tests carry.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// derefOr returns the value a pointer holds, or fallback when it is nil.
func derefOr[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}

// put writes a key into a detail map only when the value is non-empty.
//
// AN ABSENT DETAIL IS AN ABSENT KEY, never an empty string: the content blob is
// diffed across collects, and a key that appears with an empty value on one run
// and is absent on another is a change no resource had.
func put(m map[string]string, key, value string) {
	if value == "" {
		return
	}
	m[key] = value
}
