// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"fmt"
	"strconv"
	"strings"

	adminpb "cloud.google.com/go/iam/admin/apiv1/adminpb"
	resourcemanagerpb "cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	cloudidentity "google.golang.org/api/cloudidentity/v1"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// identity.go — who can do what: service accounts, the project itself, the
// policy bindings on it, and the directory groups those bindings name.
//
// A BINDING NAMES A PRINCIPAL BY STRING, and that string is what a group
// placeholder is. This file emits the binding edge exactly as the policy states
// it; the resolver that runs after the walk is what turns a group placeholder
// into an edge onto the group's own node. Resolving it here would need a
// directory lookup per binding, and the group enumeration has already made one.

// ServiceAccounts enumerates the project's service accounts.
func ServiceAccounts(list Lister[*adminpb.ServiceAccount]) Subcollector {
	return New("gcp-service-accounts", list, convertServiceAccount)
}

func convertServiceAccount(_ string, account *adminpb.ServiceAccount) (gcpgraph.Result, error) {
	if account == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil service account")
	}
	id := account.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a service account with no resource name")
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: account.GetEmail(), SelfLink: id, Description: account.GetDescription(),
		Fields: nonEmptyFields(map[string]string{
			"email":       account.GetEmail(),
			"displayName": account.GetDisplayName(),
			"uniqueId":    account.GetUniqueId(),
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	// The email is metadata rather than only content because the cross-project
	// trust derivation reads it to place the account in a project, and a
	// derivation that had to parse content for an identity would be reading past
	// the classification a consumer filters on.
	metadata := map[string]string{"disabled": strconv.FormatBool(account.GetDisabled())}
	setIfNotEmpty(metadata, "email", account.GetEmail())
	setIfNotEmpty(metadata, "display_name", account.GetDisplayName())
	setIfNotEmpty(metadata, "unique_id", account.GetUniqueId())

	return gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: account.GetEmail(), ResourceType: gcpgraph.ResourceTypeServiceAccount,
		Content: raw, Metadata: metadata,
	}}}, nil
}

// Projects enumerates the project this collect names, as a node of its own. It
// is one item rather than a list, and it exists because the cross-project trust
// derivation and the identity federation edges both point at it.
func Projects(list Lister[*resourcemanagerpb.Project]) Subcollector {
	return New("gcp-projects", list, convertProject)
}

func convertProject(_ string, project *resourcemanagerpb.Project) (gcpgraph.Result, error) {
	if project == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil project")
	}
	if project.GetProjectId() == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a project with no project id")
	}
	// projects/{projectId}, NOT the API's own projects/{projectNumber} name:
	// every other reference in this graph is by project id, and two spellings
	// for one project would split it into two nodes.
	id := gcpgraph.ProjectResourceName(project.GetProjectId())
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: project.GetProjectId(), SelfLink: id, State: project.GetState().String(),
		Labels: project.GetLabels(),
		Fields: nonEmptyFields(map[string]string{
			"displayName":   project.GetDisplayName(),
			"parent":        project.GetParent(),
			"projectNumber": project.GetName(),
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{}
	setIfNotEmpty(metadata, "state", project.GetState().String())
	setIfNotEmpty(metadata, "display_name", project.GetDisplayName())
	setIfNotEmpty(metadata, "parent", project.GetParent())
	for k, v := range project.GetLabels() {
		metadata["label/"+k] = v
	}
	return gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: project.GetProjectId(), ResourceType: gcpgraph.ResourceTypeProject,
		Content: raw, Metadata: metadata,
	}}}, nil
}

// PolicyBinding is one role granted to one principal on one resource. It is this
// package's own type because a policy is fetched per resource rather than
// enumerated, so the production wiring assembles the list and the converter
// stays a pure function of it.
type PolicyBinding struct {
	// ResourceID is the node the policy is attached to.
	ResourceID string
	// Role is the role granted, in its full roles/... form.
	Role string
	// Principal is the policy's own spelling of who holds it: a
	// serviceAccount:, user:, group: or domain: prefixed string.
	Principal string
}

