// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
)

// completeness_census_test.go — EVERY SDK CALL'S FAILURE REACHES THE VERDICT,
// asserted over the call sites rather than at the boundary that aggregates them.
//
// THE HAZARD IS NAMED IN collector.go's OWN DOC COMMENT and it is the reason this
// census exists: "Asserting the second over a partial walk is how a throttled
// service deletes half a graph". Every registered custom family is on FULL-REPLACE
// deletion, so walk_complete=true tells the server that every record this walk did
// not name is gone. A read that fails silently therefore does not merely lose an
// edge for one collect — it DELETES that edge, on the next collect, permanently,
// while the result claims a complete enumeration of the account.
//
// WHY THE BOUNDARY TEST WAS NOT ENOUGH. runServiceWalks has a partial-walk test
// and it passes: a service walk that returns an error does mark the account
// incomplete. But six reads sat BELOW that boundary, inside service walks,
// returning bare on error — and every one of them was invisible to a test of the
// boundary, because the boundary never saw them. The requirement is a property of
// each read, so the census is over each read.
//
// THE THIRD OUTCOME IS THE POINT. A per-resource read has three possible
// dispositions and only two of them were reachable in the code this replaces:
// fail the whole service walk (which throws away everything else it read), or
// swallow (which is the deletion hazard). The third — keep what was read AND mark
// the account incomplete — is what recordSubreadFailure makes available, and it
// is the only one that is both complete and truthful.

