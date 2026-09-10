// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"encoding/json"
	"net/url"
	"strings"
)

// derive_trust.go — TRUSTS and WORKLOAD_IDENTITY, both read out of an IAM role's
// trust policy.
//
// THE TRUST POLICY IS THE ROLE'S ANSWER TO "WHO MAY BECOME ME", which makes it
// the single most useful document in an account for a reachability question, and
// the IAM API returns it inline on ListRoles — so neither of these edges costs an
// extra call.
//
// IT ARRIVES URL-ENCODED. The AWS API returns the policy document as a
// percent-encoded JSON string, so it is decoded before it is parsed; a parser fed
// the raw form finds no statements and reports an empty policy, which looks
// exactly like a role nobody may assume.
//
// A KNOWN PARITY BOUND on TRUSTS, recorded rather than discovered. The built-in
// collector emits a TRUSTS edge only for a foreign principal it can FIND in
// another account's already-collected cloud graph, and it also writes a mirrored
// edge INTO that peer account's graph. Neither is expressible here: the first is
// a cross-graph READ and the second a cross-graph WRITE, and one collect returns
// one envelope for one graph instance. So this collector carries the LOCAL edge —
// from the foreign principal ARN to the local role — for every cross-account
// principal a trust policy names, which makes its TRUSTS set a SUPERSET of the
// built-in collector's, and it attempts no mirrored edge at all.

// policyDocument is the part of an IAM policy this collector reads. Everything
// else in the document is deliberately not modeled: a partial decode of a
// well-known shape is more robust than a full one, because an unmodeled field
// added by AWS cannot break it.
type policyDocument struct {
	Statement []policyStatement `json:"Statement"`
}

type policyStatement struct {
	Effect    string          `json:"Effect"`
	Principal json.RawMessage `json:"Principal"`
	Condition json.RawMessage `json:"Condition"`
	Action    json.RawMessage `json:"Action"`
}

// decodePolicy decodes a URL-encoded IAM policy document.
//
// IT RETURNS A ZERO DOCUMENT AND NO ERROR on an unparseable body, and that is
// deliberate rather than a swallowed failure: a trust policy this collector
// cannot read yields no TRUSTS edges for that ROLE, which is a gap in one role's
// relationships. Failing the whole service walk instead would turn one
// unrecognized document into an incomplete account walk and disable the server's
// deletion phase for every resource in it.
func decodePolicy(encoded string) policyDocument {
	if encoded == "" {
		return policyDocument{}
	}
	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		// Not URL-encoded: try it as-is, because the field is documented as
		// encoded but a fixture or a future API version may not be.
		decoded = encoded
	}
	var doc policyDocument
	if err := json.Unmarshal([]byte(decoded), &doc); err != nil {
		return policyDocument{}
	}
	return doc
}

// principalARNs extracts every ARN a statement's Principal names.
//
// THE FIELD IS THREE SHAPES IN ONE JSON KEY: a bare string, an object mapping a
// principal TYPE to a string, and the same object mapping a type to a LIST. A
// decoder handling only the object form silently reads nothing from the string
// form, which is the shape a service principal usually takes.
func principalARNs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return []string{asString}
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asMap); err != nil {
		return nil
	}
	var out []string
	// THE KEYS ARE READ IN A FIXED ORDER, because a map ranged in place would
	// order the emitted edges by Go's hash seed and the result would differ
	// between two collects of one unchanged account.
	for _, key := range []string{"AWS", "Service", "Federated", "CanonicalUser"} {
		v, ok := asMap[key]
		if !ok {
			continue
		}
		var one string
		if err := json.Unmarshal(v, &one); err == nil {
			out = append(out, one)
			continue
		}
		var many []string
		if err := json.Unmarshal(v, &many); err == nil {
			out = append(out, many...)
		}
	}
	return out
}

// deriveTrust emits one TRUSTS edge per FOREIGN principal a role's trust policy
// admits.
//
// FOREIGN IS THE POINT. A statement naming a principal in THIS account describes
// an internal delegation the rest of the graph already shows; a statement naming
// another account's principal is the account boundary being crossed, which is
// what a security review is looking for. A service principal
// (lambda.amazonaws.com) is not an ARN and carries no account, so it is skipped
// here rather than emitted as a foreign trust.
func (w *walkContext) deriveTrust() {
	for _, r := range w.derived.roles {
		doc := decodePolicy(r.assumeRolePolicy)
		for _, st := range doc.Statement {
			if !strings.EqualFold(st.Effect, "Allow") {
				continue
			}
			for _, p := range principalARNs(st.Principal) {
				acct := arnAccount(p)
				if acct == "" || acct == w.account {
					continue
				}
				w.sink.addEdge(p, r.arn, EdgeTrusts, map[string]string{
					"principal_account": acct,
					"via":               "role.assume_role_policy",
				})
			}
		}
	}
}
