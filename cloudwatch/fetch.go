// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// fetch.go — WALKING THE LOG GROUPS, and deciding what the walk may claim about
// its own completeness.

// pageLimit is the per-request event count. AWS documents 10,000 as the maximum
// and the default, and it is used purely as a BATCHING unit: it is not a
// ceiling on the collect, which pages on until the cursor is exhausted.
const pageLimit = 10000

// filterLogEventsClient is the one method this collector calls. Declaring the
// narrow interface rather than depending on the concrete SDK client is what
// lets a test substitute a fake with recorded pages and no network — the
// concrete *cloudwatchlogs.Client satisfies it as it stands.
type filterLogEventsClient interface {
	FilterLogEvents(
		ctx context.Context,
		params *cloudwatchlogs.FilterLogEventsInput,
		optFns ...func(*cloudwatchlogs.Options),
	) (*cloudwatchlogs.FilterLogEventsOutput, error)
}

// fetchResult is one walk's events and its completeness.
type fetchResult struct {
	// Entries are the normalized events, in the order CloudWatch returned
	// them.
	Entries []LogEntry
	// Truncated reports that a BOUND cut the walk short, so the source held
	// entries this walk did not enumerate.
	Truncated bool
	// Reason names the bound, and is empty when the walk was complete. It is
	// what the incomplete assertion carries.
	Reason string
}

// fetchAll walks every named log group in order and returns their entries.
//
// WHAT MAKES A WALK INCOMPLETE IS DATA THE SOURCE STILL HELD, never a bound
// having been spent. The two are different questions and collapsing them into
// one comparison is the defect this shape exists to prevent: a bound that
// happens to equal the source's size stops the read AND leaves nothing behind,
// so the walk enumerated its whole source and is COMPLETE. Reporting otherwise
// would disable the deletion phase permanently for any operator whose bound
// sits at or a little above a steady-state group's size, and the graph would
// accumulate rows no collect could ever retire.
//
// A NARROW TIME WINDOW DOES NOT make a walk incomplete either: the requested
// window IS the source for this collect, so enumerating all of it is complete.
func fetchAll(ctx context.Context, client filterLogEventsClient, p Params, w window) (fetchResult, error) {
	// bound is the resolved caller bound, zero meaning unbounded. It is read
	// once here rather than per group so the whole walk shares one budget.
	bound := p.entryBound()

	var out fetchResult
	for i, group := range p.LogGroups {
		if bound > 0 && len(out.Entries) >= bound {
			// The budget is spent before this group was read. Whether that
			// TRUNCATED anything depends on whether the groups left hold an
			// entry this collect would have kept, so ask rather than assume.
			more, err := anyGroupHasEntry(ctx, client, p.LogGroups[i:], p, w)
			if err != nil {
				return fetchResult{}, err
			}
			if more {
				out.Truncated = true
				out.Reason = fmt.Sprintf(
					"the max_entries bound of %d was spent before log group %q was read, and it holds entries",
					bound, group)
			}
			return out, nil
		}
		remaining := 0
		if bound > 0 {
			remaining = bound - len(out.Entries)
		}
		groupResult, err := fetchGroup(ctx, client, group, p, w, remaining)
		if err != nil {
			return fetchResult{}, err
		}
		out.Entries = append(out.Entries, groupResult.Entries...)
		if groupResult.Truncated {
			out.Truncated = true
			out.Reason = groupResult.Reason
			return out, nil
		}
	}
	return out, nil
}

// fetchGroup pages one log group, stopping when the budget is spent and then
// settling whether anything was left behind.
//
// A PAGE ERROR FAILS THE WHOLE COLLECT, including one on a later page after
// entries have already been read. Returning the partial result instead is a
// silent degrade, and here it would be worse than silent: a partial page set
// asserted as a complete walk is what tells the deletion phase that every row
// this collect did not carry is gone.
func fetchGroup(
	ctx context.Context,
	client filterLogEventsClient,
	group string,
	p Params,
	w window,
	remaining int,
) (fetchResult, error) {
	input := buildFilterInput(group, p, w, remaining)
	var out fetchResult
	for {
		// ctx rides every call, so a cancelled collect stops paging rather
		// than draining the rest of the group. The SDK's standard retryer
		// already handles throttling and 5xx with backoff, so there is no
		// retry loop here.
		page, err := client.FilterLogEvents(ctx, input)
		if err != nil {
			return fetchResult{}, fmt.Errorf(
				"cloudwatch: reading log group %q failed after %d entries: %w", group, len(out.Entries), err)
		}
		kept, err := keptEntries(page.Events, group, p)
		if err != nil {
			return fetchResult{}, err
		}

		if remaining > 0 && len(out.Entries)+len(kept) >= remaining {
			return closeAtBudget(ctx, client, input, page.NextToken, out, kept, remaining, group, p)
		}

		out.Entries = append(out.Entries, kept...)
		if page.NextToken == nil {
			return out, nil
		}
		input.NextToken = page.NextToken
	}
}

