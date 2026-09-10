// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// walk_test.go — the WALK'S OWN behavior: determinism, completeness, and the
// error arms.

// TestWalk_TwoCollectsOfAnUnchangedAccountAreIdentical is R6's precondition, and
// it is stricter than it looks.
//
// The client's carry-forward diff keys on the NODE ID, so a second collect of an
// unchanged account writes zero rows only if this collector emits every node with
// a byte-identical id AND byte-identical content. A collect timestamp in a
// content blob, a Go map ranged in place, or a slice appended from the
// bounded-concurrency fan-out would each defeat that — and the first two would
// survive a set-comparison, which is why this asserts the full ordered slices
// rather than sets.
func TestWalk_TwoCollectsOfAnUnchangedAccountAreIdentical(t *testing.T) {
	first := runWalk(t, fixtureClients(), Params{})
	second := runWalk(t, fixtureClients(), Params{})

	if !reflect.DeepEqual(first.Nodes, second.Nodes) {
		t.Errorf("two collects of one unchanged account produced different NODE slices; the carry-forward diff "+
			"would write a row for every difference.\nfirst=%d nodes second=%d nodes\n%s",
			len(first.Nodes), len(second.Nodes), firstNodeDifference(first, second))
	}
	if !reflect.DeepEqual(first.Edges, second.Edges) {
		t.Errorf("two collects of one unchanged account produced different EDGE slices; first=%d second=%d",
			len(first.Edges), len(second.Edges))
	}
}

// TestWalk_NodeIDsAreByteStableAcrossRuns is the STRICTER half, stated on its own
// because set equality would pass a collector whose ids varied between runs while
// its node SET stayed the same size.
//
// It runs the walk several times and requires the id set to be identical every
// time. A run-varying component in an id — a counter, a timestamp, a random
// suffix — moves a node into the diff's changed set on every collect, and nothing
// else in this suite would notice.
func TestWalk_NodeIDsAreByteStableAcrossRuns(t *testing.T) {
	base := nodeIDs(runWalk(t, fixtureClients(), Params{}))
	if len(base) == 0 {
		t.Fatal("the first walk emitted no nodes, so this test would pass on nothing")
	}
	for i := range 4 {
		got := nodeIDs(runWalk(t, fixtureClients(), Params{}))
		if !reflect.DeepEqual(base, got) {
			t.Fatalf("run %d produced a different node id set than the first run; a node id must be "+
				"byte-identical across collects or carry-forward writes a row for it every time", i+2)
		}
	}
}

// firstNodeDifference names the first differing node, so a failure above points
// at one resource rather than at two slice lengths.
func firstNodeDifference(a, b framework.Result) string {
	for i := range a.Nodes {
		if i >= len(b.Nodes) {
			return "the second walk is shorter, first missing node: " + a.Nodes[i].ID
		}
		if !reflect.DeepEqual(a.Nodes[i], b.Nodes[i]) {
			return "first differing node at index " + a.Nodes[i].ID
		}
	}
	return ""
}

// TestWalk_OneFailingServiceShipsAnIncompleteResult is the partial-walk arm.
//
// THE STAKE IS DELETION. A result asserting a COMPLETE walk tells the server that
// every record this collect did not name is gone; asserting it over a walk where
// a service was denied or throttled is how a permissions change deletes half a
// graph. So the result must SHIP — the rest of the account is worth carrying —
// with walk_complete FALSE and the failed service named.
func TestWalk_OneFailingServiceShipsAnIncompleteResult(t *testing.T) {
	denied := errors.New("AccessDeniedException: User is not authorized to perform ec2:DescribeVpcs")
	clients := fixtureClients()
	ec2Fake := clients.EC2.(*fakeEC2)
	ec2Fake.describeVpcs = func(*ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error) {
		return nil, denied
	}

	res := runWalk(t, clients, Params{})

	if res.Complete.IsComplete() {
		t.Fatal("a walk whose vpc service was denied asserted a COMPLETE enumeration; that arms the server's " +
			"deletion phase over every resource the failed service would have named")
	}
	if !strings.Contains(res.Complete.Reason(), "vpc") {
		t.Errorf("the incomplete reason must name the failed service; got %q", res.Complete.Reason())
	}
	if !strings.Contains(res.Complete.Reason(), "AccessDenied") {
		t.Errorf("the incomplete reason must carry the cause an operator can act on; got %q", res.Complete.Reason())
	}
	// THE REST OF THE ACCOUNT STILL SHIPS. Without this the test would pass on a
	// collector that failed the whole walk, which is a different behavior.
	if len(res.Nodes) == 0 {
		t.Error("a partial walk must still carry what it did enumerate")
	}
	if len(nodesOfType(res, ResourceTypeIAMRole)) == 0 {
		t.Error("a failure in the vpc walk must not suppress an unrelated service's nodes")
	}
}

