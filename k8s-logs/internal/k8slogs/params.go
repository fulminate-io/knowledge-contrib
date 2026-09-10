// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"fmt"
	"time"
)

// params.go — the collect parameters this collector advertises, and the
// refusals that stand between them and a walk.
//
// EVERY FIELD IS OPTIONAL ON THE WIRE and that is a contract requirement rather
// than a convenience: the client omits the params object entirely from a
// collect that carries none, so a required field would refuse every paramless
// collect before it was sent. What a field may not be is PRESENT AND
// MEANINGLESS — an empty namespace list, a range whose end precedes its start,
// a negative bound — and each of those is refused by name.

// Params is the collector's own half of the advertised input schema.
type Params struct {
	// Context names the kubecontext to read. Empty means the kubeconfig's
	// current context, which is the operator declining to name one rather than
	// a fall-through.
	Context string `json:"context,omitempty" jsonschema:"The kubecontext to read. Empty uses the kubeconfig's current context."`

	// Namespaces are the namespaces to read, one list call each. Empty means
	// EVERY namespace, and this collector refuses it: at the API an empty
	// namespace is a cluster-wide read, and a collect that reads a whole
	// cluster should say so in its own parameters.
	Namespaces []string `json:"namespaces,omitempty" jsonschema:"The namespaces to read. At least one is required; there is no cluster-wide default."`

	// LabelSelector narrows the pod list, in the API's own selector syntax.
	LabelSelector string `json:"label_selector,omitempty" jsonschema:"A Kubernetes label selector narrowing which pods are read, e.g. app=api,tier!=batch."`

	// Containers narrows which containers of each pod are read, by name. Empty
	// reads every container INCLUDING every init container.
	Containers []string `json:"containers,omitempty" jsonschema:"Container names to read. Empty reads every container of each pod, init containers included."`

	// Since and Until bound the time range, as RFC3339 timestamps. Since is
	// applied server-side; Until is applied client-side because the API has no
	// end bound.
	Since string `json:"since,omitempty" jsonschema:"RFC3339 start of the time range, applied server-side."`
	Until string `json:"until,omitempty" jsonschema:"RFC3339 end of the time range, applied client-side: the Kubernetes API has no end bound."`

	// TailLines and LimitBytes are the operator's optional self-bounds. This
	// collector imposes NEITHER by default: a bound that engaged makes the walk
	// incomplete, and choosing one on the operator's behalf would quietly
	// disable the server's deletion phase on every collect.
	TailLines  *int64 `json:"tail_lines,omitempty" jsonschema:"Read only the last N lines of each container. Unset reads the whole retained log."`
	LimitBytes *int64 `json:"limit_bytes,omitempty" jsonschema:"Stop each container's read after N bytes. Unset reads the whole retained log."`

	// Stream selects stdout, stderr or both.
	Stream string `json:"stream,omitempty" jsonschema:"Which container stream to read: all (the default), stdout, or stderr."`

	// ChunkWindowSeconds is the chunk bucket width. Zero means the default.
	ChunkWindowSeconds int `json:"chunk_window_seconds,omitempty" jsonschema:"Chunk bucket width in seconds. Zero uses the default."`
}

// Resolved is Params after validation: the same request with its strings parsed
// and its refusals already made.
type Resolved struct {
	Context       string
	Namespaces    []string
	LabelSelector string
	Containers    []string
	Since         time.Time
	Until         time.Time
	TailLines     *int64
	LimitBytes    *int64
	Stream        string
	ChunkWindow   time.Duration
}

// Validate parses and checks the parameters, refusing each bad shape by name.
func (p Params) Validate() (Resolved, error) {
	out := Resolved{
		Context:       p.Context,
		LabelSelector: p.LabelSelector,
		Containers:    p.Containers,
		TailLines:     p.TailLines,
		LimitBytes:    p.LimitBytes,
		Stream:        p.Stream,
	}

	namespaces, err := checkNamespaces(p.Namespaces)
	if err != nil {
		return Resolved{}, err
	}
	out.Namespaces = namespaces

	if out.Since, err = parseBound("since", p.Since); err != nil {
		return Resolved{}, err
	}
	if out.Until, err = parseBound("until", p.Until); err != nil {
		return Resolved{}, err
	}
	if !out.Since.IsZero() && !out.Until.IsZero() && out.Until.Before(out.Since) {
		return Resolved{}, fmt.Errorf(
			"k8s-logs: the time range ends before it starts: until %s precedes since %s",
			p.Until, p.Since)
	}

	if err := checkBound("tail_lines", p.TailLines); err != nil {
		return Resolved{}, err
	}
	if err := checkBound("limit_bytes", p.LimitBytes); err != nil {
		return Resolved{}, err
	}
	if err := ValidateStream(p.Stream, p.TailLines); err != nil {
		return Resolved{}, err
	}

	if p.ChunkWindowSeconds < 0 {
		return Resolved{}, fmt.Errorf(
			"k8s-logs: chunk_window_seconds is %d; a chunk window is a duration and cannot be negative",
			p.ChunkWindowSeconds)
	}
	out.ChunkWindow = time.Duration(p.ChunkWindowSeconds) * time.Second
	return out, nil
}

// checkNamespaces refuses an empty list and an empty member.
//
// THE EMPTY LIST IS THE ONE THAT MATTERS. At the Kubernetes API an empty
// namespace means every namespace in the cluster, so a collect that named none
// would read the whole cluster while its parameters said nothing about it.
func checkNamespaces(namespaces []string) ([]string, error) {
	if len(namespaces) == 0 {
		return nil, fmt.Errorf(
			"k8s-logs: no namespaces were named. An empty namespace reaches the Kubernetes API as EVERY " +
				"namespace, so this collector requires the namespaces to read to be named explicitly")
	}
	out := make([]string, 0, len(namespaces))
	seen := make(map[string]struct{}, len(namespaces))
	for i, ns := range namespaces {
		if ns == "" {
			return nil, fmt.Errorf(
				"k8s-logs: namespaces[%d] is empty, which at the Kubernetes API means every namespace", i)
		}
		if _, dup := seen[ns]; dup {
			continue
		}
		seen[ns] = struct{}{}
		out = append(out, ns)
	}
	return out, nil
}

// parseBound parses one RFC3339 time bound, naming the field in a refusal.
func parseBound(field, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("k8s-logs: %s is not an RFC3339 timestamp: %q", field, value)
	}
	return ts.UTC(), nil
}

// checkBound refuses a self-bound that is present and not positive. Zero is
// refused as well as a negative: an explicit zero reads as "read nothing",
// which is not what an operator setting a bound means, and unset already
// expresses "no bound".
func checkBound(field string, value *int64) error {
	if value == nil {
		return nil
	}
	if *value <= 0 {
		return fmt.Errorf(
			"k8s-logs: %s is %d; a bound must be positive, and leaving it unset is how a read stays unbounded",
			field, *value)
	}
	return nil
}
