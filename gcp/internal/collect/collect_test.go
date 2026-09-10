// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// collect_test.go — the three arms every one of the forty enumerations shares,
// asserted once at the seam they share rather than forty times.

func okConverter(_ string, item string) (gcpgraph.Result, error) {
	return gcpgraph.Result{Resources: []gcpgraph.Resource{
		{ID: item, ResourceType: gcpgraph.ResourceTypeDisk, Name: item},
	}}, nil
}

func TestSubcollectorConvertsEveryListedItem(t *testing.T) {
	sub := collect.New("test-enum",
		func(context.Context, string) ([]string, error) { return []string{"a", "b"}, nil },
		okConverter)
	got, err := sub.Run(t.Context(), "proj-a")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got.Resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(got.Resources))
	}
	if sub.Name != "test-enum" {
		t.Errorf("Name: got %q", sub.Name)
	}
}

// An empty page emits nothing and is not an error: it is what a project with
// none of that resource looks like, which is the first real run of any
// collector.
func TestSubcollectorOnAnEmptyPageEmitsNothing(t *testing.T) {
	sub := collect.New("test-enum",
		func(context.Context, string) ([]string, error) { return nil, nil },
		okConverter)
	got, err := sub.Run(t.Context(), "proj-a")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got.Resources) != 0 || len(got.Relations) != 0 {
		t.Errorf("an empty page produced %d resources and %d relations",
			len(got.Resources), len(got.Relations))
	}
}

// The denial arm is NOT here: it has its own file, because "denied" is a third
// outcome rather than a shade of "empty" and the distinction is consequential
// enough to be read on its own. See denied_test.go.

// The same-run control for the case above: every OTHER error IS returned. Without
// it, an implementation that swallowed every error would pass the denial cases.
func TestSubcollectorReturnsEveryOtherListError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"plain", errors.New("the network went away")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := collect.New("test-enum",
				func(context.Context, string) ([]string, error) { return nil, tc.err },
				okConverter)
			_, err := sub.Run(t.Context(), "proj-a")
			if err == nil {
				t.Fatal("the enumeration swallowed a real failure")
			}
			if !strings.Contains(err.Error(), "test-enum") {
				t.Errorf("error %q does not name the enumeration", err)
			}
		})
	}
}

// A malformed API value ERRORS rather than emitting a partial node. A node with
// an id and nothing else is indistinguishable from a resource that genuinely has
// no detail, so the walk says so instead.
func TestSubcollectorReturnsAConverterError(t *testing.T) {
	sub := collect.New("test-enum",
		func(context.Context, string) ([]string, error) { return []string{"a", "bad"}, nil },
		func(_ string, item string) (gcpgraph.Result, error) {
			if item == "bad" {
				return gcpgraph.Result{}, errors.New("no name in the response")
			}
			return okConverter("", item)
		})
	_, err := sub.Run(t.Context(), "proj-a")
	if err == nil {
		t.Fatal("a malformed item was skipped silently")
	}
	if !strings.Contains(err.Error(), "item 1") {
		t.Errorf("error %q does not locate the item it could not convert", err)
	}
}

func TestIsPermissionDeniedIsFalseForNil(t *testing.T) {
	if collect.IsPermissionDenied(nil) {
		t.Error("a nil error was classified as a permission denial")
	}
}
