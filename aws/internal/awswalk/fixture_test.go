// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// fixture_test.go — the FULLY-POPULATED FAKE ACCOUNT, and the harness that runs
// a collector against it.
//
// ONE FIXTURE, AND IT HOLDS AT LEAST ONE OF EVERY RESOURCE TYPE AND EVERY EDGE
// RELATIONSHIP the parity floor names. That is what makes the census in
// parity_test.go a real gate rather than an assertion about whichever services a
// test author remembered: a service walk that stops emitting a type turns the
// census red BY NAME, and a fixture missing a type does the same, so neither can
// drift without a failure.
//
// NOTHING HERE TOUCHES A NETWORK, an AWS credential, a file or a process. Every
// response is an in-memory SDK struct; the collector's two seams — the credential
// chain and the client constructor — are replaced with functions.

const (
	fixtureAccount = "123456789012"
	fixtureRegion  = "us-east-1"
	fixtureVPC     = "vpc-0aaa"
	fixtureSubnet  = "subnet-0bbb"
	fixtureSG      = "sg-0ccc"
	fixtureNACL    = "acl-0ddd"
	fixtureInst    = "i-0eee"
	fixtureVolume  = "vol-0fff"
	// fixturePeerAccount is a DIFFERENT account, and every cross-account arm in
	// the suite keys on it: a peering, a security-group rule and an IAM trust
	// statement each name it, and each produces an edge whose far endpoint this
	// walk cannot materialize.
	fixturePeerAccount = "210987654321"
	fixturePeerVPC     = "vpc-0999"
	fixtureIssuerHost  = "oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE0000"
	fixtureClusterARN  = "arn:aws:eks:us-east-1:123456789012:cluster/prod"
	fixtureIRSARole    = "arn:aws:iam::123456789012:role/irsa-app"
	fixtureZoneID      = "arn:aws:route53:::hostedzone/Z1EXAMPLE"
)

// fixtureClients returns the fully-populated fake account.
//
// IT IS BUILT FROM PER-SERVICE HELPERS rather than one literal, because the
// literal would be two thousand lines and a reader looking for one service's
// shape would have to scan all of it.
func fixtureClients() *Clients {
	return &Clients{
		ACM:            fixtureACM(),
		APIGateway:     fixtureAPIGateway(),
		APIGatewayV2:   fixtureAPIGatewayV2(),
		CloudFront:     fixtureCloudFront(),
		CloudTrail:     fixtureCloudTrail(),
		CloudWatch:     fixtureCloudWatch(),
		CloudWatchLogs: fixtureCloudWatchLogs(),
		DynamoDB:       fixtureDynamoDB(),
		EC2:            fixtureEC2(),
		ECR:            fixtureECR(),
		ECS:            fixtureECS(),
		EFS:            fixtureEFS(),
		EKS:            fixtureEKS(),
		ElastiCache:    fixtureElastiCache(),
		ELBv2:          fixtureELBv2(),
		EventBridge:    fixtureEventBridge(),
		IAM:            fixtureIAM(),
		Kinesis:        fixtureKinesis(),
		KMS:            fixtureKMS(),
		Lambda:         fixtureLambda(),
		OpenSearch:     fixtureOpenSearch(),
		RDS:            fixtureRDS(),
		Redshift:       fixtureRedshift(),
		Route53:        fixtureRoute53(),
		S3:             fixtureS3(),
		SecretsManager: fixtureSecretsManager(),
		SES:            fixtureSES(),
		SFN:            fixtureSFN(),
		SNS:            fixtureSNS(),
		SQS:            fixtureSQS(),
	}
}

// newTestCollector returns a collector wired to the supplied clients, with both
// of its real-world seams replaced.
//
// THE IDENTITY SEAM IS REPLACED TOO, and not just the client constructor: without
// it every walk would call STS over the network to learn the account id, which is
// the one call the collector makes before any service walk.
func newTestCollector(clients *Clients) *Collector {
	return &Collector{
		loadConfig: func(_ context.Context, region string) (aws.Config, error) {
			if region == "" {
				region = fixtureRegion
			}
			return aws.Config{Region: region}, nil
		},
		newClients: func(aws.Config) *Clients { return clients },
		identity:   func(context.Context, aws.Config) (string, error) { return fixtureAccount, nil },
	}
}

// runWalk drives one collect against the supplied clients and fails the test on
// an outright error.
func runWalk(t *testing.T, clients *Clients, params Params) framework.Result {
	t.Helper()
	res, err := newTestCollector(clients).Walk(context.Background(), "acct", params, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("walk returned an error: %v", err)
	}
	return res
}

// resourceTypesIn returns the set of resource_type values a result carries.
func resourceTypesIn(res framework.Result) map[string]bool {
	out := map[string]bool{}
	for _, n := range res.Nodes {
		out[n.Metadata[MetaResourceType]] = true
	}
	return out
}

// edgeTypesIn returns the set of edge relationship types a result carries.
func edgeTypesIn(res framework.Result) map[string]bool {
	out := map[string]bool{}
	for _, e := range res.Edges {
		out[e.Type] = true
	}
	return out
}

// nodeIDs returns the set of node ids a result carries.
func nodeIDs(res framework.Result) map[string]bool {
	out := map[string]bool{}
	for _, n := range res.Nodes {
		out[n.ID] = true
	}
	return out
}

// nodesOfType returns every node of one resource type.
func nodesOfType(res framework.Result, resourceType string) []framework.Node {
	var out []framework.Node
	for _, n := range res.Nodes {
		if n.Metadata[MetaResourceType] == resourceType {
			out = append(out, n)
		}
	}
	return out
}

// edgesOfType returns every edge of one relationship type.
func edgesOfType(res framework.Result, edgeType string) []framework.Edge {
	var out []framework.Edge
	for _, e := range res.Edges {
		if e.Type == edgeType {
			out = append(out, e)
		}
	}
	return out
}

// hasEdge reports whether the result carries the exact triple.
func hasEdge(res framework.Result, from, to, edgeType string) bool {
	for _, e := range res.Edges {
		if e.FromID == from && e.ToID == to && e.Type == edgeType {
			return true
		}
	}
	return false
}
