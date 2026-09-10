// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// denied_test.go — A DENIED ENUMERATION IS NOT AN EMPTY ONE.
//
// The distinction is the whole subject of this file, and getting it wrong is a
// data-loss defect rather than a cosmetic one.
//
// "This project has no caches" and "I was not allowed to look for caches" are
// both an empty list. If the walk cannot tell them apart it asserts a COMPLETE
// walk in both cases, and a complete walk is what lets the receiving server
// treat everything the walk did not carry as gone. So a role revoked between two
// collects would delete a whole service's resources from the graph, and the
// second collect would report success.
//
// The denial therefore travels back as a distinguishable value: the enumeration
// does not fail — every other enumeration still runs and the collect still
// succeeds — but the walk that contains it is INCOMPLETE and says which
// enumeration was refused.

func TestADeniedEnumerationIsReportedAsDeniedNotAsEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"grpc permission denied", status.Error(codes.PermissionDenied, "denied")},
		{"grpc unauthenticated", status.Error(codes.Unauthenticated, "no credential")},
		{"rest 403", &googleapi.Error{Code: 403, Message: "forbidden"}},
		{"rest 401", &googleapi.Error{Code: 401, Message: "unauthorized"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := collect.New("test-enum",
				func(context.Context, string) ([]string, error) { return nil, tc.err },
				okConverter)
			got, err := sub.Run(t.Context(), "proj-a")

			if err == nil {
				t.Fatal("a denied enumeration returned no signal at all; it is indistinguishable " +
					"from a project that genuinely has none of that resource")
			}
			if !errors.Is(err, collect.ErrDenied) {
				t.Fatalf("a denied enumeration returned %v, which does not classify as a denial", err)
			}
			if !strings.Contains(err.Error(), "test-enum") {
				t.Errorf("the denial does not name the enumeration it refused: %v", err)
			}
			// It carries no partial result: the API returned nothing.
			if len(got.Resources) != 0 {
				t.Errorf("a denied enumeration produced %d resources", len(got.Resources))
			}
		})
	}
}

// The same-run control, and the distinction this file exists for: an enumeration
// that genuinely found nothing returns NO error, so the walk stays complete.
func TestAnEmptyEnumerationIsNotADenial(t *testing.T) {
	sub := collect.New("test-enum",
		func(context.Context, string) ([]string, error) { return nil, nil },
		okConverter)
	got, err := sub.Run(t.Context(), "proj-a")
	if err != nil {
		t.Fatalf("an enumeration that found nothing reported %v", err)
	}
	if len(got.Resources) != 0 {
		t.Errorf("an empty enumeration produced %d resources", len(got.Resources))
	}
}

// A real failure is neither: it must NOT classify as a denial, or a transport
// outage would be reported as a permissions problem and an operator would go
// looking at roles.
func TestARealFailureDoesNotClassifyAsADenial(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"plain", errors.New("the network went away")},
		{"grpc unavailable", status.Error(codes.Unavailable, "backend down")},
		{"rest 500", &googleapi.Error{Code: 500}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := collect.New("test-enum",
				func(context.Context, string) ([]string, error) { return nil, tc.err },
				okConverter)
			_, err := sub.Run(t.Context(), "proj-a")
			if err == nil {
				t.Fatal("a real failure was swallowed")
			}
			if errors.Is(err, collect.ErrDenied) {
				t.Errorf("%v was classified as a permission denial", err)
			}
		})
	}
}

// THE PARTIAL-READ HALF OF THE SAME RETENTION, and it is not a duplicate of the
// denial one below: they take different arms of the classification switch, and
// only this test drives the ErrPartial arm.
//
// WHY THE ASYMMETRY MATTERED. Every one of the five channels a partial read
// arrives on is a PER-SCOPE signal: an aggregated list reports one unreachable
// zone out of dozens. If the enumeration discarded its page on that signal, one
// unreachable zone would cost every OTHER zone's resources — a larger loss than
// the one being reported, which is the exact failure this whole design exists to
// avoid. The module's own source says so, and the README states it to an
// operator as a contract, and until this test nothing observed it: removing the
// arm left the entire suite green while the page went from two resources to
// zero.
func TestAPartialReadKeepsWhateverThePageAlreadyHeld(t *testing.T) {
	sub := collect.New("test-enum",
		func(context.Context, string) ([]string, error) {
			return []string{"a", "b"}, fmt.Errorf(
				"%w: the provider did not read zones/europe-west1-b", collect.ErrPartial)
		},
		okConverter)

	got, err := sub.Run(t.Context(), "proj-a")
	if !errors.Is(err, collect.ErrPartial) {
		t.Fatalf("the partial read did not classify as one: %v", err)
	}
	if len(got.Resources) != 2 {
		t.Fatalf("a partially read enumeration kept %d of the 2 resources it had already read; "+
			"one unreachable scope would then cost every other scope's resources, which is a "+
			"larger loss than the one being reported", len(got.Resources))
	}
	// The scope the lister named survives to the caller, because the walk's
	// incomplete reason is built from it.
	if !strings.Contains(err.Error(), "zones/europe-west1-b") {
		t.Errorf("the scope the lister named was lost: %v", err)
	}
	// And the enumeration is still named, so the reason locates the gap.
	if !strings.Contains(err.Error(), "test-enum") {
		t.Errorf("the enumeration is not named: %v", err)
	}
}

// THE SAME-RUN CONTROL for the retention above: a HARD FAILURE keeps nothing.
// Without it, an implementation that returned its page on every error arm would
// pass the two retention tests and the distinction they rest on would not exist.
func TestAHardFailureKeepsNothing(t *testing.T) {
	sub := collect.New("test-enum",
		func(context.Context, string) ([]string, error) {
			return []string{"a", "b"}, errors.New("the transport went away")
		},
		okConverter)

	got, err := sub.Run(t.Context(), "proj-a")
	if err == nil {
		t.Fatal("a hard failure was swallowed")
	}
	if errors.Is(err, collect.ErrPartial) || errors.Is(err, collect.ErrDenied) {
		t.Fatalf("a hard failure classified as a partial read or a refusal: %v", err)
	}
	if len(got.Resources) != 0 {
		t.Errorf("a failed enumeration returned %d resources; a failure is not a partial answer "+
			"and its page is not trustworthy", len(got.Resources))
	}
}

// A denial reaching the caller alongside a partial page keeps the page. An
// aggregated enumeration commonly refuses ONE scope out of dozens, and losing
// the other scopes' resources with it would be a far larger loss than the
// refusal itself.
func TestADenialKeepsWhateverThePageAlreadyHeld(t *testing.T) {
	sub := collect.New("test-enum",
		func(context.Context, string) ([]string, error) {
			return []string{"a"}, status.Error(codes.PermissionDenied, "one zone refused")
		},
		okConverter)
	got, err := sub.Run(t.Context(), "proj-a")
	if !errors.Is(err, collect.ErrDenied) {
		t.Fatalf("the partial denial did not classify as one: %v", err)
	}
	if len(got.Resources) != 1 {
		t.Errorf("a partial denial dropped the %d resources already read; got %d",
			1, len(got.Resources))
	}
	if got.Resources[0].ResourceType != gcpgraph.ResourceTypeDisk {
		t.Errorf("the kept resource is not the converted one: %+v", got.Resources[0])
	}
}
