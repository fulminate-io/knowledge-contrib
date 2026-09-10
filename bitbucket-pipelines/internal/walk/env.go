// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"fmt"
	"os"
	"strconv"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/enventry"
)

// env.go — the three environment values this collector reads, each refused
// loudly when it is present and unusable.

// DefaultHistoryDepth is how many recent pipeline runs are read per repository
// when the environment names no other number. It is the source provider's value.
const DefaultHistoryDepth = 50

// credentials reads the two halves of this collector's credential.
//
// BOTH ARE REQUIRED TOGETHER AND A MISSING ONE NAMES BOTH. An app password
// without its username authenticates nothing, and an operator who set one and
// not the other is one keystroke from working — so the message names the pair
// rather than only the half that is missing.
//
// PRESENT-AND-EMPTY IS MISSING. A stdio child receives exactly what its entry
// declares, and an entry that wrote `${BITBUCKET_APP_PASSWORD:-}` would deliver
// the name present and empty on every collect; treating that as set would send
// an empty password to the provider and report the refusal as the operator's
// permissions problem. The bare `${BITBUCKET_APP_PASSWORD}` an installer writes
// has no such default: a serving process that does not hold the name refuses
// that ENTRY by name and this collector is never started at all.
func credentials() (username, appPassword string, err error) {
	username = os.Getenv(enventry.UsernameVariable)
	appPassword = os.Getenv(enventry.AppPasswordVariable)
	if username == "" || appPassword == "" {
		return "", "", fmt.Errorf(
			"this collector authenticates from its environment and both halves are required: "+
				"%s and %s must each arrive non-empty in this collector's config entry's "+
				"environment. The entry names each one as a reference to your own environment — "+
				"%s and %s — and the process serving this collect supplies the values; neither "+
				"value belongs in the config file. There is no flag, no parameter and no file "+
				"this credential can come from instead",
			enventry.UsernameVariable, enventry.AppPasswordVariable,
			`"`+enventry.UsernameVariable+`": "${`+enventry.UsernameVariable+`}"`,
			`"`+enventry.AppPasswordVariable+`": "${`+enventry.AppPasswordVariable+`}"`)
	}
	return username, appPassword, nil
}

// historyDepth resolves how many recent runs to read per repository.
//
// UNSET MEANS THE DEFAULT AND ANYTHING ELSE UNUSABLE IS REFUSED BY NAME. The
// source provider parses this value with `if n, err := strconv.Atoi(v); err ==
// nil && n > 0`, so a non-integer, a zero and a negative are all silently
// ignored and the walk proceeds at the default — an operator who set it to 100O
// with a letter O gets fifty runs and no indication that the number they typed
// was never read. That is exactly the silent coercion this repository's
// bad-input rule forbids, so this collector refuses instead, quoting what was
// sent and naming the bound it broke.
//
// PRESENT-AND-EMPTY IS REFUSED TOO rather than treated as unset: an empty
// selector selects the thing named by the empty string, which resolves nothing.
// That is also why an installer omits the key entirely when the value is unset
// rather than writing it empty.
func historyDepth() (int, error) {
	raw, present := os.LookupEnv(enventry.HistoryDepthVariable)
	if !present {
		return DefaultHistoryDepth, nil
	}
	if raw == "" {
		return 0, fmt.Errorf(
			"%s is set to the empty string. It selects how many recent pipeline runs to read per "+
				"repository, and the empty string names no number; unset it to read this collector's "+
				"default of %d",
			enventry.HistoryDepthVariable, DefaultHistoryDepth)
	}
	depth, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf(
			"%s is %q, which is not a whole number. It is how many recent pipeline runs to read per "+
				"repository; unset it to read this collector's default of %d",
			enventry.HistoryDepthVariable, raw, DefaultHistoryDepth)
	}
	if depth < 1 {
		return 0, fmt.Errorf(
			"%s is %d; it is how many recent pipeline runs to read per repository, so it is at "+
				"least 1. Unset it to read this collector's default of %d",
			enventry.HistoryDepthVariable, depth, DefaultHistoryDepth)
	}
	return depth, nil
}
