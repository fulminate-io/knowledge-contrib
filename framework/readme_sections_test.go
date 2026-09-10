// SPDX-License-Identifier: Apache-2.0

package framework

import "testing"

// readme_sections_test.go — the README's TEACHING SECTIONS, each read at its own
// heading.
//
// WHY THIS FILE EXISTS, measured rather than supposed. The first version of this
// gate asserted the contract's vocabulary and its numbers, and nothing else: the
// refusal-class list, the declared foreign-graph context, the worked collector,
// the registration rules and the pointer to the client-side guide could each be
// DELETED WHOLE with the module's suite green. They are the sections a collector
// author in another language reads to write a provider at all, and this document
// is the only place they exist, so a silent deletion is the failure this whole
// file guards.
//
// EACH CASE IS SCOPED TO ITS SECTION. An assertion over the whole page passes on
// a corrected sentence that landed under some other heading and on a section
// deleted rather than fixed; the scoped read makes both impossible, and the
// section reader fatals when the heading is gone.
//
// THE PHRASES ARE THE LOAD-BEARING SENTENCES, not keywords. A keyword that also
// occurs in a neighboring paragraph observes the neighbor, not the claim —
// which is exactly how the rule-3 claim in the live file was found to be
// observing rule 1's text.

// TestREADMENamesEveryRefusalClass is the presence half of the refusal section.
// The absence half — that no unmeasurable total appears — is in
// readme_absence_test.go, and until this case existed the absence half was the
// ONLY thing observing the section: deleting the whole list, six class names and
// all, left every gate green.
//
// THE LIST GROWS. The declaration tool adds refusal classes to this same
// section, so a case that asserted a COUNT would red on a correct edit; each
// class is asserted by name instead.
func TestREADMENamesEveryRefusalClass(t *testing.T) {
	section := readmeSection(t, readmeDoc(t), "### Every refusal is by name, and nothing is written")

	assertClaims(t, section, []claim{
		{"Dial and handshake", "the class an operator meets first: the command cannot be spawned, or the child dies before the handshake"},
		{"The schema gate", "the class a port author meets first, and the one the two contract documents exist for"},
		{"The declaration", "the class the required declaration tool adds, which a port that serves no declaration will meet"},
		{"The call and its result", "the class a walk's own output is refused under, including an edge naming both graph fields"},
		{"The cross-graph resolution", "the class a cross-graph edge is refused under when its family or endpoint does not resolve"},
		{"The registration record itself", "the class read BEFORE any dial, which is why a hand-edited entry is refused without the collector running"},
	})
}

// TestREADMEDescribesTheDeclaredForeignContext observes ticket R1's
// foreign-context clause. The three phrases are the distinctions a collector
// author gets wrong: the block's shape, and the two absences that mean
// different things.
func TestREADMEDescribesTheDeclaredForeignContext(t *testing.T) {
	section := readmeSection(t, readmeDoc(t), "### The declared foreign-graph context arrives from the entry")

	assertClaims(t, section, []claim{
		{"keyed by graph-type name", "the block's shape; a reader who expects a fixed set of fields writes a decoder that drops every registered family"},
		{"present with an empty array", "a declared family whose store held no graph — a fact a collector is entitled to read"},
		{"has no key at all", "and the other absence, a family the entry never declared, which is a different fact from the one above"},
	})
}

// TestREADMECarriesAWorkedCollector observes ticket R1's worked example, which
// the prefill's own list left with no assertion at all: the entire Go example
// could be deleted with the suite green.
//
// THE TOOL NAME IS READ FROM THE FRAMEWORK'S CONSTANT rather than typed, so the
// example is checked against what a collector actually serves.
func TestREADMECarriesAWorkedCollector(t *testing.T) {
	section := readmeSection(t, readmeDoc(t), "## A worked collector")

	assertClaims(t, section, []claim{
		{DefaultToolName, "the served tool the majority shape uses, read from this package's own constant"},
		{"ServeStdio", "the entry point a collector main calls; an example that built a server by hand would teach the thing this module exists to remove"},
		{"signal.NotifyContext", "the context every collector main installs"},
		{"SIGTERM", "the signal a daemon stops a collector with — a cancelled walk that reported a complete enumeration would arm the deletion phase over everything it never reached"},
		{"Most collectors published here serve the default", "the majority tool shape the example mirrors; inverted, it sends a reader to serve a name of their own for no reason"},
	})
}

// TestREADMEStatesTheRegistrationRules observes the three registration facts an
// operator is wrong without, each of which was invertible with the suite green:
// the command that writes the entry, how the command is resolved, and which
// scope wins.
func TestREADMEStatesTheRegistrationRules(t *testing.T) {
	section := readmeSection(t, readmeDoc(t), "## Registering it is a config entry")

	assertClaims(t, section, []claim{
		{"knowledge collector add", "the command that writes the entry, dialing the collector first; a reader told only about a file edit never learns their provider is checked"},
		{"resolved against the daemon's PATH", "why a bare program name that works in a terminal resolves to nothing under a service manager"},
		{"The project scope beats the user scope", "the precedence, whole and with no per-field merge; inverted, an operator debugs the wrong file"},
	})
}

// TestREADMEStatesTheCompletenessConsequence observes the completeness section's
// own sentence. Deleting the section was green because the walk_complete token
// survives in the required-properties table and in wire-shaping rule 3 — the
// token is not the claim, and this asserts the consequence instead.
func TestREADMEStatesTheCompletenessConsequence(t *testing.T) {
	section := readmeSection(t, readmeDoc(t), "### An incomplete walk says so, and that is what protects the graph")

	assertClaims(t, section, []claim{
		{"deletes the half it never reached", "what asserting completeness for a walk that gave up half way actually does; without it the section reads as bookkeeping"},
	})
}

// TestREADMEPointsAtTheClientSideGuide observes ticket R1's pointer clause. The
// path is asserted rather than the word guide: a pointer a reader cannot follow
// is the same as no pointer.
func TestREADMEPointsAtTheClientSideGuide(t *testing.T) {
	section := readmeSection(t, readmeDoc(t), "## Where the rest is written")

	assertClaims(t, section, []claim{
		{"docs/guides/tools/custom_collector.md", "the client-side guide's path, which carries the operator's half: the full config reference, the CLI, the transports, the limits"},
	})
}
