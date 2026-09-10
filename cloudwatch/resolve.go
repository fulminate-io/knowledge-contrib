// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// resolve.go — TURNING SUPPLIED CLOUD RESOURCES INTO RESOLUTIONS, the input the
// proxy nodes and EMITTED_BY edges are built from.
//
// WHERE THE RESOURCES COME FROM. A collector process holds no cloud graph, so
// it cannot look one up; the client fills a declared block from the graphs this
// collector's configuration entry names, and the framework hands that block to
// the walk as an argument. WITH AN EMPTY BLOCK THIS COLLECTOR RESOLVES NOTHING
// AND EMITS NO PROXY, NO EMITTED_BY AND NO CORRELATES_WITH, which is exactly
// what the built-in path does when it runs with no cloud graph attached, so the
// empty case is parity rather than a gap.
//
// THE RESOURCE TYPE ARRIVES IN METADATA, NOT AS A FIELD. A declared node
// carries the fields the entry asked for, and the type this resolver ranks on
// is the cloud collector's own `resource_type` metadata key rather than the
// node's `type`, which is the graph-level kind. An entry that declares the node
// type but not that metadata key produces nodes this resolver cannot rank, and
// they simply do not match — see resourceTypeOf.

// CloudResource is one cloud-graph resource offered to the resolver.
type CloudResource struct {
	// Account is the cloud graph the resource lives in; it becomes the
	// account segment of the proxy id.
	Account string
	// ID is the resource's node id in that graph; it becomes the proxy's
	// foreign_id and the last segment of the proxy id.
	ID string
	// SymbolName is the resource's readable name, matched against a label
	// value case-insensitively.
	SymbolName string
	// ResourceType is the cloud collector's canonical type string, matched
	// against the prefix lists below.
	ResourceType string
}

// cloudContextFrom converts the framework's declared foreign block into this
// module's own resolution input.
//
// ONE ENTRY PER DECLARED CLOUD GRAPH becomes one account, because a proxy's
// identity is the account plus the resource id and the graph name IS the
// account. A declared CODE graph is ignored here: a log label resolves to a
// cloud resource, and this collector emits no code-graph proxy.
//
// IT NAMES WHAT TO EXCLUDE RATHER THAN WHAT TO INCLUDE, which is the whole
// shape of the map-keyed contract that replaced the block's `Cloud` field.
// There is no built-in cloud family any more: inventory is collected by contrib
// collectors, each registering its OWN graph type, so a declaration names the
// providers an operator installed and this collector cannot know their names
// ahead of time. Listing families to include would have to be revised every
// time an operator registers a provider, and a stale list drops resolutions
// silently rather than failing. Excluding `code` is the one thing that stays
// true whatever they register.
//
// THE EDGES ARE READ AS WELL AS THE NODES, and that is what makes a
// CORRELATES_WITH edge possible at all. The nodes answer "which cloud resource
// is this service label", which the proxy half needs; the edges answer "do these
// two resources depend on each other", which is the half that upgrades a
// temporal coincidence between two templates into a correlation. A block that
// declared nodes and no edges resolves labels and confirms nothing, which is a
// legitimate declaration rather than a broken one.
func cloudContextFrom(foreign framework.ForeignContext) CloudContext {
	out := CloudContext{dependsOn: make(map[string]struct{})}
	for _, graph := range foreign.Except(framework.FamilyCode) {
		for _, node := range graph.Nodes {
			out.Resources = append(out.Resources, CloudResource{
				Account:      graph.GraphName,
				ID:           node.ID,
				SymbolName:   node.SymbolName,
				ResourceType: resourceTypeOf(node),
			})
		}
		for _, edge := range graph.Edges {
			if edge.FromID == "" || edge.ToID == "" {
				continue
			}
			// BOTH DIRECTIONS. "These two resources are connected" is what
			// upgrades a coincidence to a correlation, and connection is
			// symmetric even where the declared edge is not.
			out.dependsOn[dependencyKey(graph.GraphName, edge.FromID, edge.ToID)] = struct{}{}
			out.dependsOn[dependencyKey(graph.GraphName, edge.ToID, edge.FromID)] = struct{}{}
		}
	}
	if len(out.dependsOn) == 0 {
		out.dependsOn = nil
	}
	return out
}

