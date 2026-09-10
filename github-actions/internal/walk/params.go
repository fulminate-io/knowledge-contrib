// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"fmt"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
)

// params.go — the collect's own parameters and the collect id, validated once
// and refused loudly.

// Params is the collect tool's parameter object. The framework infers its schema,
// advertises it inside the contract's input schema, and validates a call's
// arguments against it before [Collector.Walk] is reached — so a value of the
// wrong KIND is refused by the schema and never arrives here, and what this file
// checks is the values a correctly typed call can still get wrong.
//
// BOTH ARE POINTERS, and that is the difference between "unset" and "zero". A
// caller that omits a cap wants this collector's default; a caller that sends 0
// has asked for no runs at all, which is not a thing to collect and is refused by
// name. A plain int cannot tell those apart and would silently turn the second
// into the first.
type Params struct {
	// MaxRuns bounds how many recent workflow runs are read PER REPOSITORY.
	MaxRuns *int `json:"max_runs,omitempty" jsonschema:"how many recent workflow runs to read per repository (default 10, maximum 100)"`
	// MaxDeployments bounds how many recent deployments are read PER REPOSITORY.
	MaxDeployments *int `json:"max_deployments,omitempty" jsonschema:"how many recent deployments to read per repository (default 20, maximum 100)"`
}

// CapCeiling is the largest value either cap may take. It is EXPORTED so this
// module's own documentation gate can compare the README's operator-facing
// parameter table against the number the walk applies, rather than against a
// second literal that is free to drift from it.
//
// IT IS THE PROVIDER'S OWN PAGE MAXIMUM, AND A LARGER REQUEST IS REFUSED RATHER
// THAN CLAMPED. These two enumerations read a single page, so a request for 500
// would be answered with 100 and reported as a successful collect of 500 — the
// caller would be told it got five times what it did. Refusing names the ceiling
// and leaves the caller to ask for something this collector can deliver.
const CapCeiling = 100

// caps validates the two parameters and resolves the defaults.
func (p Params) caps() (collect.Caps, error) {
	maxRuns, err := boundedCap("max_runs", p.MaxRuns, collect.DefaultMaxRuns)
	if err != nil {
		return collect.Caps{}, err
	}
	maxDeployments, err := boundedCap("max_deployments", p.MaxDeployments, collect.DefaultMaxDeployments)
	if err != nil {
		return collect.Caps{}, err
	}
	return collect.Caps{MaxRuns: maxRuns, MaxDeployments: maxDeployments}, nil
}

// boundedCap resolves one cap: unset takes the default, and a value outside
// 1..[CapCeiling] is refused naming the parameter, what was sent and the bound
// it broke.
func boundedCap(name string, value *int, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	switch {
	case *value < 1:
		return 0, fmt.Errorf(
			"%s is %d; it is how many items to read per repository, so it is at least 1. "+
				"Omit it to read this collector's default of %d", name, *value, fallback)
	case *value > CapCeiling:
		return 0, fmt.Errorf(
			"%s is %d, above the maximum of %d. This enumeration reads a single page and the "+
				"provider's page maximum is %d, so a larger value cannot be honored and is refused "+
				"rather than silently answered with %d",
			name, *value, CapCeiling, CapCeiling, CapCeiling)
	default:
		return *value, nil
	}
}

// The provider's own rules for an organization login: 1 to 39 characters,
// alphanumerics and hyphens, not starting or ending with a hyphen.
const organizationMaxLen = 39

// validOrganization trims and validates the collect id.
//
// IT IS VALIDATED HERE RATHER THAN PASSED THROUGH because this id is
// interpolated into every request path the walk builds AND into every node id it
// emits. A malformed one passed through reaches the caller as seven per-
// enumeration errors, or worse as a walk that matched nothing and looked like an
// empty organization. Bad input errors once, naming what is wrong and quoting
// what was sent.
func validOrganization(raw string) (string, error) {
	org := strings.TrimSpace(raw)
	switch {
	case org == "":
		return "", fmt.Errorf(
			"no organization was named: the collect id IS the GitHub organization to enumerate, " +
				"and it is also the graph this collect writes into. This collector reads no " +
				"environment variable to resolve one, so there is nothing to fall back to")
	case len(org) > organizationMaxLen:
		return "", fmt.Errorf(
			"the organization %q is %d characters; a GitHub organization login is at most %d",
			org, len(org), organizationMaxLen)
	case org[0] == '-' || org[len(org)-1] == '-':
		return "", fmt.Errorf(
			"the organization %q starts or ends with a hyphen; a GitHub organization login may not", org)
	}
	for _, r := range org {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return "", fmt.Errorf(
				"the organization %q contains %q; a GitHub organization login is letters, digits and "+
					"hyphens only. If you meant a repository such as %s, pass the organization alone",
				org, r, "acme/api")
		}
	}
	return org, nil
}
