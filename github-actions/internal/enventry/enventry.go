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
// THE WHOLE CLOSURE IS TWO NAMES AND BOTH ARE CREDENTIALS. There is no proxy
// name, no trust-root name and no home directory here, and their absence is
// deliberate rather than an oversight: this collector reads no file-based
// credential chain and resolves nothing from a config directory, so the only
// thing it needs from its environment is the token.
package enventry

import "slices"

// The two names the token is read from, in the order they are consulted.
//
// TWO NAMES RATHER THAN ONE because both are in wide use: the first is what a
// workflow and most tooling set, the second is what the provider's own command
// line writes. A collector that read only one would work on half the machines
// it is installed on and fail on the other half with a message about a variable
// the operator had in fact set.
const (
	// PrimaryTokenVariable is consulted first.
	PrimaryTokenVariable = "GITHUB_TOKEN"
	// FallbackTokenVariable is consulted when the primary is unset or empty.
	FallbackTokenVariable = "GH_TOKEN"
)

// Names returns every environment variable name this collector reads, in
// consultation order.
//
// THE ORDER IS THE CONSULTATION ORDER, not sorted, because it is the answer to
// "which one wins" as well as to "which ones are read".
func Names() []string { return []string{PrimaryTokenVariable, FallbackTokenVariable} }

// Class is what an installer does with a declared name.
type Class string

// ClassSecret is the only class this collector's names take: a credential, whose
// VALUE the installing script must never write into a config file. What it does
// write for a name the installing shell holds is a bare `${NAME}` reference to
// the operator's own environment, which the process serving the collect expands
// at spawn — and refuses by name, on that entry alone, when it does not hold it.
const ClassSecret Class = "secret"

// Classes returns each name's class, which is what an install table declares.
// Both of this collector's names are [ClassSecret], so its written entry's whole
// environment block is a reference to whichever of the two the installing shell
// held — and no value.
func Classes() map[string]Class {
	return map[string]Class{
		PrimaryTokenVariable:  ClassSecret,
		FallbackTokenVariable: ClassSecret,
	}
}

// IsDeclared reports whether a name is one this collector reads.
func IsDeclared(name string) bool { return slices.Contains(Names(), name) }
