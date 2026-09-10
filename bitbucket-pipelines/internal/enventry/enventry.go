// SPDX-License-Identifier: Apache-2.0

// Package enventry declares the ENVIRONMENT NAMES this collector reads, and it
// is the only place in the module that names one.
//
// WHY THIS IS CODE AND NOT PROSE. A stdio collector receives EXACTLY the
// environment its config entry declares: the daemon copies nothing and adds
// nothing, so a name the collector reads and the entry omits is a name the child
// does not have. Declaring the set here lets a test census the module's own
// source and assert that the names it reads and the names it declares are the
// same set — an assertion no README sentence can make.
//
// THE CLOSURE IS THREE NAMES IN TWO CLASSES. Two are credentials and one is a
// tuning selector. There is no proxy name, no trust-root name and no home
// directory here, and their absence is deliberate rather than an oversight: this
// collector reads no file-based credential chain and resolves nothing from a
// config directory, so the only things it needs from its environment are the two
// halves of one credential and one optional depth.
package enventry

import "slices"

// The two halves of this collector's credential. BOTH ARE REQUIRED TOGETHER: an
// app password without the username it belongs to authenticates nothing, so a
// walk with one of them fails naming both rather than proceeding to be refused
// by the provider.
const (
	// UsernameVariable is the Bitbucket account the app password belongs to.
	UsernameVariable = "BITBUCKET_USERNAME"
	// AppPasswordVariable is the app password itself. It is a credential and its
	// value never reaches a node, a log line, a file or an argv.
	AppPasswordVariable = "BITBUCKET_APP_PASSWORD"
)

// HistoryDepthVariable overrides how many recent pipeline runs are read per
// repository.
//
// IT IS AN ENVIRONMENT SELECTOR AND NOT A COLLECT PARAMETER, deliberately. The
// value already has an environment home in the provider this module reproduces,
// and adding a parameter beside it would make one value two sources of truth
// with a precedence question no caller has asked. Unset means this collector's
// default; set, it must be a positive integer and anything else is refused by
// name rather than silently ignored.
const HistoryDepthVariable = "BITBUCKET_PIPELINE_HISTORY_DEPTH"

// Names returns every environment variable name this collector reads, in
// consultation order.
//
// THE ORDER IS THE CONSULTATION ORDER, not sorted, because it is the answer to
// "which is read first" as well as to "which ones are read".
func Names() []string {
	return []string{UsernameVariable, AppPasswordVariable, HistoryDepthVariable}
}

// Class is what an installer does with a declared name.
type Class string

const (
	// ClassSecret is a credential, whose value the installing script must never
	// write into a config file — not the value, and not a reference to it either.
	// The serving environment supplies it.
	ClassSecret Class = "secret"
	// ClassSelector is a non-secret tuning value: an installer writes it as a
	// LITERAL when it is set in the installing shell, and omits the key entirely
	// when it is not. Present-and-empty is a different input from absent and this
	// collector refuses it, which is why the key is omitted rather than written
	// empty.
	ClassSelector Class = "selector"
)

// Classes returns each name's class, which is what an install table declares.
//
// THIS COLLECTOR'S ENTRY IS THE FIRST MIXED-CLASS ONE: its written entry carries
// an environment block holding exactly one key when the selector is set in the
// installing shell, and no block at all when it is not — and neither credential
// name appears in either state.
func Classes() map[string]Class {
	return map[string]Class{
		UsernameVariable:     ClassSecret,
		AppPasswordVariable:  ClassSecret,
		HistoryDepthVariable: ClassSelector,
	}
}

// EmptySensitive reports whether this collector tells the name PRESENT AND EMPTY
// apart from ABSENT.
//
// ONLY THE DEPTH SELECTOR DOES, and the difference between the three names is
// measured rather than assumed. `historyDepth` reads its name with os.LookupEnv
// and refuses the empty string BY NAME, because an empty selector selects the
// thing named by "" and unset means this collector's default — two different
// answers. `credentials` reads its two names with os.Getenv and guards on `==
// ""`, which cannot distinguish the two states at all, so both produce the same
// refusal naming the pair.
//
// WHY THE DISTINCTION IS DECLARED. A worked entry that renders `${NAME:-}` hands
// the child that name present and empty, because the reference is expanded by the
// process serving the collect and resolves to the empty string. For the depth
// selector that is a collect that never runs; for the two credential halves it is
// the same refusal an absent one gives. The installer's documentation gate refuses
// the defaulted form for a marked name and admits it for the rest, and this is
// where it reads the answer. The walk package's own empty_sensitive_test.go drives
// all three names and fails if this function and the measurement disagree.
func EmptySensitive(name string) bool { return name == HistoryDepthVariable }

// IsDeclared reports whether a name is one this collector reads.
func IsDeclared(name string) bool { return slices.Contains(Names(), name) }
