// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"sync"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// derive.go — THE SECOND PASS, and why there is one.
//
// SIX of the thirty-three edge relationships cannot be produced by any single
// service walk, because each needs two services' output at once or needs nodes
// no API returns:
//
//	ALLOWS_INGRESS_FROM / ALLOWS_EGRESS_TO  need the CIDR sentinel nodes that give
//	                                        a rule's far end an endpoint at all
//	PEERED_WITH / ROUTES_TO_PEER            need both sides of a peering
//	TRUSTS                                  needs a role's trust policy parsed
//	WORKLOAD_IDENTITY                       needs a role's trust policy AND the
//	                                        EKS clusters whose OIDC issuers it names
//
// THE BUILT-IN COLLECTOR PRODUCES THESE IN A POST-POPULATE HOOK the client runs
// after a collect. THIS COLLECTOR CANNOT USE ONE: the client's post-populate tail
// returns before the hook lookup for any registered custom family, so a hook
// would never fire. So the derivation happens HERE, inside the walk, which is
// also the only place with the raw rules — the service walks record what they
// saw and the passes below turn it into nodes and edges once every walk is done.
//
// THE RECORDING IS SEPARATE FROM THE DERIVATION for one reason: a rule
// referencing a peer VPC must be able to see the peering the peering walk found,
// and the two walks finish in whatever order the API answers.

// derived is the raw material the service walks record for the second pass. It
// is written from the fan-out, so every method takes the lock.
type derived struct {
	mu sync.Mutex
	// sgRules are the security-group rule sets, keyed by the group's node id.
	sgRules []sgRuleSet
	// naclEntries are the network-ACL rule sets, keyed by the ACL's node id.
	naclEntries []naclRuleSet
	// peerings are the VPC peering connections, in the API's own shape.
	peerings []ec2types.VpcPeeringConnection
	// roles are the IAM roles whose trust policy the trust and IRSA passes read.
	roles []roleRecord
	// eksClusters maps an EKS cluster's OIDC issuer host to its ARN, which is
	// what turns an IRSA trust statement into an edge naming a cluster.
	eksClusters []eksClusterRecord
	// certificates and hostedZones are the two halves of VALIDATED_BY, which two
	// concurrent service walks produce and only a later pass can join.
	certificates []certificateRecord
	hostedZones  []hostedZoneRecord
}

// sgRuleSet is one security group's rules as the API returned them, with the
// group's node id so the pass can emit from it.
type sgRuleSet struct {
	groupARN string
	ingress  []ec2types.IpPermission
	egress   []ec2types.IpPermission
}

// naclRuleSet is one network ACL's entries with its node id.
type naclRuleSet struct {
	aclARN  string
	entries []ec2types.NetworkAclEntry
}

// roleRecord is one IAM role's identity and its trust policy document, which is
// URL-encoded JSON as the IAM API returns it.
type roleRecord struct {
	arn string
	// assumeRolePolicy is the trust policy: who may assume this role. It is the
	// source of both TRUSTS and WORKLOAD_IDENTITY.
	assumeRolePolicy string
}

// eksClusterRecord is one EKS cluster's ARN beside the OIDC issuer host an IRSA
// trust statement names.
type eksClusterRecord struct {
	arn string
	// issuerHost is the issuer URL with its scheme stripped, because that is the
	// form a trust policy's condition key uses: `<host>:sub`.
	issuerHost string
}

func (w *walkContext) recordSecurityGroupRules(groupARN string, g ec2types.SecurityGroup) {
	w.derived.mu.Lock()
	defer w.derived.mu.Unlock()
	w.derived.sgRules = append(w.derived.sgRules, sgRuleSet{
		groupARN: groupARN,
		ingress:  g.IpPermissions,
		egress:   g.IpPermissionsEgress,
	})
}

func (w *walkContext) recordNetworkACLRules(aclARN string, a ec2types.NetworkAcl) {
	w.derived.mu.Lock()
	defer w.derived.mu.Unlock()
	w.derived.naclEntries = append(w.derived.naclEntries, naclRuleSet{aclARN: aclARN, entries: a.Entries})
}

func (w *walkContext) recordPeering(c ec2types.VpcPeeringConnection) {
	w.derived.mu.Lock()
	defer w.derived.mu.Unlock()
	w.derived.peerings = append(w.derived.peerings, c)
}

func (w *walkContext) recordRole(arn, assumeRolePolicy string) {
	if arn == "" {
		return
	}
	w.derived.mu.Lock()
	defer w.derived.mu.Unlock()
	w.derived.roles = append(w.derived.roles, roleRecord{arn: arn, assumeRolePolicy: assumeRolePolicy})
}

func (w *walkContext) recordEKSCluster(arn, issuerHost string) {
	if arn == "" || issuerHost == "" {
		return
	}
	w.derived.mu.Lock()
	defer w.derived.mu.Unlock()
	w.derived.eksClusters = append(w.derived.eksClusters, eksClusterRecord{arn: arn, issuerHost: issuerHost})
}

// runDerivedPasses runs every second-pass derivation, in a fixed order.
//
// IT RUNS AFTER THE FAN-OUT HAS DRAINED, so every pass sees the whole account's
// recordings rather than whichever walks happened to finish first. Order is fixed
// rather than concurrent because the passes are cheap, purely local, and reading
// a fixed order is what makes the emitted set reproducible without relying on the
// sink's final sort alone.
//
// A PASS RUNS EVEN WHEN A SERVICE WALK FAILED. It derives from what WAS
// collected, which is the honest thing to do with a partial account; the result
// says the walk was partial through walk_complete, and suppressing the derivation
// as well would lose relationships among the resources that did come back.
func (w *walkContext) runDerivedPasses() {
	w.deriveSecurityGroupRules()
	w.deriveNetworkACLRules()
	w.derivePeering()
	w.deriveTrust()
	w.deriveWorkloadIdentity()
	w.deriveCertificateValidation()
}