func TestCensus_EverySDKCallsFailureReachesTheVerdict(t *testing.T) {
	sites := sdkCallSites(t)
	if len(sites) == 0 {
		t.Fatal("the census found no SDK call sites at all, so it would pass on an empty walk")
	}

	var offenders []string
	byDisposition := map[errorDisposition]int{}
	for _, s := range sites {
		byDisposition[s.ErrorReaches]++
		if s.ErrorReaches == errUnknown {
			offenders = append(offenders, fmt.Sprintf("%s:%d w.clients.%s.%s", s.File, s.Line, s.Service, s.Operation))
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("%d SDK call(s) sit in a function that neither RETURNS an error nor calls "+
			"recordSubreadFailure, so a denied or throttled read is invisible to the completeness verdict:\n"+
			"  %s\n\nThe walk then asserts walk_complete=true over an account it did not fully enumerate, and "+
			"because a registered custom family is on FULL-REPLACE deletion the server deletes every record "+
			"the failed read would have named. Either return the error, or record it with "+
			"recordSubreadFailure(operation, target, err) and keep going.",
			len(offenders), strings.Join(offenders, "\n  "))
	}

	// BOTH DISPOSITIONS PRESENT, which is what keeps the assertion above from
	// being satisfiable by a trivial reading. If nothing recorded, the census
	// would be asserting only that functions return errors, and the six sub-reads
	// this was written for would be back the moment one is added.
	if byDisposition[errRecorded] == 0 {
		t.Error("not one call site records its failure through recordSubreadFailure. Either the recorder is " +
			"gone or the classifier stopped seeing it; in both cases this census is no longer observing the " +
			"per-resource half of the completeness contract, which is the half it was written for.")
	}
	if byDisposition[errReturned] == 0 {
		t.Error("not one call site propagates its error by return, which cannot be true of a walk whose " +
			"service functions all return error — the classifier is misreading the enclosing functions.")
	}
	t.Logf("census: %d SDK call sites — %d returned, %d recorded, %d neither",
		len(sites), byDisposition[errReturned], byDisposition[errRecorded], byDisposition[errUnknown])
}

// deniedRead is the error an IAM policy produces for a read the principal is not
// authorized to make. It is the exact failure mode the completeness contract
// exists for: not an outage, not a bug, just a permission that was tightened
// between two collects.
var deniedRead = errors.New("AccessDeniedException: User is not authorized to perform this operation")

// TestCompleteness_ADeniedSubReadMakesTheAccountIncomplete is the BEHAVIORAL arm,
// one subtest per per-resource read.
//
// The census above is structural: it reads the source and says every failure has
// somewhere to go. This drives the real walk with one sub-read denied and asserts
// the THREE things that matter TOGETHER — the account is INCOMPLETE, the reason
// NAMES the operation, and the rest of the account still ships. Each alone is
// satisfiable by a wrong fix: failing the whole walk gives incomplete with nothing
// shipped, and swallowing gives everything shipped under a false complete. Only
// all three together describe the outcome the deletion hazard requires.
func TestCompleteness_ADeniedSubReadMakesTheAccountIncomplete(t *testing.T) {
	// THE HEALTHY BASELINE, measured once through the same walk, so "the rest of
	// the account still ships" is a comparison rather than an adjective.
	healthy := runWalk(t, fixtureClients(), Params{})

	for _, tc := range []struct {
		operation string
		deny      func(*Clients)
	}{{
		operation: "elbv2.DescribeListeners",
		deny: func(c *Clients) {
			c.ELBv2.(*fakeElbv2).describeListeners = func(*elbv2.DescribeListenersInput) (*elbv2.DescribeListenersOutput, error) {
				return nil, deniedRead
			}
		},
	}, {
		operation: "elbv2.DescribeTargetHealth",
		deny: func(c *Clients) {
			c.ELBv2.(*fakeElbv2).describeTargetHealth = func(*elbv2.DescribeTargetHealthInput) (*elbv2.DescribeTargetHealthOutput, error) {
				return nil, deniedRead
			}
		},
	}, {
		operation: "apigateway.GetBasePathMappings",
		deny: func(c *Clients) {
			c.APIGateway.(*fakeApiGateway).getBasePathMappings = func(*apigateway.GetBasePathMappingsInput) (*apigateway.GetBasePathMappingsOutput, error) {
				return nil, deniedRead
			}
		},
	}, {
		operation: "eventbridge.ListTargetsByRule",
		deny: func(c *Clients) {
			c.EventBridge.(*fakeEventBridge).listTargetsByRule = func(*eventbridge.ListTargetsByRuleInput) (*eventbridge.ListTargetsByRuleOutput, error) {
				return nil, deniedRead
			}
		},
	}, {
		operation: "ecs.DescribeTaskDefinition",
		deny: func(c *Clients) {
			c.ECS.(*fakeEcs).describeTaskDefinition = func(*ecs.DescribeTaskDefinitionInput) (*ecs.DescribeTaskDefinitionOutput, error) {
				return nil, deniedRead
			}
		},
	}, {
		// THE LAST TWO WERE FOUND BY THE CENSUS ABOVE, not by the review that
		// prompted this file. The review censused the sites matching two `return`
		// shapes and named six; the census parses every SDK call and classifies its
		// enclosing function, and it named these two as well — both write their
		// error into a compound condition (`err != nil || out.X == nil`) that the
		// shape-matched search did not see. That difference is the argument for
		// the census over a list.
		operation: "dynamodb.DescribeContinuousBackups",
		deny: func(c *Clients) {
			c.DynamoDB.(*fakeDynamoDB).describeContinuousBackups = func(*dynamodb.DescribeContinuousBackupsInput) (*dynamodb.DescribeContinuousBackupsOutput, error) {
				return nil, deniedRead
			}
		},
	}, {
		operation: "acm.DescribeCertificate",
		deny: func(c *Clients) {
			c.ACM.(*fakeAcm).describeCertificate = func(*acm.DescribeCertificateInput) (*acm.DescribeCertificateOutput, error) {
				return nil, deniedRead
			}
		},
	}} {
		t.Run(tc.operation, func(t *testing.T) {
			clients := fixtureClients()
			tc.deny(clients)
			res := runWalk(t, clients, Params{})

			// ONE: the account is not complete.
			if !res.Complete.IsAsserted() {
				t.Fatal("the walk returned an unasserted Completeness, which the framework refuses")
			}
			if res.Complete.IsComplete() {
				t.Fatalf("%s was DENIED and the walk still asserts a COMPLETE enumeration of the account. "+
					"Every registered custom family is on full-replace deletion, so this assertion tells the "+
					"server that every record the denied read would have named is gone — and the next collect "+
					"deletes them.", tc.operation)
			}

			// TWO: the reason names the read, so the operator knows which
			// permission to look at.
			if !strings.Contains(res.Complete.Reason(), tc.operation) {
				t.Errorf("the incomplete reason does not name %s.\nreason: %s\nA reason that does not name the "+
					"failed read sends the operator to the console with nothing to look up.",
					tc.operation, res.Complete.Reason())
			}

			// THREE: the rest of the account still ships. A fix that failed the
			// whole service walk would satisfy the first two assertions and throw
			// away every other resource that service enumerated.
			const keepAtLeast = 0.9
			if float64(len(res.Nodes)) < keepAtLeast*float64(len(healthy.Nodes)) {
				t.Errorf("denying %s cost %d of %d nodes, more than the %.0f%% this read could legitimately "+
					"account for. The failure is being propagated up and failing a whole service walk, which "+
					"throws away resources that were read successfully.",
					tc.operation, len(healthy.Nodes)-len(res.Nodes), len(healthy.Nodes), (1-keepAtLeast)*100)
			}
			if len(res.Nodes) == 0 {
				t.Errorf("denying %s produced an empty result; a per-resource denial must not empty the account",
					tc.operation)
			}

			// AND THE EDGES ARE ACTUALLY MISSING, which is the known positive for
			// the whole subtest: if the denial changed nothing at all, the three
			// assertions above would be measuring a walk that never called the
			// denied read.
			if len(res.Edges) >= len(healthy.Edges) {
				t.Errorf("denying %s left the edge count at %d against a healthy %d, so the denial reached "+
					"nothing. The assertions above would then be describing an ordinary walk.",
					tc.operation, len(res.Edges), len(healthy.Edges))
			}
		})
	}
}
