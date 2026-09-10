// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// svc_iam.go — roles, users, groups and policies, and the four relationships
// among them.
//
// IAM IS GLOBAL, not regional, so nothing here carries a region: an IAM role has
// one identity for the whole account, and stamping it with the region this
// collect happened to walk would be a fact about the collect rather than about
// the resource.
//
// THE TRUST POLICY RIDES ALONG. ListRoles returns each role's
// AssumeRolePolicyDocument inline, which is what makes TRUSTS and
// WORKLOAD_IDENTITY cost no extra call; the walk records it and derive_trust.go
// and derive_irsa.go read it once the account is enumerated.

// iamAPI is the subset of the IAM client surface this collector calls.
type iamAPI interface {
	ListRoles(ctx context.Context, in *iam.ListRolesInput, optFns ...func(*iam.Options)) (*iam.ListRolesOutput, error)
	ListUsers(ctx context.Context, in *iam.ListUsersInput, optFns ...func(*iam.Options)) (*iam.ListUsersOutput, error)
	ListGroups(ctx context.Context, in *iam.ListGroupsInput, optFns ...func(*iam.Options)) (*iam.ListGroupsOutput, error)
	ListPolicies(ctx context.Context, in *iam.ListPoliciesInput, optFns ...func(*iam.Options)) (*iam.ListPoliciesOutput, error)
	ListAttachedRolePolicies(ctx context.Context, in *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error)
	ListGroupsForUser(ctx context.Context, in *iam.ListGroupsForUserInput, optFns ...func(*iam.Options)) (*iam.ListGroupsForUserOutput, error)
}

// iamMarker adapts IAM's Marker/IsTruncated paging onto the NextToken shape
// paginate drives. IAM predates the token convention the rest of the SDK uses,
// and a walk that read IsTruncated but never sent the Marker would read one page
// of an account with thousands of roles.
func iamMarker(isTruncated bool, marker *string) *string {
	if !isTruncated {
		return nil
	}
	return marker
}

func walkIAMRoles(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*iam.ListRolesOutput, error) {
			return w.clients.IAM.ListRoles(ctx, &iam.ListRolesInput{Marker: token})
		},
		func(p *iam.ListRolesOutput) *string { return iamMarker(p.IsTruncated, p.Marker) },
		func(p *iam.ListRolesOutput) error {
			for _, r := range p.Roles {
				arn := deref(r.Arn)
				if arn == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "path", deref(r.Path))
				put(detail, "description", deref(r.Description))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeIAMRole,
					name:         deref(r.RoleName),
					summary:      fmt.Sprintf("IAM role %s", deref(r.RoleName)),
					detail:       detail,
				}, w.account))
				w.recordRole(arn, deref(r.AssumeRolePolicyDocument))
				if err := w.walkRolePolicies(ctx, arn, deref(r.RoleName)); err != nil {
					return err
				}
			}
			return nil
		})
}

// walkRolePolicies emits the GRANTS edges for one role's attached policies.
//
// ONE CALL PER ROLE is the cost, and there is no batch form: IAM has no
// list-attached-policies-for-every-role operation. It is paid because "what can
// this role do" is unanswerable without it, and the alternative — reading the
// policy list alone — says which policies exist and never which principal holds
// one.
func (w *walkContext) walkRolePolicies(ctx context.Context, roleARN, roleName string) error {
	if roleName == "" {
		return nil
	}
	return paginate(ctx,
		func(ctx context.Context, token *string) (*iam.ListAttachedRolePoliciesOutput, error) {
			return w.clients.IAM.ListAttachedRolePolicies(ctx,
				&iam.ListAttachedRolePoliciesInput{RoleName: &roleName, Marker: token})
		},
		func(p *iam.ListAttachedRolePoliciesOutput) *string { return iamMarker(p.IsTruncated, p.Marker) },
		func(p *iam.ListAttachedRolePoliciesOutput) error {
			for _, pol := range p.AttachedPolicies {
				if policyARN := deref(pol.PolicyArn); policyARN != "" {
					w.sink.addEdge(policyARN, roleARN, EdgeGrants,
						map[string]string{"policy_name": deref(pol.PolicyName)})
				}
			}
			return nil
		})
}

