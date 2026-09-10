// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"errors"
	"strings"
	"testing"
)

// matrix_test.go — RESOURCE TYPE × RESPONSE SHAPE, the five classes every service
// walk meets.
//
// The five are: an EMPTY response, a SINGLE resource, a PAGINATED response, a
// resource MISSING every optional field, and an ERROR from the service. Three of
// them are covered whole-account rather than per-service, because that is where
// they are actually decided:
//
//   - EMPTY is every fake's default, so walk_test.go's empty-account arm runs it
//     for all thirty services at once.
//   - SINGLE is the fully-populated fixture, which parity_test.go walks.
//   - PAGINATED is pagination_test.go, over the four paging conventions the SDK
//     uses rather than over thirty services that share them.
//
// The two below are the ones that need per-service coverage, and each is a
// different failure: a nil pointer dereference kills the whole collect, and an
// error that aborted the account instead of one service would delete a graph.

// TestMatrix_ResourcesMissingEveryOptionalFieldDoNotPanic is the class that
// crashes rather than fails.
//
// THE AWS SDK MODELS ALMOST EVERY FIELD AS A POINTER, including ones the API
// populates in practice, so a walk that dereferenced one directly panics on the
// first response that omits it — and a panic in a service walk's goroutine takes
// the whole collector process down, not just that walk. This fixture carries the
// minimum the walk needs to emit a node and nothing else.
func TestMatrix_ResourcesMissingEveryOptionalFieldDoNotPanic(t *testing.T) {
	res := runWalk(t, minimalClients(), Params{})

	if len(res.Nodes) == 0 {
		t.Fatal("the minimal fixture emitted no nodes, so this test would pass on a walk that did nothing")
	}
	// EVERY NODE IS STILL WELL-FORMED. A walk that survived the nil fields by
	// emitting a node with no id would produce entries the sink drops silently and
	// a graph missing resources the account has.
	for _, n := range res.Nodes {
		if n.ID == "" {
			t.Errorf("a node was emitted with an empty id from a minimal response")
		}
		if n.Metadata[MetaResourceType] == "" {
			t.Errorf("node %s carries no resource_type", n.ID)
		}
	}
	if !res.Complete.IsComplete() {
		t.Errorf("a response missing optional fields is not a failed enumeration; got incomplete: %s",
			res.Complete.Reason())
	}
}

// TestMatrix_EveryNodeIsWellFormed asserts the invariants across the fully
// populated account, which is where a converter that forgot a field shows up.
func TestMatrix_EveryNodeIsWellFormed(t *testing.T) {
	res := fixtureResult(t)
	for _, n := range res.Nodes {
		if n.ID == "" || n.Type == "" {
			t.Errorf("node %+v has an empty id or type", n)
		}
		if n.SymbolName == "" {
			t.Errorf("node %s carries no name; that is the field a human recognizes a resource by", n.ID)
		}
		if n.Summary == "" {
			t.Errorf("node %s carries no summary; that is what search matches on", n.ID)
		}
		if n.Source != "aws" {
			t.Errorf("node %s carries source %q, want aws", n.ID, n.Source)
		}
		// THE REGION IS PRESENT OR ABSENT, NEVER EMPTY. An empty-string region
		// reads to a consumer as a region named "", which is a different claim
		// from a global resource having none.
		if region, ok := n.Metadata[MetaRegion]; ok && region == "" {
			t.Errorf("node %s carries an empty region key; a global resource omits the key entirely", n.ID)
		}
	}
}

// TestMatrix_GlobalResourcesCarryNoRegion pins which resources are global, since
// stamping one with the region of the collect that found it would be a fact about
// the collect rather than about the resource.
func TestMatrix_GlobalResourcesCarryNoRegion(t *testing.T) {
	res := fixtureResult(t)
	global := map[string]bool{
		ResourceTypeIAMRole: true, ResourceTypeIAMUser: true,
		ResourceTypeIAMGroup: true, ResourceTypeIAMPolicy: true,
		ResourceTypeS3Bucket: true, ResourceTypeCloudFrontDistribution: true,
		ResourceTypeRoute53HostedZone: true,
	}
	for _, n := range res.Nodes {
		rt := n.Metadata[MetaResourceType]
		_, hasRegion := n.Metadata[MetaRegion]
		if global[rt] && hasRegion {
			t.Errorf("node %s is a %s, which is global, and carries a region", n.ID, rt)
		}
		if !global[rt] && !hasRegion {
			t.Errorf("node %s is a %s, which is regional, and carries no region", n.ID, rt)
		}
	}
}

