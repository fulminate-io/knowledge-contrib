// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// parity_test.go — THE PARITY CENSUS, which is this collector's coverage gate.
//
// The ticket's coverage target is the vocabulary the built-in AWS collector
// produces: one node type, fifty-four resource_type values, thirty-three edge
// relationships, real ARNs with exactly three synthetic node id forms. These
// tests walk the fully-populated fake account and assert every one of them
// appears, so a service walk that stops emitting a type turns them red BY NAME.
//
// THE COUNTS ARE FLOORS, not equalities: a collector that grows a type is not a
// regression, and asserting equality would make every addition a failure to
// negotiate. What is asserted for equality is the SYNTHETIC ID FORMS, because
// those are a promise about node identity that a consumer's saved query depends
// on and a new one is a change rather than a growth.

func TestParity_EveryResourceTypeIsEmitted(t *testing.T) {
	res := runWalk(t, fixtureClients(), Params{})
	seen := resourceTypesIn(res)

	var missing []string
	for _, want := range AllResourceTypes {
		if !seen[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("the walk over the fully-populated fake account emitted no node for %d of the %d resource types "+
			"the parity floor names: %v.\nA service walk that stopped emitting one, or a fixture that stopped "+
			"carrying one, both land here — open the named walk and its fixture.",
			len(missing), len(AllResourceTypes), missing)
	}

	// THE KNOWN POSITIVE, and it is not decoration: every assertion above is
	// satisfied by a walk that emitted nothing at all, because a missing type
	// would then be reported for all fifty-four and a reader could mistake one
	// broken fixture for a broken collector. This says the walk ran.
	if len(res.Nodes) < len(AllResourceTypes) {
		t.Fatalf("the walk emitted %d nodes, fewer than the %d resource types it must cover; "+
			"the fixture or the fan-out is broken rather than one service walk",
			len(res.Nodes), len(AllResourceTypes))
	}
}

func TestParity_EveryEdgeTypeIsEmitted(t *testing.T) {
	res := runWalk(t, fixtureClients(), Params{})
	seen := edgeTypesIn(res)

	var missing []string
	for _, want := range AllEdgeTypes {
		if !seen[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("the walk over the fully-populated fake account emitted no edge of %d of the %d relationship "+
			"types the parity floor names: %v", len(missing), len(AllEdgeTypes), missing)
	}
	if len(res.Edges) < len(AllEdgeTypes) {
		t.Fatalf("the walk emitted %d edges, fewer than the %d relationship types it must cover",
			len(res.Edges), len(AllEdgeTypes))
	}
}

func TestParity_OneNodeTypeOnly(t *testing.T) {
	res := runWalk(t, fixtureClients(), Params{})
	for _, n := range res.Nodes {
		if n.Type != NodeTypeCloudResource {
			t.Fatalf("node %s carries type %q; every node this collector emits is a %q and the resource kind "+
				"rides in the resource_type metadata key", n.ID, n.Type, NodeTypeCloudResource)
		}
		if n.Metadata[MetaResourceType] == "" {
			t.Fatalf("node %s carries no resource_type metadata, so no consumer can tell what it is", n.ID)
		}
		if n.Metadata[MetaAccount] != fixtureAccount {
			t.Fatalf("node %s carries account %q, want %q", n.ID, n.Metadata[MetaAccount], fixtureAccount)
		}
	}
}

// TestParity_SyntheticNodeIDForms asserts that exactly two node id forms are
// composed rather than returned by an API.
//
// IT IS AN EQUALITY, not a floor. A node id is the carry-forward diff key and the
// endpoint every edge names, so a THIRD composed form is a change to node
// identity that a consumer's saved query and the diff both see — it belongs in a
// deliberate diff on this list, not in a service walk nobody reviewed for it.
//
// IT WAS THREE. The third was an ARN-shaped id composed for an SES receipt rule,
// and it went with the walk that fabricated those nodes out of email templates:
// a composed id form is a promise to consumers about node identity, so pinning
// one for a resource this collector does not emit was pinning a promise it could
// not keep.
func TestParity_SyntheticNodeIDForms(t *testing.T) {
	res := runWalk(t, fixtureClients(), Params{})

	// The two forms, each identified by the prefix that makes it recognizable.
	wantForms := map[string]string{
		"aws:cidr:":          "the CIDR sentinel a security-group or network-ACL rule points at",
		"aws:dynamodb:pitr/": "point-in-time recovery, which is a table setting with no API object",
	}
	found := map[string]bool{}
	var unknown []string
	for id := range nodeIDs(res) {
		if strings.HasPrefix(id, "arn:aws:") {
			continue // a real ARN
		}
		matched := false
		for prefix := range wantForms {
			if strings.HasPrefix(id, prefix) || strings.Contains(id, prefix) {
				found[prefix] = true
				matched = true
				break
			}
		}
		if !matched {
			unknown = append(unknown, id)
		}
	}
	for prefix, why := range wantForms {
		if !found[prefix] {
			t.Errorf("no node carries the synthetic id form %q (%s); the walk that composes it stopped", prefix, why)
		}
	}
	if len(unknown) > 0 {
		t.Errorf("node ids that are neither a real ARN nor one of the two composed forms: %v.\n"+
			"A node id is the carry-forward diff key and the endpoint every edge names, so a new composed form "+
			"is a deliberate change to node identity rather than a detail of one service walk.", unknown)
	}
}

// TestParity_EveryEdgeEndpointIsAccountedFor is the CLOSED-ENUMERATION assertion.
//
// Most edges name a node in the same result. Five classes legitimately do not,
// and each is a real relationship the graph would lose by dropping it:
//
//  1. a CIDR sentinel, which the rules pass emits — so this one DOES resolve, and
//     it is listed here because ROUTES_TO_PEER's far end is one;
//  2. a cross-account IAM principal, from a role's trust policy;
//  3. a Kubernetes ServiceAccount id, from an IRSA binding, which resolves when
//     that cluster is collected on its own;
//  4. a PEER-ACCOUNT security group, from a rule referencing another account;
//  5. an IAM INSTANCE PROFILE, whose ARN names the profile rather than the role
//     it carries; resolving it would take a call this walk does not make.
//
// THE ENUMERATION IS CLOSED: an endpoint matching none of the five and absent
// from the result FAILS. That is what keeps this from being a rubber stamp — a
// genuine typo in a composed id, or a sentinel a walk forgot to emit, lands here.
//
// AND THE CLASSES ARE NARROW ON PURPOSE. A class written as a NAMESPACE PREFIX
// admits every id in that namespace, including the ones this walk emits itself,
// so a typo in a composed id inside it can never land here. Two such classes were
// removed — one for RDS read replicas and one for ElastiCache member clusters —
// because the endpoints they covered ARE emitted by this walk and the correct
// assertion for them is that they RESOLVE. Their fixtures now carry the resources
// those edges point at, which is what an admitted namespace was hiding.
func TestParity_EveryEdgeEndpointIsAccountedFor(t *testing.T) {
	res := runWalk(t, fixtureClients(), Params{})
	ids := nodeIDs(res)

	for _, e := range res.Edges {
		for _, endpoint := range []string{e.FromID, e.ToID} {
			if ids[endpoint] {
				continue
			}
			if externalEndpointClass(endpoint) != "" {
				continue
			}
			t.Errorf("edge %s -[%s]-> %s names endpoint %q, which is neither a node in this result nor one of "+
				"the five legitimately-external endpoint classes", e.FromID, e.Type, e.ToID, endpoint)
		}
	}
}

// externalEndpointClass names which legitimately-external class an endpoint falls
// into, or empty when it falls into none.
func externalEndpointClass(id string) string {
	switch {
	case strings.HasPrefix(id, "aws:cidr:"):
		return "cidr-sentinel"
	case strings.HasPrefix(id, "aws:eks:irsa-wildcard/"):
		return "irsa-wildcard-subject"
	case strings.Contains(id, "/ServiceAccount/"):
		return "kubernetes-service-account"
	case strings.HasPrefix(id, "arn:aws:iam::") && arnAccount(id) != fixtureAccount:
		return "cross-account-iam-principal"
	case strings.HasPrefix(id, "arn:aws:ec2:") && arnAccount(id) != fixtureAccount:
		return "peer-account-ec2-resource"
	case isInstanceProfileARN(id):
		return "instance-profile"
	default:
		return ""
	}
}

// isInstanceProfileARN matches the EXACT instance-profile ARN shape in the walked
// account, and nothing else in the iam namespace.
//
// THE SHAPE IS CHECKED STRUCTURALLY, not by substring, and that is the whole
// point of the function existing. `strings.Contains(id, ":instance-profile/")`
// admits any iam ARN carrying that text anywhere, which makes a typo in one
// unresolvable rather than a failure — the class then certifies the very ids it
// was supposed to be a narrow exception for. Six colon-separated fields, the
// account matching the one this walk enumerated, and a non-empty profile name.
func isInstanceProfileARN(id string) bool {
	const resourcePrefix = "instance-profile/"
	parts := strings.Split(id, ":")
	if len(parts) != 6 {
		return false
	}
	if parts[0] != "arn" || parts[1] != "aws" || parts[2] != "iam" || parts[3] != "" {
		return false
	}
	if parts[4] != fixtureAccount {
		return false
	}
	return strings.HasPrefix(parts[5], resourcePrefix) && len(parts[5]) > len(resourcePrefix)
}

// TestParity_KnownPositiveForTheEndpointEnumeration is the control the assertion
// above needs.
//
// WITHOUT IT the closed enumeration is satisfiable by a matcher that returns a
// class for everything. This drives invented ids through the same classifier and
// requires every one to be REFUSED.
//
// THE IDS ARE CHOSEN PER CLASS, not for variety. The earlier version of this
// control drove three ids — a non-ARN, an s3 bucket and a lambda function — none
// of which touches the namespaces the widest classes admitted, so the control
// passed while the classes that needed exercising went untested. Every entry
// below is a NEAR MISS of exactly one surviving class: same namespace, same
// account, wrong in the one way that matters.
func TestParity_KnownPositiveForTheEndpointEnumeration(t *testing.T) {
	for _, tc := range []struct {
		id   string
		near string
	}{
		{"not-an-arn-at-all", "nothing at all"},
		{"arn:aws:s3:::a-bucket-this-walk-never-emitted", "nothing at all"},
		{"arn:aws:lambda:us-east-1:123456789012:function:typo", "nothing at all"},

		// cidr-sentinel: the sentinel prefix is aws:cidr:, not aws:cidrs:.
		{"aws:cidrs:10.0.0.0/8", "cidr-sentinel"},
		// irsa-wildcard-subject: same idea one character out.
		{"aws:eks:irsa-wildcards/cluster", "irsa-wildcard-subject"},
		// kubernetes-service-account: the marker is /ServiceAccount/, capital S.
		{"cluster/namespace/serviceaccount/name", "kubernetes-service-account"},
		// cross-account-iam-principal: an iam ARN in THIS account is not external.
		{"arn:aws:iam::" + fixtureAccount + ":role/a-role-this-walk-never-emitted", "cross-account-iam-principal"},
		// peer-account-ec2-resource: an ec2 ARN in THIS account is not external.
		{"arn:aws:ec2:us-east-1:" + fixtureAccount + ":security-group/sg-typo", "peer-account-ec2-resource"},
		// instance-profile: right namespace and account, empty profile name.
		{"arn:aws:iam::" + fixtureAccount + ":instance-profile/", "instance-profile"},
		// instance-profile: right text, wrong resource — a ROLE, not a profile.
		{"arn:aws:iam::" + fixtureAccount + ":role/instance-profile/x", "instance-profile"},

		// THE TWO CLASSES THAT WERE REMOVED. Both namespaces are enumerated by
		// this walk, so an unresolvable id in either is a defect rather than an
		// external endpoint. These two entries are what keep the classes from
		// being reinstated without this control going red first.
		{"arn:aws:rds:us-east-1:" + fixtureAccount + ":db:a-replica-this-walk-never-emitted", "rds-read-replica"},
		{"arn:aws:elasticache:us-east-1:" + fixtureAccount + ":cluster:never-emitted", "elasticache-member-cluster"},
	} {
		if class := externalEndpointClass(tc.id); class != "" {
			t.Errorf("the endpoint classifier admitted %q as %q. It is a near miss of the %s class and must be "+
				"REFUSED: an id that is neither a node in the result nor a genuine external endpoint has to "+
				"land as a failure, or the closed enumeration proves nothing.", tc.id, class, tc.near)
		}
	}
}

// TestParity_EveryEdgeCarriesTheCollectMethod asserts the provenance stamp.
func TestParity_EveryEdgeCarriesTheCollectMethod(t *testing.T) {
	res := runWalk(t, fixtureClients(), Params{})
	for _, e := range res.Edges {
		if e.Method != collectMethod {
			t.Fatalf("edge %s -[%s]-> %s carries method %q, want %q: without it a consumer cannot tell an edge "+
				"this collector derived from one a person wrote", e.FromID, e.Type, e.ToID, e.Method, collectMethod)
		}
	}
}

// TestParity_WalkIsCompleteOverAHealthyAccount is the positive arm of the
// completeness assertion; walk_test.go carries the partial arm.
func TestParity_WalkIsCompleteOverAHealthyAccount(t *testing.T) {
	res := runWalk(t, fixtureClients(), Params{})
	if !res.Complete.IsAsserted() {
		t.Fatal("the walk returned an unasserted Completeness, which the framework refuses")
	}
	if !res.Complete.IsComplete() {
		t.Fatalf("a walk over an account where every service answered must assert COMPLETE; it asserted "+
			"incomplete because: %s", res.Complete.Reason())
	}
}

// fixtureResult is a convenience for tests that need the fixture walk's result
// without re-running it in every subtest.
func fixtureResult(t *testing.T) framework.Result {
	t.Helper()
	return runWalk(t, fixtureClients(), Params{})
}