// PolicyBindings turns a project's IAM policy bindings into grant edges.
func PolicyBindings(list Lister[PolicyBinding]) Subcollector {
	return New("gcp-iam-bindings", list, convertPolicyBinding)
}

func convertPolicyBinding(projectID string, binding PolicyBinding) (gcpgraph.Result, error) {
	if binding.ResourceID == "" || binding.Role == "" || binding.Principal == "" {
		return gcpgraph.Result{}, fmt.Errorf(
			"a policy binding is incomplete: resource=%q role=%q principal=%q",
			binding.ResourceID, binding.Role, binding.Principal)
	}
	// The edge runs FROM the role TO the principal, and the role string is the
	// source id. A role is not a resource this collector enumerates, so the edge
	// endpoint IS the role name — which is also what the trust derivation falls
	// back to when a binding carries no role metadata of its own.
	return gcpgraph.Result{Relations: []gcpgraph.Relation{{
		From: binding.Role,
		To:   principalNodeID(projectID, binding.Principal),
		Type: gcpgraph.EdgeGrants,
		Metadata: map[string]string{
			"role_name": binding.Role,
			"resource":  binding.ResourceID,
			"principal": binding.Principal,
		},
	}}}, nil
}

// principalNodeID maps a policy's own principal spelling onto the node id it
// refers to. A service account resolves to the account's resource name; a GROUP
// is left as the policy's own placeholder, which the group resolver rewrites
// once the directory enumeration has produced the group's node.
func principalNodeID(projectID, principal string) string {
	kind, value, ok := strings.Cut(principal, ":")
	if !ok {
		return principal
	}
	if kind == "serviceAccount" {
		return gcpgraph.ServiceAccountResourceName(projectID, value)
	}
	return principal
}

// IdentityGroups enumerates the directory groups this project's policies name,
// with the memberships that make a group's reach walkable.
func IdentityGroups(list Lister[IdentityGroup]) Subcollector {
	return New("gcp-identity-groups", list, convertIdentityGroup)
}

// IdentityGroup is one directory group and its members. Memberships are a
// second API call per group, so the production wiring assembles them and the
// converter stays pure.
type IdentityGroup struct {
	Group   *cloudidentity.Group
	Members []*cloudidentity.Membership
}

func convertIdentityGroup(_ string, group IdentityGroup) (gcpgraph.Result, error) {
	if group.Group == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil group")
	}
	id := group.Group.Name
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a group with no resource name")
	}
	email := ""
	if group.Group.GroupKey != nil {
		email = group.Group.GroupKey.Id
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: email, SelfLink: id, Description: group.Group.Description,
		Labels: group.Group.Labels,
		Fields: nonEmptyFields(map[string]string{
			"email":       email,
			"displayName": group.Group.DisplayName,
			"parent":      group.Group.Parent,
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	// The email is metadata because the group resolver joins a policy's
	// group: placeholder onto this node by email.
	metadata := map[string]string{"member_count": strconv.Itoa(len(group.Members))}
	setIfNotEmpty(metadata, "email", email)
	setIfNotEmpty(metadata, "display_name", group.Group.DisplayName)

	name := email
	if name == "" {
		name = group.Group.DisplayName
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: name, ResourceType: gcpgraph.ResourceTypeCloudIdentityGroup,
		Content: raw, Metadata: metadata,
	}}}

	for _, member := range group.Members {
		if member == nil || member.PreferredMemberKey == nil {
			continue
		}
		memberID := member.PreferredMemberKey.Id
		if memberID == "" {
			continue
		}
		// BOTH directions are emitted, and they are not redundant: one answers
		// "who is in this group" and the other "what does this identity belong
		// to", and a traversal in a graph walks edges in the direction they were
		// written.
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: memberID, Type: gcpgraph.EdgeHasMember,
			Metadata: map[string]string{"member_type": member.Type},
		})
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: memberID, To: id, Type: gcpgraph.EdgeMemberOf,
		})
	}
	return out, nil
}