// closeAtBudget ends the walk of one group at the max_entries bound: it keeps
// what still fits and records whether the group held more.
//
// MORE DATA IS OBSERVED, NOT INFERRED, in two places: entries this page held
// past the budget, and, when the budget landed exactly on a page boundary,
// whatever the cursor still points at.
func closeAtBudget(
	ctx context.Context,
	client filterLogEventsClient,
	input *cloudwatchlogs.FilterLogEventsInput,
	nextToken *string,
	out fetchResult,
	kept []LogEntry,
	remaining int,
	group string,
	p Params,
) (fetchResult, error) {
	take := remaining - len(out.Entries)
	out.Entries = append(out.Entries, kept[:take]...)

	more := len(kept) > take
	if !more {
		var err error
		more, err = cursorHoldsAnEntry(ctx, client, input, nextToken, group, p)
		if err != nil {
			return fetchResult{}, err
		}
	}
	if more {
		out.Truncated = true
		out.Reason = fmt.Sprintf(
			"the max_entries bound of %d was reached while walking log group %q, which held more",
			p.entryBound(), group)
	}
	return out, nil
}

// keptEntries normalizes one page and drops the entries this collect's filters
// exclude.
func keptEntries(events []cwtypes.FilteredLogEvent, group string, p Params) ([]LogEntry, error) {
	out := make([]LogEntry, 0, len(events))
	for i := range events {
		entry, err := normalizeEntry(events[i], group)
		if err != nil {
			return nil, err
		}
		if keepEntry(entry, p) {
			out = append(out, entry)
		}
	}
	return out, nil
}

// keepEntry applies the two filters CloudWatch cannot: a case-insensitive
// substring over the NORMALIZED message, and a severity floor, which CloudWatch
// cannot express because it has no ordering over severity levels.
func keepEntry(entry LogEntry, p Params) bool {
	if p.TextFilter != "" && !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(p.TextFilter)) {
		return false
	}
	if p.SeverityMin != "" && !SeverityAtLeast(entry.Severity, p.SeverityMin) {
		return false
	}
	return true
}

// buildFilterInput builds the first request for one group.
//
// The per-page limit SHRINKS ONLY, and never grows: it drops to the caller's
// remaining bound when that is smaller than a page, so a small collect makes one
// small request instead of pulling ten thousand events to keep a handful, and
// otherwise it stays at the batching unit while pagination carries the walk as
// far as the caller asked. A large bound is therefore honored rather than
// clamped — this collector imposes no ceiling of its own.
//
// THE COMPARISON HAPPENS IN int SPACE, BEFORE THE NARROWING CONVERSION, and
// that is the whole reason it is written this way. The request's limit field is
// 32 bits wide; converting a bound past that width first WRAPS IT NEGATIVE, and
// a negative page limit is refused by the provider. A caller naming three
// billion entries would then get an error about a malformed request rather than
// the walk they asked for.
func buildFilterInput(group string, p Params, w window, remaining int) *cloudwatchlogs.FilterLogEventsInput {
	limit := int32(pageLimit)
	if remaining > 0 && remaining < pageLimit {
		limit = int32(remaining)
	}
	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(group),
		Limit:        aws.Int32(limit),
	}
	if !w.Start.IsZero() {
		input.StartTime = aws.Int64(w.Start.UnixMilli())
	}
	if !w.End.IsZero() {
		input.EndTime = aws.Int64(w.End.UnixMilli())
	}
	if p.FilterPattern != "" {
		input.FilterPattern = aws.String(p.FilterPattern)
	}
	return input
}
