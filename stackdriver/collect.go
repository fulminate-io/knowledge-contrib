// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/logging"
	"cloud.google.com/go/logging/logadmin"
	"google.golang.org/api/iterator"
)

// collect.go — the READ: draining the Cloud Logging entries iterator into
// normalized entries, under a caller-supplied bound.
//
// THE WALK IS SERIAL AND THAT IS NOT A COMPROMISE. The API hands back one
// ordered stream and paginates it server-side; there is no second cursor to
// read in parallel and no way to partition the range without changing which
// entries are returned.

// entryNexter is the minimal surface the drain needs from an entries iterator.
// It exists so the drain — which owns the bound, the truncation assertion and
// the failure posture — is testable against recorded entries without a live
// client and without a network.
type entryNexter interface {
	Next() (*logging.Entry, error)
}

// logadminNexter adapts the SDK's iterator to entryNexter.
type logadminNexter struct{ it *logadmin.EntryIterator }

func (l *logadminNexter) Next() (*logging.Entry, error) { return l.it.Next() }

// drainResult is what one read produced.
type drainResult struct {
	// Entries are the normalized entries, in the order the API returned them.
	Entries []logEntry
	// Truncated reports that the entry bound stopped the read before the
	// iterator was exhausted. It is the value the collect's completeness
	// assertion is built from, and it is a MEASUREMENT rather than a guess: it
	// is set only where the read actually stops early.
	Truncated bool
}

// collectEntries reads the entries matching a query.
func collectEntries(ctx context.Context, client *logadmin.Client, projectID string, q logQuery) (drainResult, error) {
	it := client.Entries(ctx,
		logadmin.Filter(buildFilter(projectID, q)),
		logadmin.NewestFirst(),
	)
	return drainEntries(&logadminNexter{it: it}, projectID, q)
}

// drainEntries walks the iterator to exhaustion or to the bound.
//
// A MID-STREAM FAILURE IS A FAILURE, EVEN AFTER ENTRIES WERE READ. The
// knowledge client's own Cloud Logging adapter flushes what it has and returns
// nil here, so a read that died a third of the way through reports SUCCESS and
// the graph is silently built from a third of the source. This collector's
// result carries a completeness assertion the server acts on — a walk asserting
// completeness lets rows this collect did not carry be treated as gone — so a
// partial read reported as a successful one is a deletion of real data. The
// error names how many entries were read before it, because that is what tells
// an operator whether the failure is at the start of the range or deep inside
// it.
func drainEntries(it entryNexter, projectID string, q logQuery) (drainResult, error) {
	var entries []logEntry
	for {
		if q.MaxEntries > 0 && len(entries) >= q.MaxEntries {
			return drainResult{Entries: entries, Truncated: moreRemains(it, projectID)}, nil
		}
		entry, err := it.Next()
		if err == iterator.Done {
			return drainResult{Entries: entries}, nil
		}
		if err != nil {
			return drainResult{}, fmt.Errorf(
				"stackdriver: reading Cloud Logging entries for project %s failed after %d entries; "+
					"the entries already read are DISCARDED rather than reported as a complete walk: %w",
				projectID, len(entries), err)
		}
		le := normalizeEntry(entry, projectID)
		if !passesClientFilter(le, q) {
			continue
		}
		entries = append(entries, le)
	}
}

// moreRemains reports whether the source still held entries when the bound
// stopped the read.
//
// TRUNCATION IS A MEASUREMENT, NOT AN INFERENCE FROM THE BOUND. A read that took
// exactly as many entries as the bound allowed and had nothing left DID
// enumerate its source, and asserting otherwise disables the server's deletion
// phase for a walk that was complete. Reaching the bound is therefore not the
// question; whether anything is behind it is, and the only way to know is to
// look. The peeked entry is discarded: it is past the bound the caller set.
//
// A READ ERROR WHILE PEEKING ASSERTS TRUNCATION. It is not a collect failure —
// the caller already has every entry it asked for — but it does mean this walk
// cannot claim it saw the whole source, and between the two possible mistakes
// the one that over-reports completeness is the one that deletes data.
func moreRemains(it entryNexter, projectID string) bool {
	_, err := it.Next()
	switch {
	case err == iterator.Done:
		return false
	case err != nil:
		logDiagnostic("project %s: could not tell whether entries remained past the bound, "+
			"so this walk asserts an incomplete read: %v", projectID, err)
		return true
	default:
		return true
	}
}

// passesClientFilter applies the predicates the server-side filter cannot be
// trusted to have applied on its own.
//
// THE SEVERITY CHECK IS NOT REDUNDANT WITH THE FILTER CLAUSE. The filter asked
// the API for entries at or above a severity, but normalizeEntry may then
// RECLASSIFY an entry downward off a level marker in its own message body — so
// an entry that satisfied the server's view of its severity can fail the
// caller's after normalization. Emitting it anyway would return entries the
// caller excluded.
func passesClientFilter(le logEntry, q logQuery) bool {
	if q.TextFilter != "" && !containsFold(le.Message, q.TextFilter) {
		return false
	}
	if q.SeverityMin != "" && !severityAtLeast(le.Severity, q.SeverityMin) {
		return false
	}
	return true
}

// containsFold reports a case-insensitive substring match. The text filter is
// an operator's search term rather than a pattern, so matching it case-sensitively
// would silently exclude the entries they were looking for.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
