// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
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
// has asked for no pipeline runs at all, which is not a thing to collect and is
// refused by name. A plain int cannot tell those apart and would silently turn
// the second into the first.
type Params struct {
	// MaxPipelineRuns bounds how many recent pipeline runs are read PER PROJECT.
	MaxPipelineRuns *int `json:"max_pipeline_runs,omitempty" jsonschema:"how many recent pipeline runs to read per project (default 20, maximum 100)"`
	// MaxDeployments bounds how many recent deployments are read PER PROJECT.
	MaxDeployments *int `json:"max_deployments,omitempty" jsonschema:"how many recent deployments to read per project (default 20, maximum 100)"`
}

// capCeiling is the largest value either cap may take.
//
// IT IS THE PROVIDER'S OWN PAGE MAXIMUM, AND A LARGER REQUEST IS REFUSED RATHER
// THAN CLAMPED. These two enumerations read a single page, so a request for 500
// would be answered with 100 and reported as a successful collect of 500 — the
// caller would be told it got five times what it did. Refusing names the ceiling
// and leaves the caller to ask for something this collector can deliver.
const capCeiling = 100

// caps validates the two parameters and resolves the defaults.
func (p Params) caps() (collect.Caps, error) {
	maxRuns, err := boundedCap("max_pipeline_runs", p.MaxPipelineRuns, collect.DefaultMaxPipelineRuns)
	if err != nil {
		return collect.Caps{}, err
	}
	maxDeployments, err := boundedCap("max_deployments", p.MaxDeployments, collect.DefaultMaxDeployments)
	if err != nil {
		return collect.Caps{}, err
	}
	return collect.Caps{MaxPipelineRuns: maxRuns, MaxDeployments: maxDeployments}, nil
}

// boundedCap resolves one cap: unset takes the default, and a value outside
// 1..[capCeiling] is refused naming the parameter, what was sent and the bound
// it broke.
func boundedCap(name string, value *int, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	switch {
	case *value < 1:
		return 0, fmt.Errorf(
			"%s is %d; it is how many items to read per project, so it is at least 1. "+
				"Omit it to read this collector's default of %d", name, *value, fallback)
	case *value > capCeiling:
		return 0, fmt.Errorf(
			"%s is %d, above the maximum of %d. This enumeration reads a single page and the "+
				"provider's page maximum is %d, so a larger value cannot be honored and is refused "+
				"rather than silently answered with %d",
			name, *value, capCeiling, capCeiling, capCeiling)
	default:
		return *value, nil
	}
}

// The provider's own rules for a group's full path: slash-separated segments,
// each of alphanumerics, underscores, dots and hyphens, starting with an
// alphanumeric or an underscore, and none of them a reserved suffix.
const (
	groupPathMaxLen = 255
	// reservedSuffixes are the two endings the provider gives its own meaning to
	// on a path segment, so neither can be a group or project name.
	gitSuffix  = ".git"
	atomSuffix = ".atom"
)

// validGroup trims and validates the collect id.
//
// IT IS VALIDATED HERE RATHER THAN PASSED THROUGH because this id is
// interpolated into every request path the walk builds AND into every node id it
// emits. A malformed one passed through reaches the caller as seven per-
// enumeration errors, or worse as a walk that matched nothing and looked like an
// empty group. Bad input errors once, naming what is wrong and quoting what was
// sent.
//
// A GITLAB GROUP PATH IS NOT A GITHUB ORGANIZATION LOGIN. It may carry SLASHES,
// because a subgroup's full path is its ancestors joined by them — `acme/platform`
// is a perfectly ordinary collect id here and would be a malformed one on the
// sibling collector. So the rule is written for this provider's own grammar
// rather than borrowed, and the slash is validated per segment instead of being
// refused outright.
func validGroup(raw string) (string, error) {
	group := strings.TrimSpace(raw)
	switch {
	case group == "":
		return "", fmt.Errorf(
			"no group was named: the collect id IS the GitLab group to enumerate, and it is also " +
				"the graph this collect writes into. This collector reads no environment variable " +
				"to resolve one, so there is nothing to fall back to")
	case len(group) > groupPathMaxLen:
		return "", fmt.Errorf(
			"the group %q is %d characters; a GitLab group path is at most %d",
			group, len(group), groupPathMaxLen)
	case strings.Contains(group, "://"):
		return "", fmt.Errorf(
			"the group %q looks like a URL. The collect id is the group's PATH, such as "+
				"acme or acme/platform; the instance this collector talks to is chosen by its "+
				"environment, not by this id", group)
	case strings.HasPrefix(group, "/") || strings.HasSuffix(group, "/"):
		return "", fmt.Errorf(
			"the group %q starts or ends with a slash; a GitLab group path is its segments joined "+
				"by slashes, such as acme/platform, with none on either end", group)
	}

	for segment := range strings.SplitSeq(group, "/") {
		if err := validSegment(group, segment); err != nil {
			return "", err
		}
	}
	return group, nil
}

// validSegment checks one path segment of a group path.
func validSegment(group, segment string) error {
	switch {
	case segment == "":
		return fmt.Errorf(
			"the group %q carries an empty path segment; each segment between slashes names one "+
				"group", group)
	case strings.HasSuffix(segment, gitSuffix), strings.HasSuffix(segment, atomSuffix):
		return fmt.Errorf(
			"the group %q ends a segment with %q or %q, which GitLab reserves and no group may be "+
				"named", group, gitSuffix, atomSuffix)
	}

	// THE RUNE, NOT THE BYTE. segment[0] is a byte, and converting it to a rune
	// reinterprets the leading byte of a multi-byte UTF-8 sequence as a code
	// point — so a refusal would quote a character the caller did not type. The
	// BEHAVIOR is the same either way, since a GitLab path segment is ASCII and a
	// non-ASCII one is rightly refused; what changes is whether the operator is
	// told which character their group actually starts with.
	first, _ := utf8.DecodeRuneInString(segment)
	if !isAlphanumeric(first) && first != '_' {
		return fmt.Errorf(
			"the segment %q of the group %q starts with %q; a GitLab path segment starts with a "+
				"letter, a digit or an underscore", segment, group, first)
	}
	for _, r := range segment {
		if isAlphanumeric(r) || r == '_' || r == '.' || r == '-' {
			continue
		}
		return fmt.Errorf(
			"the group %q contains %q; a GitLab group path is letters, digits, underscores, dots "+
				"and hyphens, in segments joined by slashes. If you meant a project such as "+
				"acme/api, pass the group alone", group, r)
	}
	return nil
}

func isAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
