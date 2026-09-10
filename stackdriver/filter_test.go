// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/logging"
)

// filter_test.go — the filter string, its escaping, and both severity maps.

// TestBuildFilterOrdersClausesAndJoinsWithNewlines pins the clause ORDER, which
// is part of the emitted query rather than a formatting choice: a filter built
// in a different order is a different string, and the query it expresses is what
// a re-collect has to reproduce.
func TestBuildFilterOrdersClausesAndJoinsWithNewlines(t *testing.T) {
	got := buildFilter("proj", logQuery{
		StartTime:    baseTime,
		EndTime:      baseTime.Add(time.Hour),
		SeverityMin:  severityError,
		Source:       "stderr",
		TextFilter:   "timeout",
		FieldFilters: map[string]string{"service": "api", "pod": "api-7b6"},
		RawQuery:     `jsonPayload.code = 500`,
	})
	want := strings.Join([]string{
		`timestamp >= "2026-09-07T12:00:00Z"`,
		`timestamp <= "2026-09-07T13:00:00Z"`,
		`severity >= ERROR`,
		`logName="projects/proj/logs/stderr"`,
		`"timeout"`,
		`resource.labels.pod_name="api-7b6"`,
		`resource.labels.service_name="api"`,
		`jsonPayload.code = 500`,
	}, "\n")
	if got != want {
		t.Fatalf("filter mismatch\n got: %q\nwant: %q", got, want)
	}
}

// TestBuildFilterEmptyQueryIsEmpty is the same-run control for the row above: a
// query naming nothing produces no clauses, so the assertion there is about the
// clauses and not about some constant preamble.
func TestBuildFilterEmptyQueryIsEmpty(t *testing.T) {
	if got := buildFilter("proj", logQuery{}); got != "" {
		t.Fatalf("an empty query built the filter %q, want the empty string", got)
	}
}

// TestBuildLogNameClauseExpandsShortNamesAndKeepsQualifiedOnes covers both arms
// of the log-name expansion.
func TestBuildLogNameClauseExpandsShortNamesAndKeepsQualifiedOnes(t *testing.T) {
	if got := buildLogNameClause("proj", "stderr"); got != `logName="projects/proj/logs/stderr"` {
		t.Errorf("short name expanded to %q", got)
	}
	if got := buildLogNameClause("proj", "projects/other/logs/stdout"); got != `logName="projects/other/logs/stdout"` {
		t.Errorf("qualified name became %q", got)
	}
}

// TestFilterInputsAreSanitizedAndEscaped is the injection arm. The log name and
// the field values are caller-supplied, which is to say model-supplied, so a
// value carrying a quote or a newline must not be able to close the string it
// sits in and open a clause of its own.
func TestFilterInputsAreSanitizedAndEscaped(t *testing.T) {
	got := buildFilter("proj", logQuery{
		Source:       `stderr" OR severity>=DEFAULT OR logName="x`,
		FieldFilters: map[string]string{"service": `api" OR "1"="1`},
		TextFilter:   `he said "hello"` + "\n" + `severity >= DEBUG`,
	})
	// A clause count that grew past the three inputs would mean a value ended
	// its own clause, which is what a raw newline in a value would do.
	if clauses := strings.Split(got, "\n"); len(clauses) != 3 {
		t.Fatalf("three inputs produced %d clauses, so a value opened one of its own:\n%s", len(clauses), got)
	}
	for clause := range strings.SplitSeq(got, "\n") {
		if countUnescapedQuotes(clause)%2 != 0 {
			t.Fatalf("clause %q has an odd number of unescaped quotes, so a value escaped its string", clause)
		}
	}
	if strings.Contains(got, `OR severity>=DEFAULT`) {
		t.Errorf("the source value's injected clause survived into the filter:\n%s", got)
	}
	if strings.Contains(got, `OR "1"="1`) {
		t.Errorf("the field value's injected clause survived into the filter:\n%s", got)
	}
	// KNOWN POSITIVE: the sanitizers did not simply empty everything.
	if !strings.Contains(got, "stderr") || !strings.Contains(got, "api") {
		t.Errorf("sanitizing removed the legitimate values too:\n%s", got)
	}
}

// TestEscapeValueEscapesBackslashesBeforeQuotes pins the ORDER of the two
// replacements. Reversed, the backslash pass would double the backslashes the
// quote pass just introduced and the value would decode differently.
func TestEscapeValueEscapesBackslashesBeforeQuotes(t *testing.T) {
	if got := escapeValue(`a\b"c`); got != `a\\b\"c` {
		t.Fatalf("escapeValue = %q, want %q", got, `a\\b\"c`)
	}
}

