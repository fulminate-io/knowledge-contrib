// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"

	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// fakes_services_b_test.go — the fake for the second half of the service clients,
// alphabetically. See fakes_services_a_test.go for the shape and why every method
// defaults to an empty successful response.

type fakeElbv2 struct {
	describeLoadBalancers func(*elbv2.DescribeLoadBalancersInput) (*elbv2.DescribeLoadBalancersOutput, error)
	describeTargetGroups  func(*elbv2.DescribeTargetGroupsInput) (*elbv2.DescribeTargetGroupsOutput, error)
	describeTargetHealth  func(*elbv2.DescribeTargetHealthInput) (*elbv2.DescribeTargetHealthOutput, error)
	describeListeners     func(*elbv2.DescribeListenersInput) (*elbv2.DescribeListenersOutput, error)
}

func (f *fakeElbv2) DescribeLoadBalancers(_ context.Context, in *elbv2.DescribeLoadBalancersInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error) {
	if f.describeLoadBalancers == nil {
		return &elbv2.DescribeLoadBalancersOutput{}, nil
	}
	return f.describeLoadBalancers(in)
}

func (f *fakeElbv2) DescribeTargetGroups(_ context.Context, in *elbv2.DescribeTargetGroupsInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error) {
	if f.describeTargetGroups == nil {
		return &elbv2.DescribeTargetGroupsOutput{}, nil
	}
	return f.describeTargetGroups(in)
}

func (f *fakeElbv2) DescribeTargetHealth(_ context.Context, in *elbv2.DescribeTargetHealthInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTargetHealthOutput, error) {
	if f.describeTargetHealth == nil {
		return &elbv2.DescribeTargetHealthOutput{}, nil
	}
	return f.describeTargetHealth(in)
}

func (f *fakeElbv2) DescribeListeners(_ context.Context, in *elbv2.DescribeListenersInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeListenersOutput, error) {
	if f.describeListeners == nil {
		return &elbv2.DescribeListenersOutput{}, nil
	}
	return f.describeListeners(in)
}

type fakeEventBridge struct {
	listRules         func(*eventbridge.ListRulesInput) (*eventbridge.ListRulesOutput, error)
	listTargetsByRule func(*eventbridge.ListTargetsByRuleInput) (*eventbridge.ListTargetsByRuleOutput, error)
}

func (f *fakeEventBridge) ListRules(_ context.Context, in *eventbridge.ListRulesInput, _ ...func(*eventbridge.Options)) (*eventbridge.ListRulesOutput, error) {
	if f.listRules == nil {
		return &eventbridge.ListRulesOutput{}, nil
	}
	return f.listRules(in)
}

func (f *fakeEventBridge) ListTargetsByRule(_ context.Context, in *eventbridge.ListTargetsByRuleInput, _ ...func(*eventbridge.Options)) (*eventbridge.ListTargetsByRuleOutput, error) {
	if f.listTargetsByRule == nil {
		return &eventbridge.ListTargetsByRuleOutput{}, nil
	}
	return f.listTargetsByRule(in)
}

type fakeIam struct {
	listRoles                func(*iam.ListRolesInput) (*iam.ListRolesOutput, error)
	listUsers                func(*iam.ListUsersInput) (*iam.ListUsersOutput, error)
	listGroups               func(*iam.ListGroupsInput) (*iam.ListGroupsOutput, error)
	listPolicies             func(*iam.ListPoliciesInput) (*iam.ListPoliciesOutput, error)
	listAttachedRolePolicies func(*iam.ListAttachedRolePoliciesInput) (*iam.ListAttachedRolePoliciesOutput, error)
	listGroupsForUser        func(*iam.ListGroupsForUserInput) (*iam.ListGroupsForUserOutput, error)
}

func (f *fakeIam) ListRoles(_ context.Context, in *iam.ListRolesInput, _ ...func(*iam.Options)) (*iam.ListRolesOutput, error) {
	if f.listRoles == nil {
		return &iam.ListRolesOutput{}, nil
	}
	return f.listRoles(in)
}

