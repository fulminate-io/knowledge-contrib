// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// bounds_test.go — THE RETIRED TRAFFIC CAPS.
//
// A collect is one process handing bytes to another and none of it reaches a
// language model, so the reasons a cap exists elsewhere do not apply here. Two
// bounds that once did apply are gone, and each cell below is paired with its
// opposite so neither reads as an absence:
//
//   - THERE IS NO CEILING ON LINE LENGTH. An earlier revision buffered lines
//     through a scanner bounded at eight megabytes, which refused a longer line
//     with an error and failed the whole container's read.
//   - TRUNCATION IS MEASURED, NEVER INFERRED. An earlier revision reported every
//     read carrying a byte bound as truncated whether or not the bound was
//     reached, so a generous bound made every collect incomplete forever and the
//     server's deletion phase never ran again.
//
// What is NOT retired, and is asserted here too: a bound the operator named and
// the read actually spent is still truncation, because under-reporting
// incompleteness lets the server treat what the walk did not carry as gone.

// TestNoLineLengthCeiling — a single line larger than any fixed buffer is read
// WHOLE.
//
// This is a traffic bound and the owner's ruling removes it: a collect is one
// process handing bytes to another and none of it reaches a language model, so
// a ceiling that refuses a longer line is a cap with nothing to justify it. An
// earlier revision buffered through a scanner bounded at eight megabytes, which
// failed the whole container's read on a longer line. A serialized payload or a
// one-line stack trace at that size is ordinary.
func TestNoLineLengthCeiling(t *testing.T) {
	const size = 12 << 20 // comfortably past the eight-megabyte bound that used to be here
	huge := strings.Repeat("x", size)
	body := stamped(fixtureBase, huge, "a short line after it")

	res, err := scanLog(context.Background(), strings.NewReader(body),
		Source{Namespace: "dev", Pod: "api-1", Container: "api"}, nil, ReadOptions{})
	if err != nil {
		t.Fatalf("a %d-byte line failed the read: %v. There is no maximum line length", size, err)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("%d entries from two lines, want 2 (skipped %d)", len(res.Entries), res.SkippedLines)
	}
	if len(res.Entries[0].Message) != size {
		t.Fatalf("the long line arrived at %d bytes, want %d; it was truncated rather than read whole",
			len(res.Entries[0].Message), size)
	}
	if res.Entries[1].Message != "a short line after it" {
		t.Fatalf("the line after the long one is %q; the read did not continue past it", res.Entries[1].Message)
	}
}

// TestAByteBoundIsTruncationONLYWhenItWasReached — the arm that used to infer.
//
// An earlier revision reported EVERY read carrying a byte bound as truncated,
// whether or not the bound was reached, so an operator who set a generous bound
// made every collect incomplete forever and the server's deletion phase never
// ran again. Both cells are asserted here, and they are each other's control.
func TestAByteBoundIsTruncationONLYWhenItWasReached(t *testing.T) {
	body := stamped(fixtureBase, "one", "two")
	src := Source{Namespace: "dev", Pod: "api-1", Container: "api"}

	generous := int64(len(body) * 10)
	res, err := scanLog(context.Background(), strings.NewReader(body), src, nil,
		ReadOptions{LimitBytes: &generous})
	if err != nil {
		t.Fatal(err)
	}
	if res.Truncated {
		t.Fatalf("a %d-byte read under a %d-byte bound reported itself truncated. Truncation is MEASURED "+
			"against what the read returned, never inferred from a bound being present",
			len(body), generous)
	}

	reached := int64(len(body))
	cut, err := scanLog(context.Background(), strings.NewReader(body), src, nil,
		ReadOptions{LimitBytes: &reached})
	if err != nil {
		t.Fatal(err)
	}
	if !cut.Truncated {
		t.Fatalf("a read that delivered its whole %d-byte bound did not report itself truncated; "+
			"under-reporting incompleteness lets the server treat what the walk did not carry as gone",
			reached)
	}
}

