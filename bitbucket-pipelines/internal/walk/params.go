// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"fmt"
	"strings"
)

// params.go — the collect's own parameters and the collect id, validated once
// and refused loudly.

// Params is the collect tool's parameter object. The framework infers its
// schema, advertises it inside the contract's input schema, and validates a
// call's arguments against it before [Collector.Walk] is reached — so a value of
// the wrong KIND is refused by the schema and never arrives here, and what this
// file checks is the values a correctly typed call can still get wrong.
//
// THERE IS NO `workspace` PARAMETER AND THERE IS NO HISTORY-DEPTH PARAMETER, and
// both absences are decisions. The workspace is the collect id; see
// [Collector.Walk]. The history depth is an environment selector; see
// [historyDepth].
//
// THE ONE PARAMETER IS A POINTER, and that is the difference between "unset" and
// "zero". A caller that omits the bound wants this collector's default; a caller
// that sends 0 has asked for no enumerations to run at once, which is not a thing
// to do and is refused by name. A plain int cannot tell those apart and would
// silently turn the second into the first.
type Params struct {
	// MaxConcurrency bounds how many of the five parallel enumerations run at
	// once.
	MaxConcurrency *int `json:"max_concurrency,omitempty" jsonschema:"how many enumerations to run at once (default 10, maximum 32)"`
}

// ConcurrencyCeiling is the largest value the bound may take. It is EXPORTED for
// the same reason [DefaultConcurrency] is: the README's parameter table states
// it, and the gate that reads that table compares it against this constant
// rather than against a second literal.
//
// IT REFUSES RATHER THAN CLAMPS. This walk runs five enumerations in its
// parallel phase, so any value at or above five is already "all of them"; a
// request for 500 would be answered with five and reported as a successful
// collect at 500, telling the caller it got a hundred times the parallelism it
// did. The ceiling is above the enumeration count deliberately, so a caller who
// asks for headroom against a later phase is not refused for asking.
const ConcurrencyCeiling = 32

// concurrency validates the one parameter and resolves the default.
func (p Params) concurrency() (int, error) {
	if p.MaxConcurrency == nil {
		return DefaultConcurrency, nil
	}
	value := *p.MaxConcurrency
	switch {
	case value < 1:
		return 0, fmt.Errorf(
			"max_concurrency is %d; it is how many enumerations run at once, so it is at least 1. "+
				"Omit it to use this collector's default of %d", value, DefaultConcurrency)
	case value > ConcurrencyCeiling:
		return 0, fmt.Errorf(
			"max_concurrency is %d, above the maximum of %d. This walk runs %d enumerations in "+
				"parallel, so a larger value cannot be honored and is refused rather than silently "+
				"answered with %d",
			value, ConcurrencyCeiling, parallelEnumerations, ConcurrencyCeiling)
	default:
		return value, nil
	}
}

// The provider's own rules for a workspace id: 1 to 62 characters, lower-case
// alphanumerics, hyphens and underscores.
const workspaceMaxLen = 62

// validWorkspace trims and validates the collect id.
//
// IT IS VALIDATED HERE RATHER THAN PASSED THROUGH because this id is
// interpolated into every request path the walk builds AND into every node id it
// emits. A malformed one passed through reaches the caller as six per-
// enumeration errors, or worse as a walk that matched nothing and looked like an
// empty workspace. Bad input errors once, naming what is wrong and quoting what
// was sent.
func validWorkspace(raw string) (string, error) {
	workspace := strings.TrimSpace(raw)
	switch {
	case workspace == "":
		return "", fmt.Errorf(
			"no workspace was named: the collect id IS the Bitbucket workspace to enumerate, and " +
				"it is also the graph this collect writes into. This collector reads no environment " +
				"variable and takes no parameter naming one, so there is nothing to fall back to")
	case len(workspace) > workspaceMaxLen:
		return "", fmt.Errorf(
			"the workspace %q is %d characters; a Bitbucket workspace id is at most %d",
			workspace, len(workspace), workspaceMaxLen)
	case workspace[0] == '-' || workspace[len(workspace)-1] == '-':
		return "", fmt.Errorf(
			"the workspace %q starts or ends with a hyphen; a Bitbucket workspace id may not",
			workspace)
	}
	for _, r := range workspace {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return "", fmt.Errorf(
				"the workspace %q contains %q; a Bitbucket workspace id is lower-case letters, "+
					"digits, hyphens and underscores only. If you meant a repository such as %s, "+
					"pass the workspace alone",
				workspace, r, "acme/api")
		}
	}
	return workspace, nil
}
