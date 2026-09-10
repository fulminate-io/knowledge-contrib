// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// fixture_test.go — THE RECORDED RESPONSES EVERY WALK-LEVEL TEST DRIVES, and
// the fake client that serves them.
//
// NOTHING HERE REACHES AWS. The fake satisfies the same one-method interface
// the concrete SDK client satisfies, so the substitution is at the seam the
// production code already declares rather than at a test-only fork of it.

// fixtureFile is the recorded response set. It lives under this module's own
// testdata, so the go tool records it in the test cache key natively and an
// edit to it invalidates a stored pass without any help.
const fixtureFile = "testdata/events_parity.json"

// recordedEvent is one recorded CloudWatch event. Timestamp is a POINTER so a
// fixture can record an event that carries none, which is the shape the
// missing-timestamp refusal is driven with.
type recordedEvent struct {
	EventID       string `json:"event_id"`
	Timestamp     *int64 `json:"timestamp"`
	LogStreamName string `json:"log_stream_name"`
	Message       string `json:"message"`
}

// recordedPage is one FilterLogEvents response.
type recordedPage struct {
	NextToken string          `json:"next_token"`
	Events    []recordedEvent `json:"events"`
}

// recordedGroup is one log group's recorded responses.
type recordedGroup struct {
	LogGroup string         `json:"log_group"`
	Pages    []recordedPage `json:"pages"`
}

// recordedFixture is the decoded fixture file.
type recordedFixture struct {
	Groups       []recordedGroup          `json:"groups"`
	CloudContext framework.ForeignContext `json:"cloud_context"`
	Resolutions  []struct {
		LabelKey   string `json:"label_key"`
		LabelValue string `json:"label_value"`
		Account    string `json:"account"`
		ResourceID string `json:"resource_id"`
	} `json:"resolutions"`
}

// loadFixture decodes the recorded fixture.
func loadFixture(t *testing.T) recordedFixture {
	t.Helper()
	raw, err := os.ReadFile(fixtureFile)
	if err != nil {
		t.Fatalf("reading the recorded fixture: %v", err)
	}
	var f recordedFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decoding the recorded fixture: %v", err)
	}
	if len(f.Groups) == 0 {
		t.Fatalf("the recorded fixture names no log group; every walk test would pass vacuously")
	}
	return f
}

// fixtureParams is the collect the recorded fixture is walked with.
func (f recordedFixture) params() Params {
	groups := make([]string, 0, len(f.Groups))
	for _, g := range f.Groups {
		groups = append(groups, g.LogGroup)
	}
	return Params{LogGroups: groups}
}

// declaredBlock is the fixture's cloud block, in the framework's own shape, as
// the client would hand it to a walk. Decoding it into the framework's own type
// rather than a local one is deliberate: a change to that type's wire names
// breaks this fixture rather than silently producing an empty block.
func (f recordedFixture) declaredBlock() framework.ForeignContext { return f.CloudContext }

// resolutions is the RESOLUTIONS the fixture records as expected. They are the
// answer the production route must arrive at from the declared block above, not
// an input handed to the walk.
func (f recordedFixture) resolutions() []ResolvedProxy {
	out := make([]ResolvedProxy, 0, len(f.Resolutions))
	for _, r := range f.Resolutions {
		out = append(out, ResolvedProxy{
			LabelKey: r.LabelKey, LabelValue: r.LabelValue,
			Account: r.Account, ResourceID: r.ResourceID,
		})
	}
	return out
}

// fakeFilterClient serves recorded pages per log group.
//
// IT FAILS THE TEST ON AN UNEXPECTED CALL rather than returning an empty page.
// An empty page ends pagination, so a client that quietly answered a group it
// had no recording for would turn a routing bug into a passing test with a
// smaller graph.
type fakeFilterClient struct {
	t *testing.T
	// pages maps a log group to its recorded pages.
	pages map[string][]*cloudwatchlogs.FilterLogEventsOutput
	// cursor tracks how many pages of each group have been served.
	cursor map[string]int
	// calls counts every request, which is what a pagination assertion reads.
	calls int
	// err, when set, is returned instead of the page at index errAtCall.
	err       error
	errAtCall int
	// limits records the per-request Limit values, so a test can assert the
	// request shape rather than only its result.
	limits []int32
	// capture, when set, records each request whole. It is off by default so
	// the ordinary walk tests hold no references to request structures they do
	// not read.
	capture bool
	inputs  []*cloudwatchlogs.FilterLogEventsInput
}

// newFakeClient builds a fake serving the fixture's recorded pages.
func newFakeClient(t *testing.T, f recordedFixture) *fakeFilterClient {
	t.Helper()
	return newFakeClientFor(t, f.Groups)
}

// newFakeClientFor builds a fake over any recorded group set, so a test can
// supply its own pages instead of the checked-in fixture's.
func newFakeClientFor(t *testing.T, groups []recordedGroup) *fakeFilterClient {
	t.Helper()
	pages := make(map[string][]*cloudwatchlogs.FilterLogEventsOutput, len(groups))
	for _, g := range groups {
		for _, p := range g.Pages {
			out := &cloudwatchlogs.FilterLogEventsOutput{Events: buildEvents(p.Events)}
			if p.NextToken != "" {
				out.NextToken = aws.String(p.NextToken)
			}
			pages[g.LogGroup] = append(pages[g.LogGroup], out)
		}
	}
	return &fakeFilterClient{t: t, pages: pages, cursor: make(map[string]int), errAtCall: -1}
}

// buildEvents converts recorded events into the SDK's own type.
func buildEvents(in []recordedEvent) []cwtypes.FilteredLogEvent {
	out := make([]cwtypes.FilteredLogEvent, 0, len(in))
	for _, e := range in {
		event := cwtypes.FilteredLogEvent{
			EventId:   aws.String(e.EventID),
			Timestamp: e.Timestamp,
			Message:   aws.String(e.Message),
		}
		if e.LogStreamName != "" {
			event.LogStreamName = aws.String(e.LogStreamName)
		}
		out = append(out, event)
	}
	return out
}

// FilterLogEvents serves the next recorded page for the requested group.
func (f *fakeFilterClient) FilterLogEvents(
	_ context.Context,
	params *cloudwatchlogs.FilterLogEventsInput,
	_ ...func(*cloudwatchlogs.Options),
) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	f.t.Helper()
	f.calls++
	if params.Limit != nil {
		f.limits = append(f.limits, *params.Limit)
	}
	if f.capture {
		f.inputs = append(f.inputs, params)
	}
	if f.err != nil && f.calls == f.errAtCall {
		return nil, f.err
	}
	group := aws.ToString(params.LogGroupName)
	pages, ok := f.pages[group]
	if !ok {
		f.t.Fatalf("the walk asked for log group %q, which the fixture does not record", group)
	}
	idx := f.cursor[group]
	if idx >= len(pages) {
		f.t.Fatalf("the walk asked log group %q for page %d, past the %d the fixture records; "+
			"pagination did not stop when the cursor was exhausted", group, idx+1, len(pages))
	}
	f.cursor[group]++
	return pages[idx], nil
}

// collectorWithFake builds a collector whose client is the fake.
func collectorWithFake(client filterLogEventsClient) *Collector {
	c := New()
	c.newClient = func(context.Context, string) (filterLogEventsClient, error) { return client, nil }
	return c
}
