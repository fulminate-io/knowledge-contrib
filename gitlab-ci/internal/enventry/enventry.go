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
// THE WHOLE CLOSURE IS THREE NAMES: two spellings of one credential, and one
// selector. There is no proxy name, no trust-root name and no home directory
// here, and their absence is deliberate rather than an oversight: this collector
// reads no file-based credential chain and resolves nothing from a config
// directory.
package enventry

import "slices"

// The two names the token is read from, in the order they are consulted.
//
// TWO NAMES RATHER THAN ONE because both are in wide use: the first is what a
// pipeline and most tooling set, the second is what the provider's own API
// documentation and several clients use. A collector that read only one would
// work on half the machines it is installed on and fail on the other half with a
// message about a variable the operator had in fact set.
const (
	// PrimaryTokenVariable is consulted first.
	PrimaryTokenVariable = "GITLAB_TOKEN"
	// FallbackTokenVariable is consulted when the primary is unset or empty.
	FallbackTokenVariable = "GITLAB_PRIVATE_TOKEN"
	// BaseURLVariable selects the instance this collector talks to. It is NOT a
	// credential: it names a host, it is written into the config entry as a
	// literal when the installing shell sets one, and it is omitted entirely when
	// it does not.
	BaseURLVariable = "GITLAB_URL"
)

// Names returns every environment variable name this collector reads, in
// consultation order.
//
// THE ORDER IS THE CONSULTATION ORDER, not sorted, because for the two token
// names it is the answer to "which one wins" as well as to "which ones are read".
func Names() []string {
	return []string{PrimaryTokenVariable, FallbackTokenVariable, BaseURLVariable}
}

// TokenNames returns the credential names in consultation order, which is what
// the token resolver walks.
func TokenNames() []string { return []string{PrimaryTokenVariable, FallbackTokenVariable} }

// Class is what an installer does with a declared name.
type Class string

const (
	// ClassSecret is a credential, whose value the installing script must never
	// write into a config file — not the value, and not a reference to it either.
	// The serving environment supplies it.
	ClassSecret Class = "secret"
	// ClassSelector is a non-credential choice, written into the entry as a
	// literal when the installing shell has one and omitted entirely when it does
	// not. An empty selector is not the same as an absent one: it selects the
	// thing named by the empty string, which resolves nothing.
	ClassSelector Class = "selector"
)

// Classes returns each name's class, which is what an install table declares.
//
// THIS COLLECTOR IS NOT THE ALL-SECRET SHAPE. One of its three names is a
// selector, so the entry an installer writes carries an environment block with at
// most that one key in it — and no block at all when the installing shell does
// not set it.
func Classes() map[string]Class {
	return map[string]Class{
		PrimaryTokenVariable:  ClassSecret,
		FallbackTokenVariable: ClassSecret,
		BaseURLVariable:       ClassSelector,
	}
}

// IsDeclared reports whether a name is one this collector reads.
func IsDeclared(name string) bool { return slices.Contains(Names(), name) }
