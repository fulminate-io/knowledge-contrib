// SPDX-License-Identifier: Apache-2.0

// Package collect holds this collector's ENUMERATIONS: one per resource family,
// each pairing a narrow list interface over the provider SDK with a pure
// converter from what the API returned to what the graph carries.
//
// THE SPLIT IS WHAT MAKES FORTY ENUMERATIONS TESTABLE WITHOUT A NETWORK. A
// [Lister] is a function the production wiring implements by draining an SDK
// iterator and a test implements by returning canned values; a [Converter] is a
// pure function over one API value. Every behavior worth asserting — the id
// spelling, the resource type, the metadata, the edges and their endpoints —
// lives in the converter, so the whole family suite runs offline with no
// credential, and the untested remainder is a loop that copies an iterator into
// a slice.
//
// A DENIED PERMISSION IS NEITHER A FAILED WALK NOR AN EMPTY ONE, and keeping
// those three apart is this package's most consequential job.
//
// A project where one API is not enabled or one role is not granted is the
// ordinary case, so failing the whole collect over it would make the collector
// unusable against any real project. But reporting it as an EMPTY enumeration is
// worse than useless: "this project has no caches" and "I was not allowed to
// look for caches" are the same empty list, and a walk that cannot tell them
// apart asserts COMPLETE in both cases. A complete walk is what lets the
// receiving server treat everything the walk did not carry as gone, so a role
// revoked between two collects would silently delete a whole service's
// resources and the collect would report success.
//
// So a denial comes back as [ErrDenied], distinguishable from both: the collect
// still succeeds and every other enumeration still runs, and the walk that
// contains it is INCOMPLETE and names what it was refused.
package collect

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// Lister enumerates one resource kind for one project. The production
// implementation drains the SDK's own iterator; a test returns canned values.
type Lister[T any] func(ctx context.Context, projectID string) ([]T, error)

// Converter turns one enumerated API value into the resources and relations it
// contributes. It is pure: no network, no clock, no environment.
//
// It returns an ERROR rather than skipping when the value is malformed, because
// a converter that quietly emitted a partial node would put a resource in the
// graph with an id and nothing else, and nothing downstream can tell that from a
// resource that genuinely has no detail.
type Converter[T any] func(projectID string, item T) (gcpgraph.Result, error)

// Subcollector is one named enumeration in the walk. The name is what an
// operator reads in a diagnostic and what the incomplete-walk reason carries.
type Subcollector struct {
	Name string
	Run  func(ctx context.Context, projectID string) (gcpgraph.Result, error)
}

// The two ways an enumeration can come back having NOT seen all of what it asked
// for. Both are distinct from a failure and from an empty answer, and a caller
// classifies with errors.Is.
//
// They are distinct VALUES rather than log lines because the difference decides
// whether the walk may assert completeness, and a log line decides nothing.
var (
	// ErrDenied marks an enumeration the provider REFUSED outright.
	ErrDenied = errors.New("the provider refused this enumeration")

	// ErrPartial marks an enumeration the provider ANSWERED while saying part of
	// the answer is missing — an unreachable zone, a missing location, a failed
	// region. The provider reports this on five different channels depending on
	// the service; they all mean the same thing here.
	//
	// IT IS NOT A WEAKER FORM OF ErrDenied. A refusal is about this credential
	// and is fixed by granting a role; a partial read is about the provider and
	// is usually transient. What they share is the only thing the walk cares
	// about: this run did not see part of the project, so it may not claim to
	// have seen all of it.
	ErrPartial = errors.New("the provider answered only in part")
)

// New builds a sub-collector from a lister and a converter.
func New[T any](name string, list Lister[T], convert Converter[T]) Subcollector {
	return Subcollector{
		Name: name,
		Run: func(ctx context.Context, projectID string) (gcpgraph.Result, error) {
			items, err := list(ctx, projectID)
			converted, convertErr := convertAll(name, projectID, items, convert)
			if convertErr != nil {
				return gcpgraph.Result{}, convertErr
			}
			switch {
			case err == nil:
				return converted, nil
			case errors.Is(err, ErrPartial), errors.Is(err, ErrDenied):
				// Already classified by the lister, which knows which scope it
				// was. The partial page is KEPT.
				return converted, fmt.Errorf("%s: %w", name, err)
			case IsPermissionDenied(err):
				// The partial page is KEPT. An aggregated enumeration commonly
				// refuses one zone out of dozens, and discarding the others'
				// resources would be a far larger loss than the refusal.
				return converted, fmt.Errorf("%s: %w: %w", name, ErrDenied, err)
			default:
				return gcpgraph.Result{}, fmt.Errorf("%s: listing: %w", name, err)
			}
		},
	}
}

// convertAll runs the converter over everything the lister produced. A malformed
// item is an error whatever else happened, because a node with an id and nothing
// else is indistinguishable from a resource that genuinely has no detail.
func convertAll[T any](
	name, projectID string, items []T, convert Converter[T],
) (gcpgraph.Result, error) {
	var out gcpgraph.Result
	for i, item := range items {
		got, err := convert(projectID, item)
		if err != nil {
			return gcpgraph.Result{}, fmt.Errorf("%s: item %d: %w", name, i, err)
		}
		out.Add(got)
	}
	return out, nil
}

// IsPermissionDenied reports whether an error is the provider refusing access,
// on either of the two transports these SDKs use: a gRPC status and a REST error
// carry the same refusal in different shapes, and a check that knew only one
// would turn half the enumerations into hard failures on a project where the
// other API is simply not enabled.
func IsPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.PermissionDenied, codes.Unauthenticated:
			return true
		}
	}
	if apiErr, ok := errors.AsType[*googleapi.Error](err); ok {
		return apiErr.Code == 401 || apiErr.Code == 403
	}
	return false
}
