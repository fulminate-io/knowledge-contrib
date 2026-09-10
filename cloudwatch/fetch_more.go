// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
)

// fetch_more.go — ASKING WHETHER THE SOURCE HELD MORE, which is the question
// that decides a walk's completeness once its entry bound is spent.
//
// IT IS A DIFFERENT QUESTION FROM "SHOULD I STOP READING", and collapsing the
// two into one comparison is the defect these two probes exist to prevent: a
// bound that happens to equal the source's size stops the read AND leaves
// nothing behind, so the walk is complete. Both probes therefore look at what
// the source actually still holds rather than at whether the budget ran out.

// cursorHoldsAnEntry reports whether the cursor still points at an entry THIS
// COLLECT WOULD HAVE KEPT.
//
// IT APPLIES THE SAME FILTERS THE WALK DOES, which is what makes the answer
// mean what the caller needs: an event the text filter or the severity floor
// excludes is not data the bound cut off, and counting it would report
// truncation for a walk that in fact enumerated everything it wanted.
//
// It follows empty pages, because a paged API may answer with no events and a
// live cursor, and it asks for ONE event at a time: the question is existence,
// and the entries are not collected.
func cursorHoldsAnEntry(
	ctx context.Context,
	client filterLogEventsClient,
	input *cloudwatchlogs.FilterLogEventsInput,
	token *string,
	group string,
	p Params,
) (bool, error) {
	for token != nil {
		probe := *input
		probe.NextToken = token
		probe.Limit = aws.Int32(1)
		page, err := client.FilterLogEvents(ctx, &probe)
		if err != nil {
			return false, fmt.Errorf(
				"cloudwatch: checking whether log group %q held more than the bound allowed: %w", group, err)
		}
		kept, err := keptEntries(page.Events, group, p)
		if err != nil {
			return false, err
		}
		if len(kept) > 0 {
			return true, nil
		}
		token = page.NextToken
	}
	return false, nil
}

// anyGroupHasEntry reports whether any of the named groups holds an entry this
// collect would have kept. It is the between-groups half of the same question
// fetchGroup's cursor probe answers.
func anyGroupHasEntry(
	ctx context.Context,
	client filterLogEventsClient,
	groups []string,
	p Params,
	w window,
) (bool, error) {
	for _, group := range groups {
		input := buildFilterInput(group, p, w, 1)
		token := (*string)(nil)
		for {
			if token != nil {
				input.NextToken = token
			}
			page, err := client.FilterLogEvents(ctx, input)
			if err != nil {
				return false, fmt.Errorf(
					"cloudwatch: checking whether log group %q held more than the bound allowed: %w", group, err)
			}
			kept, err := keptEntries(page.Events, group, p)
			if err != nil {
				return false, err
			}
			if len(kept) > 0 {
				return true, nil
			}
			if page.NextToken == nil {
				break
			}
			token = page.NextToken
		}
	}
	return false, nil
}