// TestAnAlreadyQuotedTextFilterIsNotDoubleQuotedButItsInteriorIsEscaped covers
// both arms of the quoting rule.
//
// THE POINT OF THE PASS-THROUGH IS THE OUTER QUOTES, NOT THE INTERIOR. An
// operator hand-writing an exact-match phrase should not get it re-quoted into
// nonsense; that says nothing about whether what is INSIDE their quotes may
// contain a clause separator. Escaping the interior keeps the first property and
// removes the second, and for an ordinary phrase the result is byte-identical to
// the old pass-through.
func TestAnAlreadyQuotedTextFilterIsNotDoubleQuotedButItsInteriorIsEscaped(t *testing.T) {
	if got := escapeTextFilter(`"exact phrase"`); got != `"exact phrase"` {
		t.Errorf("a quoted filter was re-quoted as %q", got)
	}
	if got := escapeTextFilter(`bare`); got != `"bare"` {
		t.Errorf("a bare filter became %q, want it quoted", got)
	}
	// The interior is escaped rather than trusted.
	if got := escapeTextFilter(`"a` + "\n" + `b"`); got != `"a\nb"` {
		t.Errorf("a quoted filter carrying a line break became %q, want its interior escaped", got)
	}
	// A lone quote is not a quoted filter: it has no interior to escape, so it
	// takes the ordinary quoting arm rather than being sliced into nothing.
	if got := escapeTextFilter(`"`); got != `"\""` {
		t.Errorf("a lone quote became %q", got)
	}
}

// TestAQuotedTextFilterCannotOpenASecondClause is the injection arm the
// pass-through branch had no test for.
//
// CLAUSES ARE JOINED BY NEWLINES, so a value carrying a raw line break is a value
// that can end its own clause and begin another. The unquoted branch escapes
// them; the quoted branch used to return the value verbatim, which meant the
// stated contract that every value but params.filter is sanitized and escaped
// did not hold on one of its two branches.
func TestAQuotedTextFilterCannotOpenASecondClause(t *testing.T) {
	injected := `"foo"` + "\n" + `logName="projects/other/logs/x"`
	got := buildFilter("proj", logQuery{TextFilter: injected})
	if clauses := strings.Split(got, "\n"); len(clauses) != 1 {
		t.Fatalf("a quoted text filter produced %d clauses, want 1:\n%s", len(clauses), got)
	}
	if strings.Contains(got, `logName="projects/other/logs/x"`) {
		t.Errorf("the injected clause survived unescaped into the filter:\n%s", got)
	}

	// SAME-RUN CONTROL: the UNQUOTED form of the same text was already safe, so
	// this arm is about the branch rather than about the text.
	unquoted := buildFilter("proj", logQuery{TextFilter: `foo` + "\n" + `logName="x"`})
	if clauses := strings.Split(unquoted, "\n"); len(clauses) != 1 {
		t.Fatalf("the unquoted control produced %d clauses, want 1:\n%s", len(clauses), unquoted)
	}
}

// TestSanitizeQueryValueStripsWhatEscapingWouldOnlyNeutralize is the second
// layer's own arm.
//
// ESCAPING IS THE PRIMARY DEFENSE AND THIS IS THE ONE BEHIND IT, which means it
// has to be discriminated on its own: a filter-level assertion passes on the
// escaping alone and says nothing about whether this function ever removed
// anything. Admitting a clause separator here left the whole suite green before
// this arm existed.
func TestSanitizeQueryValueStripsWhatEscapingWouldOnlyNeutralize(t *testing.T) {
	got := sanitizeQueryValue("api" + "\n" + `x" OR (1=1)` + "\r\t" + ";|`$")
	for _, forbidden := range []string{"\n", "\r", "\t", `"`, "(", ")", ";", "|", "`", "$"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("sanitizeQueryValue kept %q in %q", forbidden, got)
		}
	}
	// SAME-RUN CONTROL: an ordinary value survives intact, so the assertions
	// above are a strip rather than an empty return.
	const ordinary = "api-server.prod_1:8080/health@v2,x+y"
	if kept := sanitizeQueryValue(ordinary); kept != ordinary {
		t.Errorf("sanitizeQueryValue mangled an ordinary value: %q -> %q", ordinary, kept)
	}
}

// TestMapFieldFiltersTranslatesCanonicalNamesAndPassesOthers covers the mapping,
// the pass-through and the drop of a name that sanitizes to nothing.
func TestMapFieldFiltersTranslatesCanonicalNamesAndPassesOthers(t *testing.T) {
	got := mapFieldFilters(map[string]string{
		"service":                "api",
		"jsonPayload.request_id": "abc",
		`"; ("`:                  "x",
	})
	if got["resource.labels.service_name"] != "api" {
		t.Errorf("service was not translated: %v", got)
	}
	if got["jsonPayload.request_id"] != "abc" {
		t.Errorf("an unmapped field did not pass through: %v", got)
	}
	if len(got) != 2 {
		t.Errorf("a field name that sanitizes to nothing was kept: %v", got)
	}
}

