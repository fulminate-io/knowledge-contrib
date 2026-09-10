// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// normalize.go — TURNING A CLOUDWATCH EVENT INTO A LOG ENTRY: the three labels
// and the message-and-severity parse.

// streamNameLimit and streamNameKeep bound the log_stream label: a name longer
// than the limit is cut to streamNameKeep characters plus an ellipsis. The
// label is hashed into the stream id, so the truncated form IS the identity —
// two streams whose names differ only past the limit are one stream.
const (
	streamNameLimit = 60
	streamNameKeep  = 57
)

// severityPrefixMaxLen bounds how far into a line a leading severity token may
// end. Past it the first word is prose, not a level.
const severityPrefixMaxLen = 8

// maxJSONNesting bounds the container-wrapper unwrapping. A platform that wraps
// a JSON application log in a JSON envelope nests two or three deep; the bound
// is what stops a crafted payload from recursing without end.
const maxJSONNesting = 3

// normalizeEntry converts one CloudWatch event into a log entry.
//
// AN EVENT WITH NO TIMESTAMP IS AN ERROR, and this is a deliberate divergence
// from the built-in adapter, which substitutes the current wall clock. The
// timestamp is floored into a chunk window and hashed into that chunk's id, so
// a wall-clock substitute makes the chunk id MOVE between two collects of the
// same events — the graph would look entirely rewritten every time, and nothing
// would report why. Refusing is this repository's bad-input rule applied where
// the alternative is a silent corruption of identity.
func normalizeEntry(event cwtypes.FilteredLogEvent, logGroupName string) (LogEntry, error) {
	if event.Timestamp == nil {
		return LogEntry{}, fmt.Errorf(
			"cloudwatch: log group %q returned an event with no timestamp (stream %q, event id %q); "+
				"the timestamp determines which chunk the entry lands in and is part of that chunk's identity, "+
				"so it cannot be defaulted",
			logGroupName, aws.ToString(event.LogStreamName), aws.ToString(event.EventId))
	}

	entry := LogEntry{
		Timestamp: time.UnixMilli(*event.Timestamp).UTC(),
		Labels: map[string]string{
			"log_group": logGroupName,
			"service":   extractService(logGroupName),
		},
	}
	if event.LogStreamName != nil {
		entry.Labels["log_stream"] = truncateStreamName(*event.LogStreamName)
	}
	entry.Message, entry.Severity = parseLogMessage(aws.ToString(event.Message))
	return entry, nil
}

// truncateStreamName applies the length bound described on streamNameLimit.
func truncateStreamName(name string) string {
	if len(name) > streamNameLimit {
		return name[:streamNameKeep] + "..."
	}
	return name
}

// extractService derives the service label from a log group path: its LAST path
// segment, so /ecs/prod/api-server yields api-server.
//
// EVERY STREAM IN ONE LOG GROUP THEREFORE SHARES ONE SERVICE LABEL. That is
// what makes the label resolvable to a single cloud resource, and it is also
// why the service label alone never distinguishes two streams — the log_stream
// label does that.
func extractService(logGroup string) string {
	parts := strings.Split(strings.TrimPrefix(logGroup, "/"), "/")
	if len(parts) == 0 {
		return logGroup
	}
	return parts[len(parts)-1]
}

// parseLogMessage unpeels the wrappers a CloudWatch line arrives in and returns
// the message text and its severity.
//
// FOUR SHAPES ARE HANDLED, in this order: a bare JSON object; a logger-prefixed
// JSON object; a severity-prefixed line, whose remainder may itself be any of
// the others; and plain text, whose body is searched for an embedded level.
func parseLogMessage(line string) (message, severity string) {
	message = strings.TrimSpace(line)
	severity = SeverityInfo
	if message == "" {
		return message, severity
	}

	jsonBody := message
	if message[0] == '[' {
		if _, after, found := strings.Cut(message, "] "); found {
			rest := strings.TrimSpace(after)
			if len(rest) > 0 && rest[0] == '{' {
				jsonBody = rest
			}
		}
	}
	if jsonBody[0] == '{' {
		if msg, sev, ok := parseJSON(jsonBody, 0); ok {
			return msg, sev
		}
	}
	if msg, sev, ok := parseSeverityPrefix(message); ok {
		return msg, sev
	}
	if embedded := DetectEmbeddedSeverity(message); embedded != "" {
		severity = embedded
	}
	return message, severity
}

