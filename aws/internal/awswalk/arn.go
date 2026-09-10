// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"fmt"
	"strings"
)

// arn.go — NODE IDENTITY.
//
// THE ID IS THE REAL ARN, UNMODIFIED, wherever the API returns one. That is not
// a convenience: an ARN is globally unique and stable across collects, and the
// carry-forward diff keys on the node ID, so a second collect of an unchanged
// account writes zero rows only if every id is byte-identical the second time.
// A composed id, a generated one, or one carrying a timestamp would pass a
// set-equality determinism check and still make carry-forward write every node
// on every collect.
//
// FOUR SHAPES ARE COMPOSED HERE and nowhere else, because the API returns no ARN
// for them. Three are NODE ids and one is an edge ENDPOINT that names a resource
// in another account which a one-account walk never materializes.

// ec2ARN builds the ARN for an EC2-family resource, which the EC2 API returns as
// a bare id (vpc-…, subnet-…, sg-…) rather than as an ARN.
func ec2ARN(region, account, resourceType, id string) string {
	return fmt.Sprintf("arn:aws:ec2:%s:%s:%s/%s", region, account, resourceType, id)
}

// cidrSentinelID is the id of the sentinel node standing for a CIDR block a
// security-group or network-ACL rule references.
//
// IT EXISTS SO AN ALLOWS EDGE HAS TWO ENDPOINTS. A rule permitting 10.0.0.0/8
// names no AWS resource, so without a sentinel the edge would dangle into
// nothing and the graph could not answer "what can reach this security group".
func cidrSentinelID(cidr string) string { return "aws:cidr:" + cidr }

// dynamoPITRID is the id of the point-in-time-recovery node for a table. PITR is
// a table SETTING rather than an API object, so it has no ARN of its own, and it
// is a node rather than a metadata key because the built-in collector's graphs
// carry it as one and a consumer's query selects on that resource type.
func dynamoPITRID(tableName string) string { return "aws:dynamodb:pitr/" + tableName }

// peerSecurityGroupARN is an EDGE ENDPOINT and never a node id: it names a
// security group in a PEER account, which a walk of one account cannot see and
// therefore never emits. The edge is emitted anyway and its endpoint dangles,
// which is the contract's own rule — endpoint resolution belongs to the write
// path, and a rule referencing another account's group is a real relationship
// whether or not this collect can see the far side.
func peerSecurityGroupARN(region, peerAccount, groupID string) string {
	return ec2ARN(region, peerAccount, "security-group", groupID)
}

// irsaServiceAccountID is the Kubernetes-side endpoint of a WORKLOAD_IDENTITY
// edge: the id a k8s collect gives the ServiceAccount that assumes an IAM role
// through IRSA. It is composed to the k8s collector's own id shape rather than
// to anything AWS-shaped, because the node it names is resolved when THAT graph
// is collected on its own — there is no cross-collector cascade.
func irsaServiceAccountID(namespace, name string) string {
	return namespace + "/ServiceAccount/" + name
}

// irsaWildcardID is the endpoint for an IRSA trust statement whose subject is a
// wildcard, which names no single ServiceAccount. It is kept distinct from the
// concrete form so a consumer can tell "this role trusts one ServiceAccount"
// from "this role trusts any ServiceAccount in this cluster".
func irsaWildcardID(clusterARN, subject string) string {
	return "aws:eks:irsa-wildcard/" + clusterARN + "/" + subject
}

// arnAccount extracts the account id from an ARN, or returns empty when the
// string is not an ARN with one. The fifth colon-separated field is the account.
//
// IT IS USED TO DECIDE WHETHER A PRINCIPAL IS FOREIGN, so an empty return is
// treated as "cannot tell" by every caller rather than as "local".
func arnAccount(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[0] != "arn" {
		return ""
	}
	return parts[4]
}

// arnRegion extracts the region from an ARN on the same terms as arnAccount.
func arnRegion(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[0] != "arn" {
		return ""
	}
	return parts[3]
}