func (f *fakeIam) ListUsers(_ context.Context, in *iam.ListUsersInput, _ ...func(*iam.Options)) (*iam.ListUsersOutput, error) {
	if f.listUsers == nil {
		return &iam.ListUsersOutput{}, nil
	}
	return f.listUsers(in)
}

func (f *fakeIam) ListGroups(_ context.Context, in *iam.ListGroupsInput, _ ...func(*iam.Options)) (*iam.ListGroupsOutput, error) {
	if f.listGroups == nil {
		return &iam.ListGroupsOutput{}, nil
	}
	return f.listGroups(in)
}

func (f *fakeIam) ListPolicies(_ context.Context, in *iam.ListPoliciesInput, _ ...func(*iam.Options)) (*iam.ListPoliciesOutput, error) {
	if f.listPolicies == nil {
		return &iam.ListPoliciesOutput{}, nil
	}
	return f.listPolicies(in)
}

func (f *fakeIam) ListAttachedRolePolicies(_ context.Context, in *iam.ListAttachedRolePoliciesInput, _ ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
	if f.listAttachedRolePolicies == nil {
		return &iam.ListAttachedRolePoliciesOutput{}, nil
	}
	return f.listAttachedRolePolicies(in)
}

func (f *fakeIam) ListGroupsForUser(_ context.Context, in *iam.ListGroupsForUserInput, _ ...func(*iam.Options)) (*iam.ListGroupsForUserOutput, error) {
	if f.listGroupsForUser == nil {
		return &iam.ListGroupsForUserOutput{}, nil
	}
	return f.listGroupsForUser(in)
}

type fakeKinesis struct {
	listStreams func(*kinesis.ListStreamsInput) (*kinesis.ListStreamsOutput, error)
}

func (f *fakeKinesis) ListStreams(_ context.Context, in *kinesis.ListStreamsInput, _ ...func(*kinesis.Options)) (*kinesis.ListStreamsOutput, error) {
	if f.listStreams == nil {
		return &kinesis.ListStreamsOutput{}, nil
	}
	return f.listStreams(in)
}

type fakeKms struct {
	listKeys    func(*kms.ListKeysInput) (*kms.ListKeysOutput, error)
	describeKey func(*kms.DescribeKeyInput) (*kms.DescribeKeyOutput, error)
}

func (f *fakeKms) ListKeys(_ context.Context, in *kms.ListKeysInput, _ ...func(*kms.Options)) (*kms.ListKeysOutput, error) {
	if f.listKeys == nil {
		return &kms.ListKeysOutput{}, nil
	}
	return f.listKeys(in)
}

func (f *fakeKms) DescribeKey(_ context.Context, in *kms.DescribeKeyInput, _ ...func(*kms.Options)) (*kms.DescribeKeyOutput, error) {
	if f.describeKey == nil {
		return &kms.DescribeKeyOutput{}, nil
	}
	return f.describeKey(in)
}

type fakeLambda struct {
	listFunctions func(*lambda.ListFunctionsInput) (*lambda.ListFunctionsOutput, error)
}

func (f *fakeLambda) ListFunctions(_ context.Context, in *lambda.ListFunctionsInput, _ ...func(*lambda.Options)) (*lambda.ListFunctionsOutput, error) {
	if f.listFunctions == nil {
		return &lambda.ListFunctionsOutput{}, nil
	}
	return f.listFunctions(in)
}

type fakeOpenSearch struct {
	listDomainNames func(*opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error)
	describeDomain  func(*opensearch.DescribeDomainInput) (*opensearch.DescribeDomainOutput, error)
}

func (f *fakeOpenSearch) ListDomainNames(_ context.Context, in *opensearch.ListDomainNamesInput, _ ...func(*opensearch.Options)) (*opensearch.ListDomainNamesOutput, error) {
	if f.listDomainNames == nil {
		return &opensearch.ListDomainNamesOutput{}, nil
	}
	return f.listDomainNames(in)
}

func (f *fakeOpenSearch) DescribeDomain(_ context.Context, in *opensearch.DescribeDomainInput, _ ...func(*opensearch.Options)) (*opensearch.DescribeDomainOutput, error) {
	if f.describeDomain == nil {
		return &opensearch.DescribeDomainOutput{}, nil
	}
	return f.describeDomain(in)
}

