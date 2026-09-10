// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// normalize_test.go — the three labels, the four message shapes, and the
// refusal that replaces the built-in adapter's wall-clock default.

// event builds a recorded CloudWatch event.
func event(ts int64, stream, message string) cwtypes.FilteredLogEvent {
	e := cwtypes.FilteredLogEvent{
		EventId:   aws.String("e-1"),
		Timestamp: aws.Int64(ts),
		Message:   aws.String(message),
	}
	if stream != "" {
		e.LogStreamName = aws.String(stream)
	}
	return e
}

// TestNormalizeSetsExactlyThreeLabels pins the label set, which is what the
// stream id is hashed over: a fourth label would change every stream id.
func TestNormalizeSetsExactlyThreeLabels(t *testing.T) {
	entry, err := normalizeEntry(event(1772366774000, "ecs-task-1", "hello"), "/ecs/prod/api-server")
	if err != nil {
		t.Fatalf("normalizeEntry: %v", err)
	}
	want := map[string]string{
		"log_group":  "/ecs/prod/api-server",
		"service":    "api-server",
		"log_stream": "ecs-task-1",
	}
	if len(entry.Labels) != len(want) {
		t.Errorf("labels = %v, want exactly %v", entry.Labels, want)
	}
	for k, v := range want {
		if entry.Labels[k] != v {
			t.Errorf("label %q = %q, want %q", k, entry.Labels[k], v)
		}
	}
}

// TestServiceIsTheLastPathSegmentAndIsSharedByEveryStream pins the surprising
// half: the service label is derived from the GROUP, so two streams in one
// group carry the same one and it never distinguishes them.
func TestServiceIsTheLastPathSegmentAndIsSharedByEveryStream(t *testing.T) {
	for _, tc := range []struct{ group, want string }{
		{"/ecs/prod/api-server", "api-server"},
		{"/aws/lambda/my_function", "my_function"},
		{"flat-name", "flat-name"},
		{"/trailing/", ""},
	} {
		if got := extractService(tc.group); got != tc.want {
			t.Errorf("extractService(%q) = %q, want %q", tc.group, got, tc.want)
		}
	}

	a, err := normalizeEntry(event(1, "stream-a", "x"), "/ecs/prod/api-server")
	if err != nil {
		t.Fatalf("normalizeEntry: %v", err)
	}
	b, err := normalizeEntry(event(2, "stream-b", "x"), "/ecs/prod/api-server")
	if err != nil {
		t.Fatalf("normalizeEntry: %v", err)
	}
	if a.Labels["service"] != b.Labels["service"] {
		t.Error("two streams of one log group carry different service labels")
	}
}

// TestStreamNameTruncationIsPartOfTheIdentity covers the 60-character bound.
// The truncated form is hashed into the stream id, so two names differing only
// past the bound are ONE stream.
func TestStreamNameTruncationIsPartOfTheIdentity(t *testing.T) {
	exactly60 := strings.Repeat("a", 60)
	entry, err := normalizeEntry(event(1, exactly60, "x"), "/g")
	if err != nil {
		t.Fatalf("normalizeEntry: %v", err)
	}
	if entry.Labels["log_stream"] != exactly60 {
		t.Errorf("a 60-character name was truncated to %q", entry.Labels["log_stream"])
	}

	long := strings.Repeat("b", 75)
	entry, err = normalizeEntry(event(1, long, "x"), "/g")
	if err != nil {
		t.Fatalf("normalizeEntry: %v", err)
	}
	want := strings.Repeat("b", 57) + "..."
	if entry.Labels["log_stream"] != want {
		t.Errorf("log_stream = %q, want %q", entry.Labels["log_stream"], want)
	}
}

// TestEventWithNoLogStreamNameCarriesNoSuchLabel covers the absent key, which
// the alias chain's missing-first-component branch depends on.
func TestEventWithNoLogStreamNameCarriesNoSuchLabel(t *testing.T) {
	entry, err := normalizeEntry(event(1, "", "x"), "/g")
	if err != nil {
		t.Fatalf("normalizeEntry: %v", err)
	}
	if v, present := entry.Labels["log_stream"]; present {
		t.Errorf("log_stream is present as %q; an event with no stream name carries no such label", v)
	}
}

