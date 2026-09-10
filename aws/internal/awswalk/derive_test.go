// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"testing"
)

// derive_test.go — THE SIX DERIVED EDGE RELATIONSHIPS, each with its NEGATIVE
// CELLS.
//
// These are the relationships no single service walk can produce, and each is
// guarded: an inactive peering, a deny rule, a service principal, a subject the
// cluster does not serve. EVERY GUARD IS A WAY TO EMIT AN EDGE THAT SHOULD NOT
// EXIST, and a floor-only census cannot see a superset — so each relationship
// gets a positive cell AND the negatives that would make it wrong.

func TestDerive_AllowsEdgesReachCIDRSentinelsAndPeerGroups(t *testing.T) {
	res := fixtureResult(t)
	sg := ec2ARN(fixtureRegion, fixtureAccount, "security-group", fixtureSG)

	// The ingress rule's CIDR gets a sentinel node AND an edge to it, which is the
	// whole reason the sentinel exists: without it the edge would name a range
	// that is not a node and the graph could not answer what reaches this group.
	if !hasEdge(res, sg, cidrSentinelID("10.0.0.0/8"), EdgeAllowsIngressFrom) {
		t.Error("no ALLOWS_INGRESS_FROM edge from the security group to its rule's CIDR sentinel")
	}
	if len(nodesOfType(res, ResourceTypeCIDRBlock)) == 0 {
		t.Fatal("no cidr-block sentinel node was emitted, so every ALLOWS edge would dangle")
	}
	if !hasEdge(res, sg, cidrSentinelID("0.0.0.0/0"), EdgeAllowsEgressTo) {
		t.Error("no ALLOWS_EGRESS_TO edge for the egress rule")
	}

	// THE CROSS-ACCOUNT ARM, which the same rule set carries: a peer group in
	// another account. The id is composed in THAT account, so it dangles
	// deliberately; composing it in this one would name a group that does not
	// exist here and could collide with one that does.
	peer := peerSecurityGroupARN(fixtureRegion, fixturePeerAccount, "sg-0peer")
	if !hasEdge(res, sg, peer, EdgeAllowsIngressFrom) {
		t.Error("no ALLOWS_INGRESS_FROM edge to the peer-account security group named by the rule")
	}
	if arnAccount(peer) == fixtureAccount {
		t.Error("the peer group's id was composed in this account rather than the peer's")
	}
}

func TestDerive_NetworkACLDenyEntriesProduceNoAllowsEdge(t *testing.T) {
	res := fixtureResult(t)
	acl := ec2ARN(fixtureRegion, fixtureAccount, "network-acl", fixtureNACL)

	// The two ALLOW entries produce edges.
	if !hasEdge(res, acl, cidrSentinelID("10.0.0.0/8"), EdgeAllowsIngressFrom) {
		t.Error("the network ACL's allow entry produced no ALLOWS_INGRESS_FROM edge")
	}
	if !hasEdge(res, acl, cidrSentinelID("0.0.0.0/0"), EdgeAllowsEgressTo) {
		t.Error("the network ACL's egress allow entry produced no ALLOWS_EGRESS_TO edge")
	}
	// THE NEGATIVE CELL. An ALLOWS edge for a DENY rule inverts the meaning of the
	// graph's most security-relevant relationship, and a census that only counts
	// edge types would never see it.
	if hasEdge(res, acl, cidrSentinelID("192.0.2.0/24"), EdgeAllowsIngressFrom) {
		t.Error("the network ACL's DENY entry produced an ALLOWS_INGRESS_FROM edge; the graph now asserts " +
			"reachability that the rule forbids")
	}
	// And the denied range is not even a node: a sentinel for it would suggest
	// something references it permissively.
	if nodeIDs(res)[cidrSentinelID("192.0.2.0/24")] {
		t.Error("a CIDR sentinel was emitted for a range only a DENY rule references")
	}
}

func TestDerive_PeeringEmitsBothDirectionsAndTheCIDRRoutes(t *testing.T) {
	res := fixtureResult(t)
	local := ec2ARN(fixtureRegion, fixtureAccount, "vpc", fixtureVPC)
	peer := ec2ARN("us-west-2", fixturePeerAccount, "vpc", fixturePeerVPC)

	if !hasEdge(res, local, peer, EdgePeeredWith) || !hasEdge(res, peer, local, EdgePeeredWith) {
		t.Error("PEERED_WITH must be emitted in both directions; the graph carries directed edges and a " +
			"traversal starting at either VPC must find the other")
	}
	// THE FAR SIDE'S ARN IS BUILT IN ITS OWN ACCOUNT AND REGION. Composing it from
	// this walk's would name a VPC that does not exist rather than the one that
	// does.
	if arnAccount(peer) == fixtureAccount || arnRegion(peer) == fixtureRegion {
		t.Error("the peer VPC's id was composed in this walk's account or region rather than the peering's")
	}
	// ROUTES_TO_PEER lands on the CIDR SENTINEL, which is this collector's
	// deliberate divergence from the built-in shape: the built-in writes a bare
	// CIDR string, which resolves against nothing.
	if !hasEdge(res, local, cidrSentinelID("10.9.0.0/16"), EdgeRoutesToPeer) {
		t.Error("no ROUTES_TO_PEER edge from the local VPC to the peer's CIDR sentinel")
	}
}

