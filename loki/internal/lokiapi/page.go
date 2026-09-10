// SPDX-License-Identifier: Apache-2.0

package lokiapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fulminate-io/knowledge-contrib/loki/internal/logpipe"
)

// page.go — the backward time-narrowing walk over query_range.
//
// LOKI'S query_range HAS NO CURSOR, so paging is done by shrinking the window:
// each request asks for the newest pageLimit entries in [start, end] with
// direction=backward, and the next one sets end to one nanosecond before the
// oldest entry the last RAW page held.
//
// TERMINATION IS ON THE RAW PAGE COUNT, NEVER THE FILTERED ONE. The filters
// this collector applies on its own side — severity, and a case-insensitive
// text match Loki's line filter cannot express — drop entries after Loki has
// counted them, so a severity-filtered query against a busy stream can return
// 47 entries from a full 5000-entry page. Terminating on 47 < 5000 would report
// a complete walk over a window whose remainder was never queried.

// pageLimit is the per-request BATCHING unit, chosen to match Loki's own
// default server-side maximum so one request transfers as much as the server
// will give. IT IS NOT A CEILING ON THE WALK: the walk pages until the window
// is exhausted, and this collector imposes no bound on how many entries a
// collect may return.
const pageLimit = 5000

// Result is one walk's outcome.
type Result struct {
	// Entries are every entry the walk read, in the order the pages returned
	// them: newest first within a page, and pages walking backwards in time.
	Entries []logpipe.Entry
	// Complete is false when the walk could not enumerate the whole window.
	Complete bool
	// Incomplete is why, and is empty when Complete is true. It is the reason a
	// collect's completeness assertion carries.
	Incomplete string
}