// TestMatrix_AnyServiceFailingIsolatesToThatService is the error class, run for
// EVERY service walk rather than for one.
//
// A walk that returned its error up the fan-out instead of into the failure map
// would abort the account, and the result would either fail the collect outright
// or — worse — ship a partial account asserting a COMPLETE walk. Both are
// invisible until a real account throttles one service.
func TestMatrix_AnyServiceFailingIsolatesToThatService(t *testing.T) {
	boom := errors.New("AccessDeniedException: the fixture denied this call")

	for _, tc := range []struct {
		service string
		break_  func(*Clients)
	}{
		{"vpc", func(c *Clients) { failEC2Vpcs(c, boom) }},
		{"ec2", func(c *Clients) { failEC2Instances(c, boom) }},
		{"iam-role", func(c *Clients) { failIAMRoles(c, boom) }},
		{"lambda", func(c *Clients) { failLambda(c, boom) }},
		{"s3", func(c *Clients) { failS3(c, boom) }},
		{"eks", func(c *Clients) { failEKS(c, boom) }},
		{"dynamodb", func(c *Clients) { failDynamoTables(c, boom) }},
		{"cloudtrail", func(c *Clients) { failCloudTrail(c, boom) }},
		{"opensearch", func(c *Clients) { failOpenSearch(c, boom) }},
	} {
		t.Run(tc.service, func(t *testing.T) {
			clients := fixtureClients()
			tc.break_(clients)
			res := runWalk(t, clients, Params{})

			if res.Complete.IsComplete() {
				t.Fatalf("the %s walk failed and the result still asserted a COMPLETE enumeration; that arms "+
					"the server's deletion phase over every resource that service would have named", tc.service)
			}
			if !strings.Contains(res.Complete.Reason(), tc.service) {
				t.Errorf("the incomplete reason must name the failed service %q; got %q",
					tc.service, res.Complete.Reason())
			}
			// THE SAME-RUN KNOWN POSITIVE: unrelated services still landed. Without
			// it, a collector that aborted the whole account on any failure would
			// pass every assertion above.
			if len(nodesOfType(res, ResourceTypeSQSQueue)) == 0 {
				t.Errorf("a failure in %s suppressed the sqs walk's nodes; a service failure isolates to that "+
					"service", tc.service)
			}
			if len(res.Nodes) < 10 {
				t.Errorf("a failure in %s left only %d nodes; the rest of the account must still ship",
					tc.service, len(res.Nodes))
			}
		})
	}
}