// TestEventWithNoTimestampIsRefused is the deliberate divergence from the
// built-in adapter's wall-clock default. Without it the chunk id would MOVE
// between two collects of the same events and no run could tell why.
func TestEventWithNoTimestampIsRefused(t *testing.T) {
	e := cwtypes.FilteredLogEvent{
		EventId:       aws.String("e-42"),
		Message:       aws.String("x"),
		LogStreamName: aws.String("s1"),
	}
	_, err := normalizeEntry(e, "/g")
	if err == nil {
		t.Fatal("an event with no timestamp was accepted; the built-in adapter's wall-clock default is what this refuses")
	}
	for _, want := range []string{"/g", "s1", "e-42", "timestamp"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// TestParseLogMessageCoversTheFourShapes covers the message shapes the
// specification names, each with its severity.
func TestParseLogMessageCoversTheFourShapes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		line        string
		wantMessage string
		wantSever   string
	}{
		{"json body", `{"level":"error","message":"upstream refused"}`, "upstream refused", SeverityError},
		{"logger-prefixed json", `[app.http] {"level":"warn","message":"slow response"}`, "slow response", SeverityWarn},
		{"severity-prefixed text", "ERROR connection refused", "connection refused", SeverityError},
		{"bracketed severity prefix", "[WARN] disk nearly full", "disk nearly full", SeverityWarn},
		{
			"nested container wrapper",
			`{"log":"{\"level\":\"critical\",\"msg\":\"segfault\"}","stream":"stderr"}`,
			"segfault", SeverityCritical,
		},
		{"plain text with an embedded level", "level=debug starting worker", "level=debug starting worker", SeverityDebug},
		{"plain text with no level at all", "just some text", "just some text", SeverityInfo},
		{"an empty line", "", "", SeverityInfo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg, sev := parseLogMessage(tc.line)
			if msg != tc.wantMessage {
				t.Errorf("message = %q, want %q", msg, tc.wantMessage)
			}
			if sev != tc.wantSever {
				t.Errorf("severity = %q, want %q", sev, tc.wantSever)
			}
		})
	}
}

// TestASeverityPrefixIsRecognizedExactly is the near-miss that keeps every
// short first word from reading as an INFO prefix. ParseSeverity maps anything
// unknown to INFO, so the recognition has to be exact.
func TestASeverityPrefixIsRecognizedExactly(t *testing.T) {
	msg, sev := parseLogMessage("hello there world")
	if msg != "hello there world" {
		t.Errorf("message = %q; the first word is not a severity and must not be stripped", msg)
	}
	if sev != SeverityInfo {
		t.Errorf("severity = %q, want %q", sev, SeverityInfo)
	}
	// The positive half of the pair: a real INFO prefix IS stripped.
	msg, sev = parseLogMessage("INFO started")
	if msg != "started" || sev != SeverityInfo {
		t.Errorf("message %q severity %q, want %q and %q", msg, sev, "started", SeverityInfo)
	}
}

// TestReclassifyOnlyOverwritesTheDefault pins which entries the body-derived
// severity may change. Overwriting a carried non-default level would downgrade
// every wrapped error line.
func TestReclassifyOnlyOverwritesTheDefault(t *testing.T) {
	entries := []LogEntry{
		{Severity: SeverityInfo, Message: "level=error upstream refused"},
		{Severity: "", Message: "level=warn slow"},
		{Severity: SeverityCritical, Message: "level=debug noisy"},
	}
	got := reclassifySeverity(entries)
	if got[0].Severity != SeverityError {
		t.Errorf("an INFO entry with an embedded ERROR stayed %q", got[0].Severity)
	}
	if got[1].Severity != SeverityWarn {
		t.Errorf("an entry with no severity stayed %q", got[1].Severity)
	}
	if got[2].Severity != SeverityCritical {
		t.Errorf("a CRITICAL entry was downgraded to %q by its body", got[2].Severity)
	}
}

// TestNormalizedTimestampIsTheEventsInUTC pins that the entry's instant is the
// event's, converted rather than replaced.
func TestNormalizedTimestampIsTheEventsInUTC(t *testing.T) {
	entry, err := normalizeEntry(event(1772366774000, "s", "x"), "/g")
	if err != nil {
		t.Fatalf("normalizeEntry: %v", err)
	}
	want := time.UnixMilli(1772366774000).UTC()
	if !entry.Timestamp.Equal(want) {
		t.Errorf("timestamp %s, want %s", entry.Timestamp, want)
	}
	if entry.Timestamp.Location() != time.UTC {
		t.Errorf("timestamp is in %s, want UTC", entry.Timestamp.Location())
	}
}