// resourceTypeOf reads a declared node's cloud resource type.
//
// It is the `resource_type` METADATA key, which is what the cloud collectors
// write and what the built-in resolver ranks on. A node arriving without it
// ranks against nothing and matches nothing, which is the correct outcome: a
// declaration that did not ask for the key cannot be resolved on it, and
// guessing from the node's graph-level type would resolve labels to resources
// the built-in path would not have matched.
func resourceTypeOf(node framework.ForeignNode) string {
	if node.Metadata == nil {
		return ""
	}
	return node.Metadata[metaResourceType]
}

// CloudContext is everything a collect knows about the cloud side.
type CloudContext struct {
	// Resources are the candidates label values are resolved against.
	Resources []CloudResource
	// dependsOn holds one key per declared edge, in BOTH directions, so a
	// dependency question is a lookup rather than a scan. It is unexported
	// because it is an index rather than a field a caller supplies: cloudContextFrom
	// builds it, and hasDependency is the only reader.
	dependsOn map[string]struct{}
}

// IsEmpty reports whether this context can produce anything.
func (c CloudContext) IsEmpty() bool { return len(c.Resources) == 0 && len(c.dependsOn) == 0 }

// cloudResourceRef is one resolved cloud resource, as a dependency question
// names it.
type cloudResourceRef struct {
	Account string
	ID      string
}

// hasDependency reports whether the declared block carried an edge between the
// two resources.
//
// TWO RESOURCES IN DIFFERENT ACCOUNTS ARE NEVER DEPENDENT HERE, because the
// declared edges are within-graph by the block's own shape: an edge belongs to
// one declared graph and its endpoints are ids in that graph. Answering across
// accounts would mean inventing a relation the block never carried.
func (c CloudContext) hasDependency(a, b cloudResourceRef) bool {
	if a.Account != b.Account {
		return false
	}
	_, ok := c.dependsOn[dependencyKey(a.Account, a.ID, b.ID)]
	return ok
}

// dependencyKey is one directed pair within one declared graph. The graph name
// is part of the key so two accounts holding same-named resources cannot answer
// each other's dependency questions.
func dependencyKey(account, from, to string) string {
	return account + "\x00" + from + "\x00" + to
}

// serviceResourceTypePrefixes are the resource types a SERVICE label may
// resolve to, in PRIORITY ORDER — the first matching prefix wins, so a label
// naming both a Service and a Deployment resolves to the Service.
var serviceResourceTypePrefixes = []string{
	"ecs:service",
	"ecs-service",
	"lambda:function",
	"lambda-function",
	"cloudrun:service",
	"cloud-run:service",
	"k8s:Service",
	"Service",
	"Deployment",
	"StatefulSet",
	"DaemonSet",
	"ReplicaSet",
	"Job",
	"CronJob",
	"Pod",
	"gce:instance-group",
	"appengine:service",
}

// namespaceResourceTypePrefixes are the resource types a NAMESPACE label may
// resolve to.
var namespaceResourceTypePrefixes = []string{
	"k8s:Namespace",
	"Namespace",
	"gcp:project",
	"aws:account",
	"ec2:vpc",
	"vpc",
}

// serviceIdentifyingKeys are the label keys worth resolving. Of these, a
// CloudWatch walk produces only `service`; the other three are kept because the
// resolution vocabulary is shared across log providers and a caller supplying
// enriched labels should reach the same answer here as elsewhere.
var serviceIdentifyingKeys = map[string]struct{}{
	"service":    {},
	"namespace":  {},
	"deployment": {},
	"app":        {},
}