// TestWalk_EveryFailingServiceIsNamedInAStableOrder guards the reason string
// itself: the fan-out finishes in whatever order the API answers, so an unsorted
// reason would differ between two runs that failed identically.
func TestWalk_EveryFailingServiceIsNamedInAStableOrder(t *testing.T) {
	build := func() *Clients {
		clients := fixtureClients()
		clients.EC2.(*fakeEC2).describeVpcs = func(*ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error) {
			return nil, errors.New("vpc denied")
		}
		clients.IAM.(*fakeIam).listUsers = func(*iam.ListUsersInput) (*iam.ListUsersOutput, error) {
			return nil, errors.New("users denied")
		}
		return clients
	}
	first := runWalk(t, build(), Params{}).Complete.Reason()
	for range 5 {
		if got := runWalk(t, build(), Params{}).Complete.Reason(); got != first {
			t.Fatalf("the incomplete reason is not stable across runs:\nfirst: %s\nlater: %s", first, got)
		}
	}
	if !strings.Contains(first, "vpc") || !strings.Contains(first, "iam-user") {
		t.Errorf("both failing services must be named; got %q", first)
	}
}

// TestWalk_CancellationYieldsAnIncompleteResult covers the context arm.
//
// A CANCELLED WALK IS ALWAYS INCOMPLETE, even when every walk that got to run
// succeeded: the walks that never started enumerated nothing, and a complete
// assertion over them is the same deletion hazard as a denied service.
func TestWalk_CancellationYieldsAnIncompleteResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := newTestCollector(fixtureClients()).Walk(ctx, "acct", Params{}, framework.ForeignContext{})
	if err != nil {
		// A cancelled context may surface either as a refused walk or as an
		// incomplete one depending on where it lands; both are correct and
		// neither may be a complete result.
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a cancelled walk must fail with the cancellation, got %v", err)
		}
		return
	}
	if res.Complete.IsComplete() {
		t.Fatal("a cancelled walk asserted a COMPLETE enumeration")
	}
	if !strings.Contains(res.Complete.Reason(), "cancel") {
		t.Errorf("the reason must name the cancellation; got %q", res.Complete.Reason())
	}
}

// TestWalk_AnEmptyAccountIsCompleteAndEmpty is the empty-response class, at the
// whole-account level.
//
// IT ASSERTS COMPLETE, and that distinction matters: an account with no resources
// is fully enumerated. Asserting incomplete for it would disable the server's
// deletion phase for every empty region, which is the state a decommissioned one
// is in and exactly when deletion matters.
func TestWalk_AnEmptyAccountIsCompleteAndEmpty(t *testing.T) {
	res := runWalk(t, emptyClients(), Params{})
	if !res.Complete.IsComplete() {
		t.Fatalf("an account where every service answered with nothing is fully enumerated; got incomplete: %s",
			res.Complete.Reason())
	}
	if len(res.Nodes) != 0 || len(res.Edges) != 0 {
		t.Fatalf("an empty account must produce no nodes and no edges; got %d nodes and %d edges",
			len(res.Nodes), len(res.Edges))
	}
}

