// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// TestFanOutReportsFailuresInEnumerationOrder pins what an operator reads in
// an incomplete walk's message: the failures are named in the order the
// enumerations were declared, not in the order their workers happened to
// finish. The first enumeration is held until the second has returned, so a
// fan-out that recorded results as workers finished would name the second
// first.
func TestFanOutReportsFailuresInEnumerationOrder(t *testing.T) {
	secondReturned := make(chan struct{})
	subs := []collect.Subcollector{
		{Name: "first", Run: func(ctx context.Context, _ string) (gcpgraph.Result, error) {
			select {
			case <-secondReturned:
			case <-ctx.Done():
			}
			return gcpgraph.Result{}, errors.New("first failed")
		}},
		{Name: "second", Run: func(context.Context, string) (gcpgraph.Result, error) {
			defer close(secondReturned)
			return gcpgraph.Result{}, errors.New("second failed")
		}},
	}
	_, _, failures := Collector{Concurrency: 2}.fanOut(t.Context(), "proj", subs)
	want := []string{"first: first failed", "second: second failed"}
	if !cmp.Equal(failures, want) {
		t.Fatalf("failures = %q, want %q", failures, want)
	}
}