func TestDerive_TrustsIsEmittedForForeignPrincipalsOnly(t *testing.T) {
	res := fixtureResult(t)

	// THE POSITIVE: the cross-account principal in the app role's trust policy.
	if !hasEdge(res, "arn:aws:iam::"+fixturePeerAccount+":root", fixtureRoleARN, EdgeTrusts) {
		t.Error("no TRUSTS edge from the cross-account principal to the role that admits it")
	}
	// THE NEGATIVE: a SERVICE principal is not an ARN and carries no account.
	// Treating it as a foreign trust would report every Lambda execution role in
	// every account as trusting an outside party.
	for _, e := range edgesOfType(res, EdgeTrusts) {
		if e.FromID == "lambda.amazonaws.com" {
			t.Error("a service principal produced a TRUSTS edge; it names no account and crosses no boundary")
		}
		if arnAccount(e.FromID) == fixtureAccount {
			t.Errorf("a SAME-ACCOUNT principal produced a TRUSTS edge (%s); the relationship is about the "+
				"account boundary being crossed", e.FromID)
		}
	}
}

func TestDerive_WorkloadIdentityCarriesBothSubjectForms(t *testing.T) {
	res := fixtureResult(t)

	// THE CONCRETE SUBJECT resolves to the k8s collector's own id shape, which is
	// what makes the edge resolvable when that cluster is collected on its own.
	concrete := irsaServiceAccountID("payments", "api")
	if !hasEdge(res, concrete, fixtureIRSARole, EdgeWorkloadIdentity) {
		t.Errorf("no WORKLOAD_IDENTITY edge from %s to the IRSA role", concrete)
	}
	// THE WILDCARD SUBJECT gets its OWN form, because it names no single
	// Kubernetes object and collapsing it onto a concrete id would assert a
	// binding that does not exist.
	wildcard := irsaWildcardID(fixtureClusterARN, "system:serviceaccount:batch:*")
	if !hasEdge(res, wildcard, fixtureIRSARole, EdgeWorkloadIdentity) {
		t.Errorf("no WORKLOAD_IDENTITY edge for the wildcard subject; got edges: %v",
			edgesOfType(res, EdgeWorkloadIdentity))
	}
	// USES_SA answers the other direction's question, from the cluster.
	if !hasEdge(res, fixtureClusterARN, concrete, EdgeUsesSA) {
		t.Error("no USES_SA edge from the cluster to the bound ServiceAccount")
	}
}

// TestDerive_WorkloadIdentityNeedsTheClusterThatServesTheIssuer is the negative
// cell: a role bound to a cluster in ANOTHER account names an issuer no cluster
// here serves, and inventing a node for it would assert a cluster this account
// does not have.
func TestDerive_WorkloadIdentityNeedsTheClusterThatServesTheIssuer(t *testing.T) {
	clients := fixtureClients()
	// Remove the EKS cluster, leaving the IRSA role's trust policy in place.
	clients.EKS = &fakeEks{}
	res := runWalk(t, clients, Params{})
	if got := len(edgesOfType(res, EdgeWorkloadIdentity)); got != 0 {
		t.Errorf("%d WORKLOAD_IDENTITY edge(s) emitted with no cluster serving the issuer", got)
	}
	// SAME-RUN KNOWN POSITIVE: the roles are still there, so the zero is the
	// issuer match failing rather than the IAM walk finding nothing.
	if len(nodesOfType(res, ResourceTypeIAMRole)) == 0 {
		t.Fatal("the IAM roles must still be emitted, or the zero above proves nothing")
	}
}

func TestDerive_CertificateValidationMatchesTheLongestZoneSuffix(t *testing.T) {
	res := fixtureResult(t)
	if !hasEdge(res, fixtureCertARN, fixtureZoneID, EdgeValidatedBy) {
		t.Error("no VALIDATED_BY edge from the DNS-validated certificate to the hosted zone that proves it")
	}
	// THE NEGATIVE CELL: the same certificate carries an EMAIL-validated domain,
	// which a hosted zone does not prove. One edge, not two.
	if got := len(edgesOfType(res, EdgeValidatedBy)); got != 1 {
		t.Errorf("%d VALIDATED_BY edges emitted; the certificate has one DNS-validated domain and one "+
			"email-validated one, which no zone proves", got)
	}
}

// TestDerive_OneCIDRReferencedTwiceIsOneNode is the sink's node-dedupe arm.
//
// TWO INDEPENDENT WALKS NAME ONE RANGE in the fixture — a security-group rule and
// a network-ACL entry both reference 10.0.0.0/8 — which is the ordinary case in a
// real account and the one the dedupe exists for. Without it the result carries
// two entries under one id, which is a malformed document rather than a duplicate:
// the carry-forward diff keys on the node id, and the write path has two nodes
// claiming to be the same resource.
func TestDerive_OneCIDRReferencedTwiceIsOneNode(t *testing.T) {
	res := fixtureResult(t)
	shared := cidrSentinelID("10.0.0.0/8")

	count := 0
	for _, n := range res.Nodes {
		if n.ID == shared {
			count++
		}
	}
	if count != 1 {
		t.Errorf("the result carries %d nodes with id %q; two walks referenced that range and the result must "+
			"carry one node for it", count, shared)
	}
	// THE KNOWN POSITIVE, in the same run: BOTH walks did produce their edge to it,
	// so the single node is the dedupe working rather than one walk not running.
	sg := ec2ARN(fixtureRegion, fixtureAccount, "security-group", fixtureSG)
	acl := ec2ARN(fixtureRegion, fixtureAccount, "network-acl", fixtureNACL)
	if !hasEdge(res, sg, shared, EdgeAllowsIngressFrom) {
		t.Error("the security group's edge to the shared range is missing, so the count above proves nothing")
	}
	if !hasEdge(res, acl, shared, EdgeAllowsIngressFrom) {
		t.Error("the network ACL's edge to the shared range is missing, so the count above proves nothing")
	}
}
