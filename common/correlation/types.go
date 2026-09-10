// SPDX-License-Identifier: Apache-2.0

package correlation

import "time"

// types.go — THE INPUT VOCABULARY, and the reason every one of these types is
// this module's own rather than a collector's.
//
// A COLLECTOR MODULE MAY NEVER IMPORT A SIBLING COLLECTOR. So the detector
// cannot take stackdriver's *logTemplate or k8s-logs's *Template; it takes
// values shaped like the facts every logs collector already holds, and each
// caller builds them from its own types at its own call site. The four value
// types below carry ONLY the fields the detector reads — a template's identity,
// severity and time range, a chunk's two ids, a stream's identity and labels —
// so a collector that grows a field owes this module nothing.
//
// THE TWO INTERFACES ARE THE FACTS A COLLECTOR PROCESS CANNOT DERIVE: which
// cloud resource a service label names, and whether two resources depend on each
// other. Both are properties of the operator's cloud graph, which no collector
// can read, so they arrive injected. Both are OPTIONAL and a nil value is
// meaningful rather than malformed: a collect that carried no declared cloud
// context resolves nothing and confirms nothing, which is the honest zero and
// not a degraded one.

// The service-identifying label keys, in the precedence serviceFromStream reads
// them. They are an informal contract with every producer of a log stream: a
// collector whose own service label lives under another key maps it into one of
// these when it builds a Stream.
//
// ALL FOUR ARE DECLARED because all four are read. An importer that could see
// only the first two would have to read this module's source to discover the
// rest of the contract it is being held to.
const (
	// FieldService is the explicit service label and wins over every other key.
	FieldService = "service"
	// FieldNamespace is the fallback for platforms where the namespace is the
	// service boundary.
	FieldNamespace = "namespace"
	// FieldDeployment and FieldApp are the two remaining identifiers, read in
	// that order. They are last because a deployment or an app label can name a
	// narrower thing than the service a correlation is about.
	FieldDeployment = "deployment"
	FieldApp        = "app"
)

// Template is one clustered log pattern, as the detector needs it: an identity,
// a severity to filter on and the time range that decides co-occurrence.
type Template struct {
	// ID is the template's identity, and it is what a correlation names. It
	// must be non-empty: an empty id would key a map entry every other empty id
	// resolves against.
	ID string
	// Severity is the canonical level name. The ERROR filter reads it through
	// SeverityAtLeast, so a level outside the six-name vocabulary ranks below
	// TRACE and never correlates.
	Severity string
	// FirstSeen and LastSeen bound the entries that matched. Either may be
	// unset, which passes through the overlap padding untouched: an unknown
	// bound is not a bound to widen.
	FirstSeen time.Time
	LastSeen  time.Time
}

// Chunk is a template-to-stream association. The detector reads nothing else
// from a chunk: it is how a template is attributed to the service that emitted
// it.
type Chunk struct {
	StreamID   string
	TemplateID string
}

// Stream is one label set. The detector reads the labels to name the owning
// service, and hands the whole stream to the resolver, which may need the
// surrounding context labels (project, cluster) to decide which cloud graph a
// resource lives in.
type Stream struct {
	// ID is the stream's identity, which chunks reference. It must be non-empty
	// for the same reason a template's must.
	ID     string
	Labels map[string]string
}

// ResolvedResource is one cloud resource a service label resolved to. THE
// ACCOUNT TRAVELS WITH THE ID, whole, all the way to the oracle: whether two
// resources in different accounts may depend on each other is the OPERATOR'S
// cloud-graph question, so it is the injected oracle's to answer and never this
// module's to pre-empt.
type ResolvedResource struct {
	Account string
	ID      string
}

// Resolver maps a service-identifying label value to a cloud resource. It is
// optional: a nil Resolver resolves nothing, which leaves every candidate
// unconfirmed.
type Resolver interface {
	// ResolveService maps one service name to a cloud resource, reporting
	// whether it resolved at all. The stream is supplied for the surrounding
	// context labels; an implementation is free to ignore it.
	ResolveService(stream *Stream, service string) (ResolvedResource, bool)
}

// DependencyOracle answers whether two resolved resources are connected in the
// operator's cloud graph, which is what upgrades a temporal coincidence to a
// correlation. It is optional: a nil oracle confirms nothing, which is what
// makes "no cloud context" produce no edges rather than every edge.
type DependencyOracle interface {
	HasDependency(a, b ResolvedResource) bool
}

// Result is one candidate pair of error templates whose ranges overlap across
// two different services.
//
// StructurallyConfirmed is the half a collector cannot decide alone. Only a
// confirmed pair becomes an edge, so an unconfirmed candidate is a statement
// that the dependency is UNKNOWN — not that it is absent.
type Result struct {
	TemplateA             string
	TemplateB             string
	ServiceA              string
	ServiceB              string
	ResourceA             string
	ResourceB             string
	CooccurrenceScore     float64
	StructurallyConfirmed bool
}

// Input is the whole detector call. It is a struct rather than a parameter list
// because three of its six members are optional and two of those are interfaces:
// a positional call would make a nil oracle and a nil resolver look like an
// omission at every call site instead of the deliberate "no cloud context" they
// are.
type Input struct {
	// Templates is the collector's full template list. Non-error templates are
	// filtered out here rather than by the caller, so the ERROR rule has one
	// home.
	Templates []*Template
	// Chunks attribute templates to streams.
	Chunks []*Chunk
	// Streams supply the service labels and the resolver's context.
	Streams []*Stream
	// ProxyMap maps a service label VALUE to the "account:resource" diagnostic
	// string a result carries in ResourceA/ResourceB. It is the caller's own
	// resolution set, and it may legitimately be missing a service that
	// resolves — see the note on axis (c) in correlation.go.
	ProxyMap map[string]string
	// Resolver and Oracle are the injected cloud facts. Both are optional.
	Resolver Resolver
	Oracle   DependencyOracle
}