// TestALineBoundIsTruncationONLYWhenItWasReached is the same pair for the line
// arm, kept separate so neither bound's assertion rides on the other's.
func TestALineBoundIsTruncationONLYWhenItWasReached(t *testing.T) {
	body := stamped(fixtureBase, "one", "two")
	src := Source{Namespace: "dev", Pod: "api-1", Container: "api"}

	generous := int64(50)
	res, err := scanLog(context.Background(), strings.NewReader(body), src, nil,
		ReadOptions{TailLines: &generous})
	if err != nil {
		t.Fatal(err)
	}
	if res.Truncated {
		t.Fatalf("a two-line read under a %d-line bound reported itself truncated", generous)
	}

	reached := int64(2)
	cut, err := scanLog(context.Background(), strings.NewReader(body), src, nil,
		ReadOptions{TailLines: &reached})
	if err != nil {
		t.Fatal(err)
	}
	if !cut.Truncated {
		t.Fatal("a read that returned as many lines as its bound asked for did not report itself truncated")
	}
}

// countingBody wraps a log body and records that it was closed.
type countingBody struct {
	io.Reader
	closes int
}

func (c *countingBody) Close() error { c.closes++; return nil }

// TestTheStreamIsClosedOnBothPaths — the per-container apiserver stream is the
// highest-multiplicity resource this collector holds, one per container per
// collect, and the deferred Close is what releases it.
//
// IT IS OBSERVED THROUGH THE STREAM SEAM RATHER THAN THROUGH A LEAK GATE, and
// that is a measured choice rather than a preference. Three instruments were
// tried against this Close and none discriminates it: a goroutine-leak gate in
// this package, a counting dialer over each connection, and a connection-reuse
// count. The measured reason is that the success path reads to the end of the
// body, and the HTTP transport drains and recycles a fully-read body itself, so
// the Close is genuinely redundant there — the path where it is load-bearing is
// the EARLY return a mid-body failure takes. An observable on the body itself
// sees both, needs no goroutine or socket inference, and reds on the deleted
// defer directly.
func TestTheStreamIsClosedOnBothPaths(t *testing.T) {
	src := Source{Namespace: "dev", Pod: "api-1", Container: "api"}

	t.Run("the success path", func(t *testing.T) {
		body := &countingBody{Reader: strings.NewReader(stamped(fixtureBase, "one", "two"))}
		res, err := ReadContainerLog(context.Background(), nil, src, nil, ReadOptions{
			Open: func(context.Context, kubernetes.Interface, Source, *corev1.PodLogOptions) (io.ReadCloser, error) {
				return body, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Entries) != 2 {
			t.Fatalf("%d entries from two lines", len(res.Entries))
		}
		if body.closes != 1 {
			t.Fatalf("the stream was closed %d times on a successful read, want once. One apiserver "+
				"connection per container per collect is held until it is closed", body.closes)
		}
	})

	t.Run("the mid-body failure path", func(t *testing.T) {
		// THE PATH WHERE THE CLOSE IS LOAD-BEARING: the read returns early, so
		// the transport never gets to drain and recycle the body itself.
		body := &countingBody{Reader: io.MultiReader(
			strings.NewReader(stamped(fixtureBase, "one")),
			iotest.ErrReader(errors.New("the connection broke mid-body")),
		)}
		_, err := ReadContainerLog(context.Background(), nil, src, nil, ReadOptions{
			Open: func(context.Context, kubernetes.Interface, Source, *corev1.PodLogOptions) (io.ReadCloser, error) {
				return body, nil
			},
		})
		if err == nil {
			t.Fatal("a body that failed mid-read returned no error")
		}
		if body.closes != 1 {
			t.Fatalf("the stream was closed %d times after an early return, want once. This is the path the "+
				"transport cannot clean up for us, because the body was never read to its end", body.closes)
		}
	})
}
