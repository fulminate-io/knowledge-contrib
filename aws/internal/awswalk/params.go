// SPDX-License-Identifier: Apache-2.0

package awswalk

import "fmt"

// params.go — THE COLLECT PARAMS, and why every field is optional.
//
// The framework infers this type's JSON Schema and splices it into the contract
// input schema as the `params` property. The client OMITS `params` from the call
// arguments entirely when a collect supplies none, and it validates the arguments
// against the advertised schema BEFORE sending them — so a required key here
// would refuse every paramless collect before it left the client. Optional with a
// documented default is the only shape that admits both callers.

// Params are the selectors a collect may carry.
type Params struct {
	// Region is the AWS region to walk. Empty means the region the credential
	// chain resolves — AWS_REGION, AWS_DEFAULT_REGION, or the active profile's
	// setting — which is the same default every other AWS tool on the host uses.
	//
	// IT IS HONORED RATHER THAN RECORDED. A collect naming a region walks that
	// region's API endpoints, and every node it emits carries it as metadata; a
	// collector that accepted the selector and walked the default anyway would
	// produce a graph that looks right and describes another region.
	Region string `json:"region,omitempty"`

	// Account is the AWS account id the collect expects to walk. Empty means
	// whichever account the credential resolves to.
	//
	// IT IS A GUARD, NOT A SWITCH: this collector cannot assume a role or change
	// credentials, so naming an account it is not authenticated as is REFUSED
	// with both ids rather than silently walking the other one. A collect that
	// landed another account's resources in a graph named for this one would be
	// wrong in a way no consumer could see.
	Account string `json:"account,omitempty"`
}

// validate refuses a params value this collector cannot honor.
//
// It is called BEFORE any API call, so a refusal costs no round trip, and it
// names the field: bad input errors, and an error a caller cannot act on is
// barely better than a silent default.
func (p Params) validate() error {
	if p.Region != "" && !looksLikeRegion(p.Region) {
		return fmt.Errorf("params.region %q is not an AWS region name (expected a form like us-east-1)", p.Region)
	}
	if p.Account != "" && !looksLikeAccountID(p.Account) {
		return fmt.Errorf("params.account %q is not an AWS account id (expected 12 digits)", p.Account)
	}
	return nil
}

// looksLikeRegion is a SHAPE check and deliberately not a list of regions. AWS
// adds regions continually, and a collector carrying a fixed list would refuse a
// new one — an availability failure caused by this code rather than by the input.
// What it rejects is the class the shape check can decide: an empty segment, an
// obviously wrong separator, a value that is plainly not a region name.
func looksLikeRegion(s string) bool {
	if len(s) < 5 || len(s) > 40 {
		return false
	}
	dashes := 0
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
			dashes++
		default:
			return false
		}
	}
	return dashes >= 2 && s[0] != '-' && s[len(s)-1] != '-'
}

// looksLikeAccountID reports whether s is the twelve digits an AWS account id is.
func looksLikeAccountID(s string) bool {
	if len(s) != 12 {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