// TestSanitizeFieldNameStripsOperatorsRatherThanRejectingTheName pins what
// happens to a name that is PARTLY unsafe: the operators are removed and the
// remainder survives. That is a deliberate posture and worth an arm of its own,
// because the drop above only covers the name that sanitizes to nothing.
func TestSanitizeFieldNameStripsOperatorsRatherThanRejectingTheName(t *testing.T) {
	if got := sanitizeFieldName(`"; DROP; "`); got != "DROP" {
		t.Errorf("sanitizeFieldName = %q, want the safe remainder %q", got, "DROP")
	}
	if got := sanitizeFieldName(`"; ("`); got != "" {
		t.Errorf("a wholly unsafe name sanitized to %q, want the empty string", got)
	}
}

// countUnescapedQuotes counts the double quotes that are not preceded by a
// backslash, which is the only quote count that says anything about whether a
// value closed the string it was in.
func countUnescapedQuotes(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '"' {
			continue
		}
		if i > 0 && s[i-1] == '\\' {
			continue
		}
		n++
	}
	return n
}

// TestMapSeverityToGCPCoversEveryCanonicalNameAndRefusesTheRest is both arms of
// the outbound map. The empty return is what makes buildFilter omit the clause
// rather than emit one naming a level the API would reject.
func TestMapSeverityToGCPCoversEveryCanonicalNameAndRefusesTheRest(t *testing.T) {
	for canonical, want := range map[string]string{
		severityTrace:    "DEBUG",
		severityDebug:    "DEBUG",
		severityInfo:     "INFO",
		severityWarn:     "WARNING",
		severityError:    "ERROR",
		severityCritical: "CRITICAL",
	} {
		if got := mapSeverityToGCP(canonical); got != want {
			t.Errorf("mapSeverityToGCP(%s) = %q, want %q", canonical, got, want)
		}
	}
	if got := mapSeverityToGCP("NOTASEVERITY"); got != "" {
		t.Errorf("an unknown severity mapped to %q, want the empty string", got)
	}
}

// TestMapGCPSeverityFloorsToTheHighestNamedLevelAtOrBelow covers every arm of
// the inbound map INCLUDING the intermediate numeric values the API admits
// between the named levels, which is the reason the arms compare with >= rather
// than ==.
//
// THE NAME SAYS FLOOR BECAUSE THE ARMS PIN A FLOOR. The knowledge client's own
// adapter carries this switch under a comment claiming it rounds up; the source
// doc beside this map corrects that, and this name is the other half of the same
// correction.
func TestMapGCPSeverityFloorsToTheHighestNamedLevelAtOrBelow(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   logging.Severity
		want string
	}{
		{"emergency", logging.Emergency, severityCritical},
		{"alert", logging.Alert, severityCritical},
		{"critical", logging.Critical, severityCritical},
		{"error", logging.Error, severityError},
		{"warning", logging.Warning, severityWarn},
		{"notice", logging.Notice, severityInfo},
		{"info", logging.Info, severityInfo},
		{"debug", logging.Debug, severityDebug},
		{"default", logging.Default, severityInfo},
		// The in-between values take the highest NAMED level at or below them,
		// which is a floor rather than a round-up. These three arms are what
		// pin that direction.
		{"between warning and error floors to warn", logging.Warning + 50, severityWarn},
		{"between info and notice floors to info", logging.Info + 50, severityInfo},
		{"between error and critical floors to error", logging.Error + 50, severityError},
		{"above emergency stays critical", logging.Emergency + 100, severityCritical},
	} {
		if got := mapGCPSeverity(tc.in); got != tc.want {
			t.Errorf("%s: mapGCPSeverity(%v) = %s, want %s", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestEverySeverityAnEntryCanCarryIsCanonical is what makes the empty-suffix
// alias arm unreachable from a collect: no numeric severity produces a value
// outside the six, so no template a collect produces carries an unmapped one.
func TestEverySeverityAnEntryCanCarryIsCanonical(t *testing.T) {
	for s := logging.Default; s <= logging.Emergency+200; s++ {
		got := mapGCPSeverity(s)
		if _, ok := severityOrder[got]; !ok {
			t.Fatalf("severity %v mapped to %q, which is outside the canonical six", s, got)
		}
		if severityShort(got) == "" {
			t.Fatalf("severity %v mapped to %q, which derives no alias suffix", s, got)
		}
	}
}
