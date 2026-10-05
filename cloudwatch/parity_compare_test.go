// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// parity_compare_test.go — how the emitted graph is compared to the answer key:
// nodes by id in both directions, edges as multisets carrying every field that
// rides the wire.

// checkNodes checks every node of the answer key against the walk's, by id.
func checkNodes(want []goldenNode, got []framework.Node) error {
	var errs []error
	byID := make(map[string]framework.Node, len(got))
	for _, n := range got {
		if prior, dup := byID[n.ID]; dup {
			errs = append(errs, fmt.Errorf("the walk emitted node id %q twice, as %s and %s", n.ID, prior.Type, n.Type))
		}
		byID[n.ID] = n
	}
	for _, w := range want {
		g, ok := byID[w.ID]
		if !ok {
			errs = append(errs, fmt.Errorf("the walk emitted no node %q (%s %q), which the built-in path produced",
				w.ID, w.Type, w.SymbolName))
			continue
		}
		delete(byID, w.ID)
		if g.Type != w.Type {
			errs = append(errs, fmt.Errorf("node %q: type %q, want %q", w.ID, g.Type, w.Type))
		}
		if g.SymbolName != w.SymbolName {
			errs = append(errs, fmt.Errorf("node %q: symbol name %q, want %q", w.ID, g.SymbolName, w.SymbolName))
		}
		if g.Source != w.Source {
			errs = append(errs, fmt.Errorf("node %q: source %q, want %q", w.ID, g.Source, w.Source))
		}
		if g.Description != w.Description {
			errs = append(errs, fmt.Errorf("node %q: description %q, want %q", w.ID, g.Description, w.Description))
		}
		errs = append(errs, checkMetadata(w.ID, w.Metadata, g.Metadata))
		if !w.ContentOmit && g.Content != "" {
			errs = append(errs, fmt.Errorf("node %q carries content the built-in path did not produce", w.ID))
		}
		if w.ContentOmit && g.Content == "" {
			errs = append(errs, fmt.Errorf("chunk node %q carries no content; the entry block is this collector's chunk payload", w.ID))
		}
	}
	for id, n := range byID {
		errs = append(errs, fmt.Errorf("the walk emitted node %q (%s %q), which the built-in path did not produce", id, n.Type, n.SymbolName))
	}
	return errors.Join(errs...)
}

// checkMetadata checks one node's metadata map exactly, in both directions.
func checkMetadata(id string, want, got map[string]string) error {
	var errs []error
	for k, v := range want {
		if got[k] != v {
			errs = append(errs, fmt.Errorf("node %q metadata[%q] = %q, want %q", id, k, got[k], v))
		}
	}
	for k, v := range got {
		if _, expected := want[k]; !expected {
			errs = append(errs, fmt.Errorf("node %q carries metadata[%q] = %q, which the built-in path did not produce", id, k, v))
		}
	}
	return errors.Join(errs...)
}

// checkEdges checks the edge sets as multisets, since an edge has no id and
// EMITTED_BY is deliberately not deduplicated.
func checkEdges(want []goldenEdge, got []framework.Edge) error {
	var errs []error
	wantKeys := make([]string, 0, len(want))
	for _, e := range want {
		wantKeys = append(wantKeys, edgeKeyGolden(e))
	}
	gotKeys := make([]string, 0, len(got))
	for _, e := range got {
		gotKeys = append(gotKeys, edgeKey(e))
	}
	sort.Strings(wantKeys)
	sort.Strings(gotKeys)

	missing, extra := diffSorted(wantKeys, gotKeys)
	for _, m := range missing {
		errs = append(errs, fmt.Errorf("the walk emitted no edge %s, which the built-in path produced", m))
	}
	for _, e := range extra {
		errs = append(errs, fmt.Errorf("the walk emitted edge %s, which the built-in path did not produce", e))
	}
	return errors.Join(errs...)
}

// edgeKey renders an edge as a comparable string carrying every field that
// rides the wire, so a dropped Confidence or a rewritten Evidence is a
// difference rather than a match.
func edgeKey(e framework.Edge) string {
	return jsonRender([]any{e.FromID, e.ToID, e.Type, e.Confidence, e.Method, e.Evidence})
}

func edgeKeyGolden(e goldenEdge) string {
	return jsonRender([]any{e.FromID, e.ToID, e.Type, e.Confidence, e.Method, e.Evidence})
}

// jsonRender renders a value for a comparison key or a failure message. A value
// that does not marshal renders as a LOUD placeholder carrying the error rather
// than as the empty string, which would read as "there was nothing here".
func jsonRender(v any) string {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<unrenderable: %v>", err)
	}
	return string(body)
}

// diffSorted returns the elements present only in want and only in got.
func diffSorted(want, got []string) (missing, extra []string) {
	counts := make(map[string]int, len(want))
	for _, w := range want {
		counts[w]++
	}
	for _, g := range got {
		counts[g]--
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for i := 0; i < counts[k]; i++ {
			missing = append(missing, k)
		}
		for i := 0; i > counts[k]; i-- {
			extra = append(extra, k)
		}
	}
	return missing, extra
}
