// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"net/url"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// fixture_identity_test.go — IAM and EKS, the two services whose output the
// derived trust and workload-identity passes read.
//
// THE TRUST POLICY IS URL-ENCODED, exactly as the IAM API returns it, and that is
// deliberate rather than incidental: a fixture carrying plain JSON would pass
// against a collector that forgot to decode, which is the defect the encoding
// exists to catch. It is encoded HERE with the standard library rather than
// pasted pre-encoded, so a reader can see the document it stands for.

const (
	fixtureRoleARN   = "arn:aws:iam::123456789012:role/app"
	fixturePolicyARN = "arn:aws:iam::123456789012:policy/app-policy"
	fixtureUserARN   = "arn:aws:iam::123456789012:user/alice"
	fixtureGroupARN  = "arn:aws:iam::123456789012:group/devs"
)

// fixtureAppTrustPolicy is the ordinary role's trust policy: a service principal
// and a CROSS-ACCOUNT principal.
//
// THE SERVICE PRINCIPAL MUST PRODUCE NO EDGE — lambda.amazonaws.com is not an
// ARN and carries no account, so treating it as a foreign trust would report
// every Lambda execution role as trusting an outside party.
const fixtureAppTrustPolicy = `{
  "Version": "2012-10-17",
  "Statement": [
    {"Effect": "Allow", "Principal": {"Service": "lambda.amazonaws.com"}, "Action": "sts:AssumeRole"},
    {"Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::210987654321:root"}, "Action": "sts:AssumeRole"}
  ]
}`

// fixtureIRSATrustPolicy is the IRSA role's: a federated principal with a
// subject condition naming one ServiceAccount and a second naming a WILDCARD.
//
// THE WILDCARD ARM IS NOT DECORATION. It admits any ServiceAccount in a
// namespace, which names no single Kubernetes object, and collapsing it onto a
// concrete id would assert a binding that does not exist.
const fixtureIRSATrustPolicy = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {"Federated": "arn:aws:iam::123456789012:oidc-provider/` + fixtureIssuerHost + `"},
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {"` + fixtureIssuerHost + `:sub": "system:serviceaccount:payments:api"},
        "StringLike": {"` + fixtureIssuerHost + `:sub": "system:serviceaccount:batch:*"}
      }
    }
  ]
}`

func fixtureIAM() *fakeIam {
	return &fakeIam{
		listRoles: func(*iam.ListRolesInput) (*iam.ListRolesOutput, error) {
			return &iam.ListRolesOutput{Roles: []iamtypes.Role{
				{
					Arn:                      new(fixtureRoleARN),
					RoleName:                 new("app"),
					Path:                     new("/"),
					AssumeRolePolicyDocument: new(url.QueryEscape(fixtureAppTrustPolicy)),
				},
				{
					Arn:                      new(fixtureIRSARole),
					RoleName:                 new("irsa-app"),
					AssumeRolePolicyDocument: new(url.QueryEscape(fixtureIRSATrustPolicy)),
				},
			}}, nil
		},
		listAttachedRolePolicies: func(in *iam.ListAttachedRolePoliciesInput) (*iam.ListAttachedRolePoliciesOutput, error) {
			if deref(in.RoleName) != "app" {
				return &iam.ListAttachedRolePoliciesOutput{}, nil
			}
			return &iam.ListAttachedRolePoliciesOutput{AttachedPolicies: []iamtypes.AttachedPolicy{{
				PolicyArn: new(fixturePolicyARN), PolicyName: new("app-policy"),
			}}}, nil
		},
		listUsers: func(*iam.ListUsersInput) (*iam.ListUsersOutput, error) {
			return &iam.ListUsersOutput{Users: []iamtypes.User{{
				Arn: new(fixtureUserARN), UserName: new("alice"), Path: new("/"),
			}}}, nil
		},
		listGroupsForUser: func(in *iam.ListGroupsForUserInput) (*iam.ListGroupsForUserOutput, error) {
			if deref(in.UserName) != "alice" {
				return &iam.ListGroupsForUserOutput{}, nil
			}
			return &iam.ListGroupsForUserOutput{Groups: []iamtypes.Group{{
				Arn: new(fixtureGroupARN), GroupName: new("devs"),
			}}}, nil
		},
		listGroups: func(*iam.ListGroupsInput) (*iam.ListGroupsOutput, error) {
			return &iam.ListGroupsOutput{Groups: []iamtypes.Group{{
				Arn: new(fixtureGroupARN), GroupName: new("devs"), Path: new("/"),
			}}}, nil
		},
		listPolicies: func(in *iam.ListPoliciesInput) (*iam.ListPoliciesOutput, error) {
			// THE SCOPE IS ASSERTED IN THE FIXTURE: a walk that stopped sending
			// Scope=Local would enumerate every AWS-managed policy in a real
			// account, and this arm is what notices.
			if in.Scope != "Local" {
				return &iam.ListPoliciesOutput{}, nil
			}
			return &iam.ListPoliciesOutput{Policies: []iamtypes.Policy{{
				Arn: new(fixturePolicyARN), PolicyName: new("app-policy"),
				AttachmentCount: new(int32(1)), Path: new("/"),
			}}}, nil
		},
	}
}

func fixtureEKS() *fakeEks {
	return &fakeEks{
		listClusters: func(*eks.ListClustersInput) (*eks.ListClustersOutput, error) {
			return &eks.ListClustersOutput{Clusters: []string{"prod"}}, nil
		},
		describeCluster: func(in *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
			if deref(in.Name) != "prod" {
				return &eks.DescribeClusterOutput{}, nil
			}
			return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{
				Arn:     new(fixtureClusterARN),
				Name:    new("prod"),
				Version: new("1.31"),
				Status:  ekstypes.ClusterStatusActive,
				RoleArn: new(fixtureRoleARN),
				// THE ISSUER CARRIES ITS SCHEME here, as the API returns it, so the
				// walk's own scheme stripping is exercised rather than assumed.
				Identity: &ekstypes.Identity{Oidc: &ekstypes.OIDC{
					Issuer: new("https://" + fixtureIssuerHost),
				}},
				ResourcesVpcConfig: &ekstypes.VpcConfigResponse{
					VpcId:            new(fixtureVPC),
					SubnetIds:        []string{fixtureSubnet},
					SecurityGroupIds: []string{fixtureSG},
				},
			}}, nil
		},
	}
}
