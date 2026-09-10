// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	apigatewaytypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

// pagination_sites_test.go — THE PAGE-TWO PROBE for each read that used to stop
// at page one.
//
// WHAT THIS ADDS THAT THE CENSUS DOES NOT. The census in
// pagination_census_test.go is structural: it says the call is lexically inside a
// paginate call. That is necessary and it is not sufficient, because a call can
// be inside the paginator while its ADAPTERS are wrong — an input that never
// carries the token back, or a nextToken function returning the wrong field —
// and the loop then makes exactly one request and terminates looking correct.
//
// SO EACH SITE GETS A TWO-PAGE ACCOUNT. The fake returns a first page with a
// continuation token and a second page without one, each carrying a DIFFERENT
// resource, and the test asserts BOTH edges are present. The page-one edge is the
// same-run control: if the walk never reached the read at all, page one would be
// missing too and the page-two assertion would be reporting a dead probe rather
// than a dropped page.
//
// AND THE REQUEST COUNT IS ASSERTED, because "both edges present" is also
// satisfiable by a fake that ignores the token and returns everything at once.
// Two calls is what a two-page read costs.

func TestPagination_DescribeListenersReadsPageTwo(t *testing.T) {
	const (
		certPageOne = "arn:aws:acm:us-east-1:123456789012:certificate/page-one"
		certPageTwo = "arn:aws:acm:us-east-1:123456789012:certificate/page-two"
	)
	calls := 0
	clients := fixtureClients()
	clients.ELBv2.(*fakeElbv2).describeListeners = func(in *elbv2.DescribeListenersInput) (*elbv2.DescribeListenersOutput, error) {
		calls++
		if in.Marker == nil {
			return &elbv2.DescribeListenersOutput{
				Listeners: []elbv2types.Listener{{
					ListenerArn:  new(fixtureLBARN + "/listener/1"),
					Certificates: []elbv2types.Certificate{{CertificateArn: new(certPageOne)}},
				}},
				NextMarker: new("page-2"),
			}, nil
		}
		return &elbv2.DescribeListenersOutput{Listeners: []elbv2types.Listener{{
			ListenerArn:  new(fixtureLBARN + "/listener/2"),
			Certificates: []elbv2types.Certificate{{CertificateArn: new(certPageTwo)}},
		}}}, nil
	}

	res := runWalk(t, clients, Params{})
	assertPageEdges(t, res, "elbv2.DescribeListeners", calls,
		edgeProbe{fixtureLBARN, certPageOne, EdgeUsesCert},
		edgeProbe{fixtureLBARN, certPageTwo, EdgeUsesCert})
}

func TestPagination_GetBasePathMappingsReadsPageTwo(t *testing.T) {
	const (
		apiPageOne = "api-page-one"
		apiPageTwo = "api-page-two"
	)
	calls := 0
	clients := fixtureClients()
	clients.APIGateway.(*fakeApiGateway).getBasePathMappings = func(
		in *apigateway.GetBasePathMappingsInput,
	) (*apigateway.GetBasePathMappingsOutput, error) {
		calls++
		if in.Position == nil {
			return &apigateway.GetBasePathMappingsOutput{
				Items:    []apigatewaytypes.BasePathMapping{{RestApiId: new(apiPageOne), BasePath: new("/one")}},
				Position: new("page-2"),
			}, nil
		}
		return &apigateway.GetBasePathMappingsOutput{
			Items: []apigatewaytypes.BasePathMapping{{RestApiId: new(apiPageTwo), BasePath: new("/two")}},
		}, nil
	}

	res := runWalk(t, clients, Params{})
	from := apiGWDomainNodeID(t, res)
	assertPageEdges(t, res, "apigateway.GetBasePathMappings", calls,
		edgeProbe{from, restAPIARN(fixtureRegion, apiPageOne), EdgeBoundTo},
		edgeProbe{from, restAPIARN(fixtureRegion, apiPageTwo), EdgeBoundTo})
}

func TestPagination_ListTargetsByRuleReadsPageTwo(t *testing.T) {
	const (
		targetPageOne = "arn:aws:lambda:us-east-1:123456789012:function:page-one"
		targetPageTwo = "arn:aws:lambda:us-east-1:123456789012:function:page-two"
	)
	calls := 0
	clients := fixtureClients()
	clients.EventBridge.(*fakeEventBridge).listTargetsByRule = func(
		in *eventbridge.ListTargetsByRuleInput,
	) (*eventbridge.ListTargetsByRuleOutput, error) {
		calls++
		if in.NextToken == nil {
			return &eventbridge.ListTargetsByRuleOutput{
				Targets:   []eventbridgetypes.Target{{Id: new("t1"), Arn: new(targetPageOne)}},
				NextToken: new("page-2"),
			}, nil
		}
		return &eventbridge.ListTargetsByRuleOutput{
			Targets: []eventbridgetypes.Target{{Id: new("t2"), Arn: new(targetPageTwo)}},
		}, nil
	}

	res := runWalk(t, clients, Params{})
	assertPageEdges(t, res, "eventbridge.ListTargetsByRule", calls,
		edgeProbe{fixtureRuleARN, targetPageOne, EdgeTriggers},
		edgeProbe{fixtureRuleARN, targetPageTwo, EdgeTriggers})
}

// edgeProbe is one edge a page is expected to produce.
type edgeProbe struct{ from, to, edgeType string }

// apiGWDomainNodeID resolves the API Gateway custom domain's node id FROM THE
// RESULT rather than re-composing it in the test.
//
// A TEST THAT RE-COMPOSES AN ID under test is checking its own arithmetic: it
// would keep passing if the walk's composition changed, because the expectation
// would change with it. Reading the id out of the emitted node means the probe is
// anchored to what the walk actually produced.
func apiGWDomainNodeID(t *testing.T, res framework.Result) string {
	t.Helper()
	for _, n := range res.Nodes {
		if n.Metadata[MetaResourceType] == ResourceTypeAPIGWDomain {
			return n.ID
		}
	}
	t.Fatalf("the walk emitted no %s node, so the base-path-mapping probe has no domain to hang its edges on",
		ResourceTypeAPIGWDomain)
	return ""
}

// assertPageEdges checks that every probed edge landed and that the read made
// exactly two requests.
//
// THE FIRST PROBE IS THE CONTROL. It is the page-ONE edge, and it is asserted for
// the same reason every absence claim in this repository carries a known
// positive: without it, a walk that never called the read at all would report the
// page-two edge as missing and be indistinguishable from a walk that dropped it.
func assertPageEdges(t *testing.T, res framework.Result, operation string, calls int, probes ...edgeProbe) {
	t.Helper()

	for i, p := range probes {
		found := false
		for _, e := range res.Edges {
			if e.FromID == p.from && e.ToID == p.to && e.Type == p.edgeType {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if i == 0 {
			t.Fatalf("%s: the PAGE-ONE edge %s -[%s]-> %s is absent, so this probe never reached the read and "+
				"proves nothing about pagination. Fix the fixture before reading the page-two result.",
				operation, p.from, p.edgeType, p.to)
		}
		t.Errorf("%s: the PAGE-TWO edge %s -[%s]-> %s is absent while the page-one edge landed. The read stops "+
			"at the first page: either the input does not carry the continuation token back, or the nextToken "+
			"adapter returns the wrong field. An account larger than one page is then reported as a small one.",
			operation, p.from, p.edgeType, p.to)
	}

	if calls != 2 {
		t.Errorf("%s was called %d time(s) over a two-page account, want exactly 2. One call means the token "+
			"was never followed; more than two means the loop is re-requesting a page.", operation, calls)
	}
}
