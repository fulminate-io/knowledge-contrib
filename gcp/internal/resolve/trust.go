// SPDX-License-Identifier: Apache-2.0

package resolve

import (
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// MethodCrossProjectTrust is the method discriminator on a trust edge.
const MethodCrossProjectTrust = "gcp-cross-project-trust"

// impersonationRoles are the two roles that let one identity act as another. The
// predicate is the WHOLE POINT of this resolver: a cross-project binding of any
// other role is ordinary access, and emitting trust for all of them would turn a
// security-relevant edge into noise on every real project.
var impersonationRoles = map[string]bool{
	"roles/iam.serviceAccountTokenCreator": true,
	"roles/iam.serviceAccountUser":         true,
}

// CrossProjectTrust derives trust from this project to a service account in
// ANOTHER project that holds an impersonation role here. The edge runs FROM the
// foreign account TO the local project, which matches how a cross-account trust
// relationship reads everywhere else in this graph family.
//
// It takes the local project id as an argument rather than inferring it from the
// accounts it sees: inferring it from the first account would make the whole
// derivation depend on which account the walk happened to enumerate first.
func CrossProjectTrust(projectID string, in gcpgraph.Result) (gcpgraph.Result, error) {
	// Index the accounts by node id so an inbound grant can be placed in a
	// project by the account's email rather than by its node id, which is a
	// LOCAL resource name even for a foreign account.
	emails := map[string]string{}
	for _, res := range in.Resources {
		if res.ResourceType != gcpgraph.ResourceTypeServiceAccount {
			continue
		}
		if email := res.Metadata["email"]; email != "" {
			emails[res.ID] = email
		}
	}
	if len(emails) == 0 {
		return gcpgraph.Result{}, nil
	}

	var out gcpgraph.Result
	seen := map[string]bool{}
	for _, rel := range in.Relations {
		if rel.Type != gcpgraph.EdgeGrants {
			continue
		}
		email, ok := emails[rel.To]
		if !ok {
			continue
		}
		if !impersonationRoles[roleOf(rel)] {
			continue
		}
		// A principal that is not a service account has no project in its email,
		// so a human or a group never yields a trust edge.
		accountProject := gcpgraph.ProjectFromServiceAccountEmail(email)
		if accountProject == "" || accountProject == projectID {
			continue
		}
		if seen[rel.To] {
			continue
		}
		seen[rel.To] = true
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: rel.To, To: gcpgraph.ProjectResourceName(projectID),
			Type: gcpgraph.EdgeTrusts, Method: MethodCrossProjectTrust,
			Metadata: map[string]string{
				"foreign_project": accountProject,
				"role_name":       roleOf(rel),
			},
		})
	}
	return out, nil
}

// roleOf reads the role a binding granted. It comes from the edge's own
// metadata, and falls back to the edge SOURCE, because the source id of an IAM
// binding edge IS the role string when the binding has no separate role node.
func roleOf(rel gcpgraph.Relation) string {
	if role := rel.Metadata["role_name"]; role != "" {
		return role
	}
	return rel.From
}