// TestMatrix_ConverterShapes covers the converters whose mapping is not a field
// copy, each with the input form that makes it non-obvious.
func TestMatrix_ConverterShapes(t *testing.T) {
	t.Run("an SQS queue url becomes an ARN", func(t *testing.T) {
		arn, name := sqsARNFromURL("https://sqs.us-east-1.amazonaws.com/123456789012/jobs", "us-east-1")
		if arn != "arn:aws:sqs:us-east-1:123456789012:jobs" || name != "jobs" {
			t.Errorf("got %q / %q; every other service names a queue by ARN, so the URL cannot be the node id",
				arn, name)
		}
		// A URL WITH TOO FEW SEGMENTS names no queue and must yield nothing rather
		// than an ARN composed around an empty field.
		if arn, _ := sqsARNFromURL("https://sqs.us-east-1.amazonaws.com/", "us-east-1"); arn != "" {
			t.Errorf("a malformed queue url produced the ARN %q", arn)
		}
	})

	t.Run("an ECR image reference becomes a repository ARN", func(t *testing.T) {
		for _, tc := range []struct{ image, want string }{
			{"123456789012.dkr.ecr.us-east-1.amazonaws.com/api:v3",
				"arn:aws:ecr:us-east-1:123456789012:repository/api"},
			{"123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api@sha256:abc",
				"arn:aws:ecr:us-east-1:123456789012:repository/team/api"},
			// ANOTHER ACCOUNT'S REGISTRY: the ARN names where the image LIVES, not
			// where the task runs, so it is composed from the reference.
			{"999999999999.dkr.ecr.eu-west-1.amazonaws.com/shared:latest",
				"arn:aws:ecr:eu-west-1:999999999999:repository/shared"},
			// NOT ECR: a public image names no repository in any account.
			{"docker.io/library/nginx:1.27", ""},
			{"nginx", ""},
			{"", ""},
		} {
			if got := ecrRepositoryARNForImage(tc.image, fixtureRegion, fixtureAccount); got != tc.want {
				t.Errorf("image %q mapped to %q, want %q", tc.image, got, tc.want)
			}
		}
	})

	t.Run("an API Gateway v2 protocol chooses the resource type", func(t *testing.T) {
		for _, tc := range []struct{ protocol, want string }{
			{"HTTP", ResourceTypeAPIGWHTTPAPI},
			{"WEBSOCKET", ResourceTypeAPIGWWSAPI},
			// A PROTOCOL THIS COLLECTOR DOES NOT KNOW yields no type rather than a
			// guess: a consumer's query selects on resource_type, and a future
			// protocol under the wrong one is worse than an omission the parity
			// census would catch.
			{"GRPC", ""},
			{"", ""},
		} {
			if got := resourceTypeForProtocol(tc.protocol); got != tc.want {
				t.Errorf("protocol %q mapped to %q, want %q", tc.protocol, got, tc.want)
			}
		}
	})

	t.Run("a CloudFront origin domain names a bucket only when it is one", func(t *testing.T) {
		for _, tc := range []struct {
			domain, want string
			ok           bool
		}{
			{"site-assets.s3.us-east-1.amazonaws.com", "site-assets", true},
			{"site-assets.s3.amazonaws.com", "site-assets", true},
			{"api.example.com", "", false},
			{"d111.cloudfront.net", "", false},
		} {
			got, ok := s3BucketFromOriginDomain(tc.domain)
			if got != tc.want || ok != tc.ok {
				t.Errorf("origin %q mapped to %q/%t, want %q/%t", tc.domain, got, ok, tc.want, tc.ok)
			}
		}
	})

	t.Run("a Route 53 zone id loses its path prefix", func(t *testing.T) {
		res := fixtureResult(t)
		zones := nodesOfType(res, ResourceTypeRoute53HostedZone)
		if len(zones) != 1 {
			t.Fatalf("want one hosted zone, got %d", len(zones))
		}
		if zones[0].ID != fixtureZoneID {
			t.Errorf("hosted zone id is %q, want %q: the API returns /hostedzone/<id> and the ARN wants the "+
				"bare id", zones[0].ID, fixtureZoneID)
		}
	})

	t.Run("a log group id drops the API's trailing wildcard", func(t *testing.T) {
		res := fixtureResult(t)
		groups := nodesOfType(res, ResourceTypeCloudWatchLogGroup)
		if len(groups) != 1 {
			t.Fatalf("want one log group, got %d", len(groups))
		}
		if strings.HasSuffix(groups[0].ID, ":*") {
			t.Errorf("the log group id %q keeps the API's trailing :*; every other service names a log group "+
				"without it, so every flow log's SINKS_TO edge would point at a node that is not there",
				groups[0].ID)
		}
	})

	t.Run("an IRSA subject splits into a namespace and a name", func(t *testing.T) {
		for _, tc := range []struct {
			subject          string
			ns, name         string
			wildcard, wanted bool
		}{
			{"system:serviceaccount:payments:api", "payments", "api", false, true},
			{"system:serviceaccount:batch:*", "batch", "*", true, true},
			// NOT AN IRSA SUBJECT: a trust condition on some other claim is a
			// different mechanism, and reading it as a ServiceAccount binding would
			// invent one.
			{"sts.amazonaws.com", "", "", false, false},
			{"system:serviceaccount:", "", "", false, false},
		} {
			ns, name, wildcard := parseIRSASubject(tc.subject)
			gotAny := ns != ""
			if gotAny != tc.wanted || (tc.wanted && (ns != tc.ns || name != tc.name || wildcard != tc.wildcard)) {
				t.Errorf("subject %q parsed to %q/%q wildcard=%t, want %q/%q wildcard=%t",
					tc.subject, ns, name, wildcard, tc.ns, tc.name, tc.wildcard)
			}
		}
	})
}
