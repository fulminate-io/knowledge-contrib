// SPDX-License-Identifier: Apache-2.0

package resolve

import (
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// All runs every resolver over one walk and returns the walk PLUS everything
// they derived, with the group placeholders rewritten.
//
// THE ORDER IS NOT ARBITRARY. The five DERIVERS read the enumerated result and
// add to it, so they see the same input whatever order they run in. The group
// REWRITE runs LAST, over the accumulated relations, because it must reach a
// placeholder wherever it came from — including one a deriver produced — and
// because a rewrite that ran first would be re-shadowed by whatever came after.
//
// A RESOLVER'S FAILURE IS THE WALK'S FAILURE. None of them is best-effort: a
// resolver that cannot read its input has produced an incomplete derivation, and
// a caller that logged and continued would return a graph short of edges while
// asserting a complete walk. The caller turns this error into an INCOMPLETE
// assertion instead, which is what stops the server treating the missing half as
// deleted.
func All(projectID string, in gcpgraph.Result) (gcpgraph.Result, error) {
	out := gcpgraph.Result{
		Resources: append([]gcpgraph.Resource(nil), in.Resources...),
		Relations: append([]gcpgraph.Relation(nil), in.Relations...),
	}

	for _, deriver := range []struct {
		name string
		run  func(string, gcpgraph.Result) (gcpgraph.Result, error)
	}{
		{"firewall", func(_ string, r gcpgraph.Result) (gcpgraph.Result, error) { return Firewall(r) }},
		{"shared-vpc", SharedVPC},
		{"image-lineage", func(_ string, r gcpgraph.Result) (gcpgraph.Result, error) {
			return CloudRunImages(r)
		}},
		{"cross-project-trust", CrossProjectTrust},
		{"dns-targets", func(_ string, r gcpgraph.Result) (gcpgraph.Result, error) {
			return DNSRecordTargets(r)
		}},
	} {
		// Every deriver reads the ENUMERATED result, not the accumulating one,
		// so one deriver's output can never become another's input by accident.
		derived, err := deriver.run(projectID, in)
		if err != nil {
			return gcpgraph.Result{}, fmt.Errorf("gcp walk: deriving %s: %w", deriver.name, err)
		}
		out.Add(derived)
	}

	rewritten, err := IAMBindingGroups(out)
	if err != nil {
		return gcpgraph.Result{}, fmt.Errorf("gcp walk: resolving iam group bindings: %w", err)
	}
	return rewritten, nil
}
