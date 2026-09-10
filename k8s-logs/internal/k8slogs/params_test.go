// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"strings"
	"testing"
	"time"
)

// params_test.go — one arm per refusal. Bad input errors; nothing is coerced,
// defaulted or degraded.

//go:fix inline
func ptr(v int64) *int64 { return new(v) }

// TestNoNamespacesIsRefused is the BOUNDARY that matters most: an empty
// namespace reaches the Kubernetes API as every namespace.
func TestNoNamespacesIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params Params
	}{
		{"absent", Params{}},
		{"empty list", Params{Namespaces: []string{}}},
		{"an empty member", Params{Namespaces: []string{"dev", ""}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.params.Validate()
			if err == nil {
				t.Fatal("an empty namespace was accepted; at the API it means EVERY namespace in the cluster")
			}
			if !strings.Contains(err.Error(), "namespace") {
				t.Fatalf("the refusal does not name the namespace parameter: %v", err)
			}
		})
	}
}

// TestNamespacesAreDeduplicated — the same namespace named twice is one list
// call, not two.
func TestNamespacesAreDeduplicated(t *testing.T) {
	got, err := Params{Namespaces: []string{"dev", "dev", "staging"}}.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Namespaces) != 2 {
		t.Fatalf("namespaces resolved to %v; a repeated namespace is one read", got.Namespaces)
	}
}

// TestTimeRangeArms — parsing, ordering, and the pass-through of an absent
// bound.
func TestTimeRangeArms(t *testing.T) {
	t.Run("both parse", func(t *testing.T) {
		got, err := Params{
			Namespaces: []string{"dev"},
			Since:      "2026-09-07T10:00:00Z",
			Until:      "2026-09-07T11:00:00Z",
		}.Validate()
		if err != nil {
			t.Fatal(err)
		}
		if got.Since.IsZero() || got.Until.IsZero() {
			t.Fatalf("a parsed range came back zero: %s to %s", got.Since, got.Until)
		}
		if !got.Until.After(got.Since) {
			t.Fatalf("the parsed range is inverted: %s to %s", got.Since, got.Until)
		}
	})

	t.Run("an inverted range is refused", func(t *testing.T) {
		_, err := Params{
			Namespaces: []string{"dev"},
			Since:      "2026-09-07T11:00:00Z",
			Until:      "2026-09-07T10:00:00Z",
		}.Validate()
		if err == nil {
			t.Fatal("a range ending before it starts was accepted")
		}
	})

	for _, field := range []string{"since", "until"} {
		t.Run("unparseable "+field, func(t *testing.T) {
			p := Params{Namespaces: []string{"dev"}}
			if field == "since" {
				p.Since = "yesterday"
			} else {
				p.Until = "yesterday"
			}
			_, err := p.Validate()
			if err == nil {
				t.Fatalf("%s = %q was accepted", field, "yesterday")
			}
			if !strings.Contains(err.Error(), field) {
				t.Fatalf("the refusal does not name %s: %v", field, err)
			}
		})
	}

	t.Run("no range at all", func(t *testing.T) {
		got, err := Params{Namespaces: []string{"dev"}}.Validate()
		if err != nil {
			t.Fatal(err)
		}
		if !got.Since.IsZero() || !got.Until.IsZero() {
			t.Fatal("an absent range did not stay zero; unset means unbounded")
		}
	})
}

// TestSelfBoundsMustBePositive — an explicit zero is refused, because unset
// already means unbounded.
func TestSelfBoundsMustBePositive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params Params
	}{
		{"tail_lines zero", Params{Namespaces: []string{"dev"}, TailLines: ptr(0)}},
		{"tail_lines negative", Params{Namespaces: []string{"dev"}, TailLines: ptr(-1)}},
		{"limit_bytes zero", Params{Namespaces: []string{"dev"}, LimitBytes: ptr(0)}},
		{"limit_bytes negative", Params{Namespaces: []string{"dev"}, LimitBytes: ptr(-5)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.params.Validate(); err == nil {
				t.Fatal("a non-positive bound was accepted")
			}
		})
	}

	t.Run("unset stays unbounded", func(t *testing.T) {
		got, err := Params{Namespaces: []string{"dev"}}.Validate()
		if err != nil {
			t.Fatal(err)
		}
		if got.TailLines != nil || got.LimitBytes != nil {
			t.Fatal("an unset bound was given a default; this collector imposes no size bound of its own")
		}
	})
}

// TestStreamSelectorArms including the combination the Kubernetes API itself
// forbids.
func TestStreamSelectorArms(t *testing.T) {
	for _, name := range []string{"", StreamAll, StreamStdout, StreamStderr} {
		if _, err := (Params{Namespaces: []string{"dev"}, Stream: name}).Validate(); err != nil {
			t.Errorf("stream=%q was refused: %v", name, err)
		}
	}
	if _, err := (Params{Namespaces: []string{"dev"}, Stream: "STDOUT"}).Validate(); err == nil {
		t.Error("an unknown stream selector was accepted; it must not silently read both")
	}
	_, err := (Params{Namespaces: []string{"dev"}, Stream: StreamStderr, TailLines: ptr(10)}).Validate()
	if err == nil {
		t.Fatal("tail_lines beside a single-stream selection was accepted; the Kubernetes API refuses that " +
			"combination mid-walk, where nothing in the result explains it")
	}
	if !strings.Contains(err.Error(), "tail_lines") {
		t.Fatalf("the refusal does not name tail_lines: %v", err)
	}
}

// TestChunkWindowArms — a negative window is refused, zero means the default.
func TestChunkWindowArms(t *testing.T) {
	if _, err := (Params{Namespaces: []string{"dev"}, ChunkWindowSeconds: -1}).Validate(); err == nil {
		t.Fatal("a negative chunk window was accepted")
	}
	got, err := Params{Namespaces: []string{"dev"}, ChunkWindowSeconds: 90}.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if got.ChunkWindow != 90*time.Second {
		t.Fatalf("chunk window is %s, want 90s", got.ChunkWindow)
	}
}

// TestEveryParameterSurvivesValidation is the pass-through control: a refusal
// test set proves nothing if validation drops what it accepts.
func TestEveryParameterSurvivesValidation(t *testing.T) {
	got, err := Params{
		Context:       "gke_fulminate-services_us-central1_main",
		Namespaces:    []string{"dev"},
		LabelSelector: "app=api",
		Containers:    []string{"api", "istio-init"},
		Since:         "2026-09-07T10:00:00Z",
		Until:         "2026-09-07T11:00:00Z",
		LimitBytes:    ptr(4096),
		Stream:        StreamStdout,
	}.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if got.Context != "gke_fulminate-services_us-central1_main" {
		t.Errorf("context was dropped: %q", got.Context)
	}
	if got.LabelSelector != "app=api" {
		t.Errorf("label selector was dropped: %q", got.LabelSelector)
	}
	if len(got.Containers) != 2 {
		t.Errorf("containers were dropped: %v", got.Containers)
	}
	if got.LimitBytes == nil || *got.LimitBytes != 4096 {
		t.Errorf("limit_bytes was dropped: %v", got.LimitBytes)
	}
	if got.Stream != StreamStdout {
		t.Errorf("stream was dropped: %q", got.Stream)
	}
}