// emptyClients is every service answering with an empty successful response,
// which is what each fake does by default.
func emptyClients() *Clients {
	return &Clients{
		ACM: &fakeAcm{}, APIGateway: &fakeApiGateway{}, APIGatewayV2: &fakeApiGatewayV2{},
		CloudFront: &fakeCloudFront{}, CloudTrail: &fakeCloudTrail{}, CloudWatch: &fakeCloudWatch{},
		CloudWatchLogs: &fakeCloudWatchLogs{}, DynamoDB: &fakeDynamoDB{}, EC2: &fakeEC2{},
		ECR: &fakeEcr{}, ECS: &fakeEcs{}, EFS: &fakeEfs{}, EKS: &fakeEks{},
		ElastiCache: &fakeElastiCache{}, ELBv2: &fakeElbv2{}, EventBridge: &fakeEventBridge{},
		IAM: &fakeIam{}, Kinesis: &fakeKinesis{}, KMS: &fakeKms{}, Lambda: &fakeLambda{},
		OpenSearch: &fakeOpenSearch{}, RDS: &fakeRds{}, Redshift: &fakeRedshift{},
		Route53: &fakeRoute53{}, S3: &fakeS3{}, SecretsManager: &fakeSecretsManager{},
		SES: &fakeSes{}, SFN: &fakeSfn{}, SNS: &fakeSns{}, SQS: &fakeSqs{},
	}
}

// TestWalk_AnUnresolvableCredentialFailsNamingTheChain covers R4's other half.
//
// THE MESSAGE IS THE POINT. config.LoadDefaultConfig resolves lazily and its own
// error rarely says what an operator should do, so the wrapper names every source
// the chain would have used. A test asserting only that an error occurred would
// pass on the bare SDK error.
func TestWalk_AnUnresolvableCredentialFailsNamingTheChain(t *testing.T) {
	c := &Collector{
		loadConfig: func(context.Context, string) (aws.Config, error) {
			return aws.Config{}, errors.New("no EC2 IMDS role found")
		},
	}
	_, err := c.Walk(context.Background(), "acct", Params{}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("an unresolvable credential must fail the collect")
	}
	for _, want := range []string{"AWS_ACCESS_KEY_ID", "AWS_PROFILE", "HOME", "AWS_WEB_IDENTITY_TOKEN_FILE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure must name the chain that was tried; %q is absent from: %v", want, err)
		}
	}
}

// TestWalk_AnUnusableCredentialFailsAtTheIdentityCall covers the arm where the
// chain resolved something the API rejects — an expired key, a revoked role.
func TestWalk_AnUnusableCredentialFailsAtTheIdentityCall(t *testing.T) {
	c := newTestCollector(fixtureClients())
	c.identity = func(context.Context, aws.Config) (string, error) {
		return "", errors.New("ExpiredToken: The security token included in the request is expired")
	}
	_, err := c.Walk(context.Background(), "acct", Params{}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("a credential the API rejects must fail the collect rather than producing an empty graph")
	}
	if !strings.Contains(err.Error(), "ExpiredToken") {
		t.Errorf("the failure must carry the cause; got %v", err)
	}
	if !strings.Contains(err.Error(), "AWS_PROFILE") {
		t.Errorf("the failure must name the chain that was tried; got %v", err)
	}
}

// TestWalk_AnIdentityWithNoAccountIsRefused covers the arm the walk cannot
// proceed past: every composed EC2-family ARN carries the account id, so an
// empty one would emit ids that resolve against nothing.
func TestWalk_AnIdentityWithNoAccountIsRefused(t *testing.T) {
	c := newTestCollector(fixtureClients())
	c.identity = func(context.Context, aws.Config) (string, error) { return "", nil }
	_, err := c.Walk(context.Background(), "acct", Params{}, framework.ForeignContext{})
	if err == nil || !strings.Contains(err.Error(), "no account id") {
		t.Fatalf("an identity with no account id must be refused naming the consequence; got %v", err)
	}
}

// TestWalk_NoRegionIsRefused covers the arm where neither the params nor the
// resolved config names one.
func TestWalk_NoRegionIsRefused(t *testing.T) {
	c := newTestCollector(fixtureClients())
	c.loadConfig = func(context.Context, string) (aws.Config, error) { return aws.Config{}, nil }
	_, err := c.Walk(context.Background(), "acct", Params{}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("a collect with no region anywhere must be refused rather than walking an unnamed region")
	}
	for _, want := range []string{"params.region", "AWS_REGION", "AWS_DEFAULT_REGION"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name every way to set a region; %q is absent from: %v", want, err)
		}
	}
}
