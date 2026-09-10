// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strconv"
	"strings"
	"testing"
)

// defaults_parsing_test.go — the MESSAGE-PARSING and LABEL defaults. They set
// the parsed severity, which sets a template's alias and its symbol name, and
// the stream-name truncation, which is hashed into the stream id.

// TestStreamNameLimitIsObserved straddles the truncation trigger: a name at the
// limit survives whole, one past it is cut. The label is hashed into the stream
// id, so the trigger point is part of stream identity.
func TestStreamNameLimitIsObserved(t *testing.T) {
	atLimit := strings.Repeat("a", builtinStreamNameLimit)
	entry, err := normalizeEntry(event(1772366774000, atLimit, "x"), "/g")
	if err != nil {
		t.Fatalf("normalizeEntry: %v", err)
	}
	if entry.Labels["log_stream"] != atLimit {
		t.Errorf("a name of exactly %d characters was truncated", builtinStreamNameLimit)
	}

	pastLimit := strings.Repeat("a", builtinStreamNameLimit+1)
	entry, err = normalizeEntry(event(1772366774000, pastLimit, "x"), "/g")
	if err != nil {
		t.Fatalf("normalizeEntry: %v", err)
	}
	if entry.Labels["log_stream"] == pastLimit {
		t.Errorf("a name of %d characters was not truncated", builtinStreamNameLimit+1)
	}
	if len(entry.Labels["log_stream"]) != builtinStreamNameKeep+3 {
		t.Errorf("the truncated name is %d characters, want %d kept plus an ellipsis",
			len(entry.Labels["log_stream"]), builtinStreamNameKeep)
	}
}

// TestSeverityPrefixMaxLenIsObserved straddles how far into a line a leading
// severity token may end. EMERGENCY is nine characters, one past the production
// limit, so it is NOT read as a prefix; ERROR is five and is.
func TestSeverityPrefixMaxLenIsObserved(t *testing.T) {
	msg, sev := parseLogMessage("EMERGENCY disk failure")
	if sev != SeverityInfo || msg != "EMERGENCY disk failure" {
		t.Errorf("a nine-character severity token was read as a prefix (message %q severity %q); the limit "+
			"is %d", msg, sev, builtinSeverityPrefixMaxLen)
	}
	msg, sev = parseLogMessage("ERROR disk failure")
	if sev != SeverityError || msg != "disk failure" {
		t.Errorf("a five-character severity token was NOT read as a prefix (message %q severity %q)", msg, sev)
	}
}

// TestMaxJSONNestingIsObserved straddles the container-unwrapping depth: a
// payload nested one level deeper than the limit stops being unwrapped and the
// remaining JSON is the message.
func TestMaxJSONNestingIsObserved(t *testing.T) {
	innermost := `{"level":"error","message":"the innermost message"}`
	wrapped := innermost
	for range builtinMaxJSONNesting + 1 {
		wrapped = `{"log":` + strconv.Quote(wrapped) + `}`
	}
	msg, _ := parseLogMessage(wrapped)
	if msg == "the innermost message" {
		t.Errorf("a payload nested %d deep was fully unwrapped though the limit is %d",
			builtinMaxJSONNesting+1, builtinMaxJSONNesting)
	}

	// The control at the limit, which IS unwrapped.
	within := innermost
	for range builtinMaxJSONNesting - 1 {
		within = `{"log":` + strconv.Quote(within) + `}`
	}
	if msg, _ := parseLogMessage(within); msg != "the innermost message" {
		t.Errorf("a payload nested %d deep was not unwrapped: %q", builtinMaxJSONNesting-1, msg)
	}
}

// TestEmbeddedSeverityScanWindowIsObserved straddles how much of a line the
// level scan reads. A marker just past the window is not found; the same marker
// just inside it is.
func TestEmbeddedSeverityScanWindowIsObserved(t *testing.T) {
	const marker = "level=error"

	// THE STRADDLE IS ONE CHARACTER WIDE, which is what a by-one drift needs.
	// A marker ENDING exactly at the limit is read whole; the same marker one
	// character later has its last character cut, and the truncated token
	// parses as the default level rather than the one the producer wrote — a
	// silent misclassification rather than a visible absence, which is the
	// failure this bound produces in the field.
	justInside := strings.Repeat("p", builtinSeverityScanLimit-len(marker)) + marker
	if got := DetectEmbeddedSeverity(justInside); got != SeverityError {
		t.Errorf("a marker ending exactly at character %d was read as %q, want %q",
			builtinSeverityScanLimit, got, SeverityError)
	}

	justOutside := strings.Repeat("p", builtinSeverityScanLimit-len(marker)+1) + marker
	if got := DetectEmbeddedSeverity(justOutside); got == SeverityError {
		t.Errorf("a marker ending one character past %d was still read as %q; the scan window does not bound it",
			builtinSeverityScanLimit, got)
	}

	// And a marker far past the window is not seen at all.
	if got := DetectEmbeddedSeverity(strings.Repeat("p", builtinSeverityScanLimit*2) + " " + marker); got != "" {
		t.Errorf("a marker far past the window was detected as %q", got)
	}
}