// parseSeverityPrefix reads a leading severity token, returning ok=false when
// the first word is not one.
//
// The recognition test is exact rather than lenient: ParseSeverity maps
// anything unknown to INFO, so a bare INFO result is accepted only when the
// token really did spell INFO. Without that check every line whose first word
// is short would be read as an INFO-prefixed line.
func parseSeverityPrefix(msg string) (string, string, bool) {
	idx := strings.IndexByte(msg, ' ')
	if idx <= 0 || idx > severityPrefixMaxLen {
		return "", "", false
	}
	prefix := strings.Trim(msg[:idx], "[]")
	sev := ParseSeverity(prefix)
	if sev == SeverityInfo && !strings.EqualFold(prefix, "INFO") {
		return "", "", false
	}
	rest := strings.TrimSpace(msg[idx+1:])
	if rest == "" {
		return "", "", false
	}
	rest = stripLoggerPrefix(rest)
	if rest[0] == '{' {
		if inner, innerSev, ok := parseJSON(rest, 0); ok {
			if innerSev != SeverityInfo {
				sev = innerSev
			}
			return inner, sev, true
		}
	}
	return rest, sev, true
}

// stripLoggerPrefix removes a leading "[logger.name] " when what follows is a
// JSON object. It is conditioned on the JSON because a bracketed prefix in
// front of prose is part of the message.
func stripLoggerPrefix(s string) string {
	if len(s) == 0 || s[0] != '[' {
		return s
	}
	_, tail, ok := strings.Cut(s, "] ")
	if !ok {
		return s
	}
	after := strings.TrimSpace(tail)
	if len(after) > 0 && after[0] == '{' {
		return after
	}
	return s
}

// parseJSON decodes a structured line and extracts its message and severity,
// then unwraps a nested container payload. It gives up past maxJSONNesting.
func parseJSON(line string, depth int) (string, string, bool) {
	if depth > maxJSONNesting {
		return line, SeverityInfo, false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return "", "", false
	}
	msg := extractMessageFromMap(m)
	sev := extractSeverityFromMap(m)
	msg, sev = unwrapNested(msg, sev, depth+1)
	return msg, sev, true
}

// unwrapNested handles a container wrapper whose extracted message is itself a
// structured line, with or without its own severity and logger prefixes.
//
// AN INNER SEVERITY WINS ONLY WHEN IT IS NOT THE DEFAULT. An inner payload that
// simply failed to declare a level parses as INFO, and letting that overwrite
// an outer ERROR would downgrade every wrapped error line.
func unwrapNested(msg, sev string, depth int) (string, string) {
	if len(msg) == 0 {
		return msg, sev
	}
	if msg[0] == '{' {
		if inner, innerSev, ok := parseJSON(msg, depth); ok {
			if innerSev != SeverityInfo {
				sev = innerSev
			}
			return inner, sev
		}
		return msg, sev
	}
	rest := msg
	if idx := strings.IndexByte(rest, ' '); idx > 0 && idx <= severityPrefixMaxLen {
		prefix := strings.Trim(rest[:idx], "[]")
		if prefixSev := ParseSeverity(prefix); prefixSev != SeverityInfo || strings.EqualFold(prefix, "INFO") {
			if prefixSev != SeverityInfo {
				sev = prefixSev
			}
			rest = strings.TrimSpace(rest[idx+1:])
		}
	}
	rest = stripLoggerPrefix(rest)
	if len(rest) > 0 && rest[0] == '{' {
		if inner, innerSev, ok := parseJSON(rest, depth); ok {
			if innerSev != SeverityInfo {
				sev = innerSev
			}
			return inner, sev
		}
	}
	return msg, sev
}

// extractSeverityFromMap reads a level out of a decoded payload, checking the
// conventional field names and then the nested message objects a container
// wrapper produces.
func extractSeverityFromMap(m map[string]any) string {
	for _, key := range []string{"level", "severity", "lvl", "log_level"} {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return ParseSeverity(s)
			}
		}
	}
	for _, key := range []string{"message", "msg", "log"} {
		if v, ok := m[key]; ok {
			if nested, ok := v.(map[string]any); ok {
				if sev := extractSeverityFromMap(nested); sev != SeverityInfo {
					return sev
				}
			}
		}
	}
	return SeverityInfo
}
