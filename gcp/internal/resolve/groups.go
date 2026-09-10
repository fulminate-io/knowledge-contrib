// SPDX-License-Identifier: Apache-2.0

package resolve

import (
	"strings"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// MethodIAMGroupResolve is the method discriminator on a resolved group binding.
const MethodIAMGroupResolve = "gcp-iam-group-resolve"

// groupPlaceholderPrefix is how an IAM policy names a group principal before
// anything has resolved it to a directory object.
const groupPlaceholderPrefix = "group:"

// IAMBindingGroups resolves IAM bindings that name a group by email onto the
// group's own node, and it is the ONE resolver that REWRITES the walk rather
// than adding to it.
//
// BOTH HALVES ARE THE POINT. It re-targets the binding onto the group node AND
// DROPS the placeholder edge. Doing only the first leaves a permanently dangling
// endpoint beside a correct one, and nothing downstream notices: a membership
// question answered off the resolved edge looks right while the placeholder sits
// in the graph forever.
//
// A placeholder whose email is NOT among the collected groups is LEFT ALONE. It
// is the only record that the binding exists, and dropping it to tidy the graph
// would delete information rather than resolve it.
func IAMBindingGroups(in gcpgraph.Result) (gcpgraph.Result, error) {
	byEmail := map[string]string{}
	for _, res := range in.Resources {
		if res.ResourceType != gcpgraph.ResourceTypeCloudIdentityGroup {
			continue
		}
		if email := res.Metadata["email"]; email != "" {
			byEmail[email] = res.ID
		}
	}

	out := gcpgraph.Result{Resources: in.Resources}
	if len(byEmail) == 0 {
		// Nothing to resolve against: the walk passes through with its
		// placeholders intact, which is a smaller claim than resolving them.
		out.Relations = in.Relations
		return out, nil
	}

	resolved := map[string]bool{}
	for _, rel := range in.Relations {
		groupNodeID := ""
		if rel.Type == gcpgraph.EdgeGrants && strings.HasPrefix(rel.To, groupPlaceholderPrefix) {
			groupNodeID = byEmail[strings.TrimPrefix(rel.To, groupPlaceholderPrefix)]
		}
		if groupNodeID == "" {
			out.Relations = append(out.Relations, rel)
			continue
		}
		key := rel.From + "|" + groupNodeID
		if resolved[key] {
			continue
		}
		resolved[key] = true
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: rel.From, To: groupNodeID, Type: gcpgraph.EdgeGrants,
			Method:   MethodIAMGroupResolve,
			Metadata: rel.Metadata,
		})
	}
	return out, nil
}