// Walk reads the whole window, paging backwards.
//
// A PAGE FAILURE FAILS THE WALK. The built-in adapter returns success after a
// mid-walk failure when earlier pages produced entries, which reports a
// complete walk over a truncated window and lets the deletion phase treat every
// unreached entry as gone. That is a silent degrade and this collector does not
// copy it: a partial read is either asserted incomplete, below, or it is an
// error.
func (c *Client) Walk(ctx context.Context, q Query, start, end time.Time) (Result, error) {
	if !start.Before(end) {
		return Result{}, fmt.Errorf("lokiapi: the collect window starts at %s and ends at %s; the start must be before the end",
			start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	}
	logQL := BuildLogQL(q)
	startNanos, endNanos := start.UnixNano(), end.UnixNano()

	var entries []logpipe.Entry
	for {
		page, err := c.fetchPage(ctx, logQL, q, startNanos, endNanos)
		if err != nil {
			return Result{}, err
		}
		entries = append(entries, page.entries...)
		if page.rawCount < pageLimit {
			// Loki had no more entries in this window.
			return Result{Entries: entries, Complete: true}, nil
		}

		// A FULL PAGE WHOSE ENTRIES ALL SHARE ONE TIMESTAMP IS THE END OF WHAT A
		// CURSORLESS PROTOCOL CAN READ, and this is the arm the built-in adapter
		// gets wrong. The only way to ask for older entries is to move the end
		// bound below the oldest one returned — but here that is also below the
		// NEWEST one returned, so any entries Loki still holds AT THAT SAME
		// INSTANT beyond this page are stepped over, and no narrower request can
		// reach them.
		//
		// The built-in adapter narrows past the instant and reports a complete
		// walk, which would let the deletion phase treat every skipped entry as
		// gone. This collector asserts INCOMPLETE and names the instant.
		if page.rawOldestNanos == page.rawNewestNanos {
			return Result{
				Entries:  entries,
				Complete: false,
				Incomplete: fmt.Sprintf(
					"a full page of %d entries all carry the timestamp %s, so narrowing the window past that instant would step over any further entries at it; "+
						"query_range has no cursor, so the remainder at that instant is unreachable — collect that instant as its own window to read it",
					pageLimit, time.Unix(0, page.rawOldestNanos).UTC().Format(time.RFC3339Nano)),
			}, nil
		}

		nextEnd := page.rawOldestNanos - 1
		if nextEnd < startNanos {
			// The page reached the start of the window.
			return Result{Entries: entries, Complete: true}, nil
		}
		if nextEnd >= endNanos {
			// THE WALK MADE NO PROGRESS, which means the server returned entries
			// NEWER than the end bound the request carried. That is the server
			// contradicting the request rather than a window this collector can
			// narrow, so the walk stops rather than looping, and it reports what
			// it saw instead of a complete read.
			return Result{
				Entries:  entries,
				Complete: false,
				Incomplete: fmt.Sprintf(
					"the walk could not make progress: the page's oldest entry is at %s, at or after the end bound %s the request carried, "+
						"so the next request would repeat this one",
					time.Unix(0, page.rawOldestNanos).UTC().Format(time.RFC3339Nano),
					time.Unix(0, endNanos).UTC().Format(time.RFC3339Nano)),
			}, nil
		}
		endNanos = nextEnd
	}
}

// pageResult carries the filtered slice a caller keeps beside the RAW metadata
// termination and narrowing need. Splitting the two is the whole point.
type pageResult struct {
	entries  []logpipe.Entry
	rawCount int
	// rawOldestNanos and rawNewestNanos bound the RAW page. Both are needed:
	// the oldest drives the narrowing, and the two being EQUAL on a full page
	// is what says the page is one instant wide and cannot be narrowed past.
	rawOldestNanos int64
	rawNewestNanos int64
}

// fetchPage issues one query_range request.
func (c *Client) fetchPage(ctx context.Context, logQL string, q Query, startNanos, endNanos int64) (pageResult, error) {
	params := url.Values{
		"query":     {logQL},
		"limit":     {strconv.Itoa(pageLimit)},
		"direction": {"backward"},
		"start":     {strconv.FormatInt(startNanos, 10)},
		"end":       {strconv.FormatInt(endNanos, 10)},
	}
	body, err := c.get(ctx, "/loki/api/v1/query_range", params)
	if err != nil {
		return pageResult{}, err
	}

	var resp queryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return pageResult{}, fmt.Errorf("lokiapi: %s answered query_range with a body that is not the expected JSON: %w", c.address, err)
	}
	if resp.Status != "success" {
		return pageResult{}, fmt.Errorf("lokiapi: %s answered query_range with status %q, not \"success\"", c.address, resp.Status)
	}

	raw, filtered, err := normalizePage(resp.Data.Result, q)
	if err != nil {
		return pageResult{}, err
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Timestamp.After(filtered[j].Timestamp) })

	out := pageResult{entries: filtered, rawCount: len(raw)}
	if len(raw) > 0 {
		oldest, newest := raw[0].Timestamp.UnixNano(), raw[0].Timestamp.UnixNano()
		for _, e := range raw[1:] {
			switch t := e.Timestamp.UnixNano(); {
			case t < oldest:
				oldest = t
			case t > newest:
				newest = t
			}
		}
		out.rawOldestNanos, out.rawNewestNanos = oldest, newest
	}
	return out, nil
}

// normalizePage converts one response's streams, returning the raw entries and
// the filtered ones.
//
// A VALUE THAT IS NOT A [timestamp, line] PAIR IS AN ERROR, not a skipped row:
// the shape is the documented one, and silently dropping a malformed value
// would make a Loki that changed its response shape look like a Loki with
// fewer entries.
func normalizePage(streams []stream, q Query) (raw, filtered []logpipe.Entry, err error) {
	for si, s := range streams {
		for vi, val := range s.Values {
			if len(val) < 2 {
				return nil, nil, fmt.Errorf(
					"lokiapi: query_range returned stream %d value %d with %d fields; a value is a [timestamp, line] pair",
					si, vi, len(val))
			}
			entry := NormalizeEntry(s.Stream, val[0], val[1])
			raw = append(raw, entry)
			if q.TextFilter != "" && !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(q.TextFilter)) {
				continue
			}
			if q.SeverityMin != "" && !logpipe.SeverityAtLeast(entry.Severity, q.SeverityMin) {
				continue
			}
			filtered = append(filtered, entry)
		}
	}
	return raw, filtered, nil
}

// The query_range response shape.
type queryResponse struct {
	Status string    `json:"status"`
	Data   queryData `json:"data"`
}

type queryData struct {
	ResultType string   `json:"resultType"`
	Result     []stream `json:"result"`
}

type stream struct {
	Stream map[string]string `json:"stream"`
	// Values is a list of [nanosecond timestamp, line] pairs.
	Values [][]string `json:"values"`
}
