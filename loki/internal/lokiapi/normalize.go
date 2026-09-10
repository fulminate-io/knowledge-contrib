// SPDX-License-Identifier: Apache-2.0

package lokiapi

import (
	"encoding/json"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/fulminate-io/knowledge-contrib/loki/internal/logpipe"
)

// normalize.go — one Loki value becomes one pipeline entry.
//
// THE SEVERITY PRECEDENCE IS THE REVERSE OF THE ORDER THE CHECKS RUN IN, and
// that inversion is the cell an implementation gets wrong. The stream LABEL is
// read FIRST, but only as the default handed to the line parser, which then
// overrides it: a JSON body's level field wins outright, otherwise a level
// marker embedded in plain text wins, otherwise the label stands, otherwise the
// seed. So the precedence, strongest first, is:
//
//	JSON body field -> embedded plain-text marker -> stream label -> INFO
//
// A stream labeled level=info carrying a line whose JSON says level=error is an
// ERROR entry, and reading the label after the body would make it an INFO one.

// NormalizeEntry converts one Loki (labels, timestamp, line) triple.
//
// EVERY STREAM LABEL IS COPIED ONTO THE ENTRY VERBATIM, with no guard and no
// pipeline-added key. That is what makes the label set the operator's own, and
// it is why all five stream-alias arms are reachable from a Loki collect: the
// labels are whatever their shipper was configured to attach.
func NormalizeEntry(labels map[string]string, tsNanos, line string) logpipe.Entry {
	entry := logpipe.Entry{
		Timestamp: time.Now(),
		Severity:  logpipe.SeverityInfo,
		Labels:    make(map[string]string, len(labels)),
	}
	maps.Copy(entry.Labels, labels)
	if ns, err := strconv.ParseInt(tsNanos, 10, 64); err == nil {
		entry.Timestamp = time.Unix(0, ns)
	}
	if sev := severityFromLabels(labels); sev != "" {
		entry.Severity = sev
	}
	entry.Message, entry.Severity = parseLogLine(line, entry.Severity)
	return entry
}

// severityFromLabels reads the three label keys a shipper may carry a level in.
func severityFromLabels(labels map[string]string) string {
	for _, key := range []string{"level", "severity", "detected_level"} {
		if v, ok := labels[key]; ok && v != "" {
			return logpipe.ParseSeverity(v)
		}
	}
	return ""
}

// severityFromJSON reads the three JSON keys a structured logger may carry a
// level in.
func severityFromJSON(m map[string]any) string {
	for _, key := range []string{"level", "severity", "lvl"} {
		v, ok := m[key]
		if !ok {
			continue
		}
		if s, ok := v.(string); ok && s != "" {
			return logpipe.ParseSeverity(s)
		}
	}
	return ""
}

// parseLogLine extracts the message and the final severity. defaultSev is what
// the stream labels said, and it stands only when the line itself says nothing.
func parseLogLine(line, defaultSev string) (message, severity string) {
	message, severity = line, defaultSev
	if line == "" {
		return message, severity
	}
	trimmed := strings.TrimSpace(line)
	if msg, sev, ok := parseJSONLine(trimmed); ok {
		if msg != trimmed {
			message = msg
		}
		if sev != "" {
			severity = sev
		}
		// A JSON line's own level is final: an embedded marker inside its
		// message text is part of the payload, not a second opinion about the
		// entry's level.
		return message, severity
	}
	if embedded := logpipe.DetectEmbeddedSeverity(message); embedded != "" {
		severity = embedded
	}
	return message, severity
}

// parseJSONLine decodes a structured line. A line that is not a JSON OBJECT is
// not structured: a bare array or a bare number carries no message field, and
// treating it as structured would replace the line with its own re-encoding.
func parseJSONLine(trimmed string) (message, severity string, ok bool) {
	if trimmed == "" || trimmed[0] != '{' {
		return "", "", false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		return "", "", false
	}
	return extractMessage(m, trimmed), severityFromJSON(m), true
}

// extractMessage pulls the human message out of a structured payload, trying
// the field names loggers actually use and falling back to the payload's own
// JSON so a line is never dropped for lacking a known field.
//
// fallback is what the message becomes when even that re-encoding fails. A map
// decoded from JSON re-marshals in a correct build, so reaching that branch
// means the build is not correct — and RETURNING THE EMPTY STRING THERE WOULD
// LOSE THE LOG LINE ENTIRELY, indistinguishable from an entry that really was
// empty. The caller passes the raw text, so the entry degrades to the line as
// it arrived rather than to nothing.
func extractMessage(m map[string]any, fallback string) string {
	for _, key := range []string{"message", "msg", "event", "textPayload", "log", "body"} {
		if s := extractField(m, key); s != "" {
			return s
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return fallback
	}
	return string(b)
}

// extractField reads one field, recursing into a nested object because some
// loggers wrap the message one level down.
func extractField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	if nested, ok := v.(map[string]any); ok {
		// A NESTED payload falls back to NOTHING rather than to the raw line:
		// "no message under this key" is the answer the caller wants here, and
		// it moves on to the next key. The raw-line fallback belongs at the top
		// level, where there is no next key to try.
		return extractMessage(nested, "")
	}
	return ""
}