type fakeRds struct {
	describeDBInstances func(*rds.DescribeDBInstancesInput) (*rds.DescribeDBInstancesOutput, error)
}

func (f *fakeRds) DescribeDBInstances(_ context.Context, in *rds.DescribeDBInstancesInput, _ ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	if f.describeDBInstances == nil {
		return &rds.DescribeDBInstancesOutput{}, nil
	}
	return f.describeDBInstances(in)
}

type fakeRedshift struct {
	describeClusters func(*redshift.DescribeClustersInput) (*redshift.DescribeClustersOutput, error)
}

func (f *fakeRedshift) DescribeClusters(_ context.Context, in *redshift.DescribeClustersInput, _ ...func(*redshift.Options)) (*redshift.DescribeClustersOutput, error) {
	if f.describeClusters == nil {
		return &redshift.DescribeClustersOutput{}, nil
	}
	return f.describeClusters(in)
}

type fakeRoute53 struct {
	listHostedZones func(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error)
}

func (f *fakeRoute53) ListHostedZones(_ context.Context, in *route53.ListHostedZonesInput, _ ...func(*route53.Options)) (*route53.ListHostedZonesOutput, error) {
	if f.listHostedZones == nil {
		return &route53.ListHostedZonesOutput{}, nil
	}
	return f.listHostedZones(in)
}

type fakeS3 struct {
	listBuckets func(*s3.ListBucketsInput) (*s3.ListBucketsOutput, error)
}

func (f *fakeS3) ListBuckets(_ context.Context, in *s3.ListBucketsInput, _ ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	if f.listBuckets == nil {
		return &s3.ListBucketsOutput{}, nil
	}
	return f.listBuckets(in)
}

type fakeSecretsManager struct {
	listSecrets func(*secretsmanager.ListSecretsInput) (*secretsmanager.ListSecretsOutput, error)
}

func (f *fakeSecretsManager) ListSecrets(_ context.Context, in *secretsmanager.ListSecretsInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	if f.listSecrets == nil {
		return &secretsmanager.ListSecretsOutput{}, nil
	}
	return f.listSecrets(in)
}

type fakeSes struct {
	listEmailIdentities func(*sesv2.ListEmailIdentitiesInput) (*sesv2.ListEmailIdentitiesOutput, error)
}

func (f *fakeSes) ListEmailIdentities(_ context.Context, in *sesv2.ListEmailIdentitiesInput, _ ...func(*sesv2.Options)) (*sesv2.ListEmailIdentitiesOutput, error) {
	if f.listEmailIdentities == nil {
		return &sesv2.ListEmailIdentitiesOutput{}, nil
	}
	return f.listEmailIdentities(in)
}

type fakeSfn struct {
	listStateMachines func(*sfn.ListStateMachinesInput) (*sfn.ListStateMachinesOutput, error)
}

func (f *fakeSfn) ListStateMachines(_ context.Context, in *sfn.ListStateMachinesInput, _ ...func(*sfn.Options)) (*sfn.ListStateMachinesOutput, error) {
	if f.listStateMachines == nil {
		return &sfn.ListStateMachinesOutput{}, nil
	}
	return f.listStateMachines(in)
}

type fakeSns struct {
	listTopics func(*sns.ListTopicsInput) (*sns.ListTopicsOutput, error)
}

func (f *fakeSns) ListTopics(_ context.Context, in *sns.ListTopicsInput, _ ...func(*sns.Options)) (*sns.ListTopicsOutput, error) {
	if f.listTopics == nil {
		return &sns.ListTopicsOutput{}, nil
	}
	return f.listTopics(in)
}

type fakeSqs struct {
	listQueues func(*sqs.ListQueuesInput) (*sqs.ListQueuesOutput, error)
}

func (f *fakeSqs) ListQueues(_ context.Context, in *sqs.ListQueuesInput, _ ...func(*sqs.Options)) (*sqs.ListQueuesOutput, error) {
	if f.listQueues == nil {
		return &sqs.ListQueuesOutput{}, nil
	}
	return f.listQueues(in)
}