// resolveStreams returns one resolution per distinct service-identifying label
// pair that matches a supplied resource.
//
// Pairs are deduplicated by key and value across every stream, so two streams
// carrying one service label produce ONE resolution and therefore one proxy —
// which for CloudWatch is the common shape, since every stream in a log group
// shares that group's service label.
//
// A pair that matches nothing is skipped silently. That is not a swallowed
// error: most labels name nothing in the cloud graph, and a resolver that
// failed on a miss could never run against a partial context.
func resolveStreams(streams []*LogStream, ctx CloudContext) []ResolvedProxy {
	if len(ctx.Resources) == 0 {
		return nil
	}
	var out []ResolvedProxy
	seen := make(map[string]struct{})
	for _, s := range streams {
		if s == nil {
			continue
		}
		for _, k := range sortedKeys(s.LowCardLabels) {
			v := s.LowCardLabels[k]
			if v == "" {
				continue
			}
			if _, ok := serviceIdentifyingKeys[k]; !ok {
				continue
			}
			if _, dup := seen[k+"="+v]; dup {
				continue
			}
			seen[k+"="+v] = struct{}{}
			if resolved, ok := resolveLabel(k, v, ctx.Resources); ok {
				out = append(out, resolved)
			}
		}
	}
	return out
}

// resolveLabel matches one label pair against the supplied resources, choosing
// the prefix list by the label's key.
func resolveLabel(key, value string, resources []CloudResource) (ResolvedProxy, bool) {
	prefixes := serviceResourceTypePrefixes
	if key == "namespace" {
		prefixes = namespaceResourceTypePrefixes
	}
	best, ok := pickBestResource(resources, value, prefixes)
	if !ok {
		return ResolvedProxy{}, false
	}
	return ResolvedProxy{LabelKey: key, LabelValue: value, Account: best.Account, ResourceID: best.ID}, true
}

// pickBestResource returns the highest-priority resource whose name matches and
// whose type is allowed. Priority is the index in allowed, so the caller's
// ordering decides, and a tie is broken by the first resource in the supplied
// slice.
func pickBestResource(resources []CloudResource, name string, allowed []string) (CloudResource, bool) {
	var best CloudResource
	bestRank := len(allowed)
	found := false
	for _, r := range resources {
		if r.SymbolName == "" || !strings.EqualFold(r.SymbolName, name) {
			continue
		}
		rank, ok := prefixRank(r.ResourceType, allowed)
		if !ok || rank >= bestRank {
			continue
		}
		bestRank, best, found = rank, r, true
	}
	return best, found
}

// prefixRank returns the index of the first allowed prefix that matches a
// resource type.
//
// A MATCH REQUIRES A WORD BOUNDARY: either the whole string equals the prefix,
// or the next character is one of the separators the cloud collectors use. That
// is what stops "Service" from matching "ServiceAccount", which is a different
// kind and sits elsewhere in the priority order. Matching is case-SENSITIVE
// here, unlike the name comparison, because a resource type is produced by the
// cloud collector rather than by a log producer.
func prefixRank(resourceType string, allowed []string) (int, bool) {
	if resourceType == "" {
		return 0, false
	}
	for i, p := range allowed {
		if !strings.HasPrefix(resourceType, p) {
			continue
		}
		if len(resourceType) == len(p) {
			return i, true
		}
		switch resourceType[len(p)] {
		case ':', '-', '/':
			return i, true
		}
	}
	return 0, false
}

// fixtureFamily is the registered graph type this module's tests declare their
// cloud fixtures under.
//
// IT IS A NAME AN OPERATOR REGISTERS, not a built-in. The block used to carry a
// `Cloud` field, so a fixture said "cloud" by construction and no test could be
// wrong about the family. Under the map-keyed contract the KEY is the family,
// there is no built-in cloud type to key on, and a fixture has to pick one the
// way an operator would — so it picks the name the aws contrib collector
// registers itself under, which is what produces the resources a CloudWatch log
// stream resolves against.
//
// IT LIVES BESIDE THE RESOLVER RATHER THAN IN A TEST FILE because the resolver's
// own contract is what it illustrates: cloudContextFrom takes EVERY declared
// family but code, so this constant is one legitimate value of that set and
// never a value the production path checks for.
const fixtureFamily = "aws"