func walkIAMUsers(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*iam.ListUsersOutput, error) {
			return w.clients.IAM.ListUsers(ctx, &iam.ListUsersInput{Marker: token})
		},
		func(p *iam.ListUsersOutput) *string { return iamMarker(p.IsTruncated, p.Marker) },
		func(p *iam.ListUsersOutput) error {
			for _, u := range p.Users {
				arn := deref(u.Arn)
				if arn == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "path", deref(u.Path))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeIAMUser,
					name:         deref(u.UserName),
					summary:      fmt.Sprintf("IAM user %s", deref(u.UserName)),
					detail:       detail,
				}, w.account))
				if err := w.walkUserGroups(ctx, arn, deref(u.UserName)); err != nil {
					return err
				}
			}
			return nil
		})
}

// walkUserGroups emits BOTH directions of one user's group membership.
//
// TWO EDGES RATHER THAN ONE, and it is not redundancy: the graph carries directed
// edges, and the two questions — "who is in this group" and "what groups is this
// user in" — start at different nodes. A single direction makes one of them
// unanswerable by traversal.
func (w *walkContext) walkUserGroups(ctx context.Context, userARN, userName string) error {
	if userName == "" {
		return nil
	}
	return paginate(ctx,
		func(ctx context.Context, token *string) (*iam.ListGroupsForUserOutput, error) {
			return w.clients.IAM.ListGroupsForUser(ctx,
				&iam.ListGroupsForUserInput{UserName: &userName, Marker: token})
		},
		func(p *iam.ListGroupsForUserOutput) *string { return iamMarker(p.IsTruncated, p.Marker) },
		func(p *iam.ListGroupsForUserOutput) error {
			for _, g := range p.Groups {
				groupARN := deref(g.Arn)
				if groupARN == "" {
					continue
				}
				w.sink.addEdge(groupARN, userARN, EdgeHasMember, nil)
				w.sink.addEdge(userARN, groupARN, EdgeMemberOf, nil)
			}
			return nil
		})
}

func walkIAMGroups(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*iam.ListGroupsOutput, error) {
			return w.clients.IAM.ListGroups(ctx, &iam.ListGroupsInput{Marker: token})
		},
		func(p *iam.ListGroupsOutput) *string { return iamMarker(p.IsTruncated, p.Marker) },
		func(p *iam.ListGroupsOutput) error {
			for _, g := range p.Groups {
				arn := deref(g.Arn)
				if arn == "" {
					continue
				}
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeIAMGroup,
					name:         deref(g.GroupName),
					summary:      fmt.Sprintf("IAM group %s", deref(g.GroupName)),
					detail:       map[string]string{"path": deref(g.Path)},
				}, w.account))
			}
			return nil
		})
}

func walkIAMPolicies(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*iam.ListPoliciesOutput, error) {
			// SCOPE Local: the account's OWN policies. Without it the walk
			// enumerates every AWS-managed policy as well — around a thousand
			// nodes that are identical in every account and describe nothing
			// about this one.
			return w.clients.IAM.ListPolicies(ctx, &iam.ListPoliciesInput{Marker: token, Scope: "Local"})
		},
		func(p *iam.ListPoliciesOutput) *string { return iamMarker(p.IsTruncated, p.Marker) },
		func(p *iam.ListPoliciesOutput) error {
			for _, pol := range p.Policies {
				arn := deref(pol.Arn)
				if arn == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "path", deref(pol.Path))
				put(detail, "description", deref(pol.Description))
				if pol.AttachmentCount != nil {
					detail["attachment_count"] = fmt.Sprintf("%d", *pol.AttachmentCount)
				}
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeIAMPolicy,
					name:         deref(pol.PolicyName),
					summary:      fmt.Sprintf("IAM policy %s", deref(pol.PolicyName)),
					detail:       detail,
				}, w.account))
			}
			return nil
		})
}
