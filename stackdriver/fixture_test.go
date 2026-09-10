// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"time"

	"cloud.google.com/go/logging"
	"google.golang.org/api/iterator"
	mrpb "google.golang.org/genproto/googleapis/api/monitoredres"
)

// fixture_test.go — THE RECORDED INPUTS every offline arm runs on, and the fake
// iterator that stands in for the Cloud Logging entries stream.
//
// THE FAKE IS AT THE ITERATOR, WHICH IS THE PROVIDER'S OWN SEAM, and not at the
// normalizer or the pipeline: everything from the drain inward is the real code
// path, including the bound, the truncation assertion and the mid-stream failure
// posture. Nothing about the graph this module emits is produced by a test
// double.

// baseTime is the instant the recorded entries hang off. It is fixed rather than
// time.Now() so every derived chunk id is reproducible across runs and machines.
var baseTime = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

// fakeNexter replays a recorded slice of entries and then reports exhaustion,
// optionally failing part way through.
type fakeNexter struct {
	entries []*logging.Entry
	idx     int
	// failAfter, when positive, makes Next return failWith once that many
	// entries have been handed out. It is how the mid-stream failure arm is
	// reached without a network.
	failAfter int
	failWith  error
}

func (f *fakeNexter) Next() (*logging.Entry, error) {
	if f.failAfter > 0 && f.idx >= f.failAfter {
		return nil, f.failWith
	}
	if f.idx >= len(f.entries) {
		return nil, iterator.Done
	}
	e := f.entries[f.idx]
	f.idx++
	return e, nil
}

// gcpEntry builds one Cloud Logging entry with a monitored resource and entry
// labels.
func gcpEntry(offset time.Duration, sev logging.Severity, payload any,
	resourceType string, resourceLabels, entryLabels map[string]string) *logging.Entry {
	e := &logging.Entry{
		Timestamp: baseTime.Add(offset),
		Severity:  sev,
		Payload:   payload,
		LogName:   "projects/fulminate-services/logs/stderr",
		Labels:    entryLabels,
	}
	if resourceType != "" || resourceLabels != nil {
		e.Resource = &mrpb.MonitoredResource{Type: resourceType, Labels: resourceLabels}
	}
	return e
}

// recordedEntries is the entry set the walk-level arms run on: two services,
// two message shapes each, so the run produces several templates, two streams
// and cross-service error templates for the correlation pass to consider.
func recordedEntries() []logEntry {
	return normalizeAll(recordedGCPEntries(), "fulminate-services")
}

// recordedGCPEntries is the same set before normalization, for the arms whose
// subject is the read rather than the graph.
func recordedGCPEntries() []*logging.Entry {
	api := map[string]string{"container_name": "api", "pod_name": "api-7b6", "namespace_name": "prod"}
	worker := map[string]string{"container_name": "worker", "pod_name": "worker-1", "namespace_name": "prod"}
	return []*logging.Entry{
		gcpEntry(0, logging.Info, "request served in 12 ms", "k8s_container", api, nil),
		gcpEntry(time.Second, logging.Info, "request served in 340 ms", "k8s_container", api, nil),
		gcpEntry(2*time.Second, logging.Error, "connect failed: dial tcp 10.0.0.1:443", "k8s_container", api, nil),
		gcpEntry(3*time.Second, logging.Error, "queue drain failed: broker unreachable", "k8s_container", worker, nil),
		gcpEntry(4*time.Second, logging.Warning, "retrying in 5 s", "k8s_container", worker, nil),
	}
}

// normalizeAll runs the real normalizer over recorded entries.
func normalizeAll(entries []*logging.Entry, project string) []logEntry {
	out := make([]logEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, normalizeEntry(e, project))
	}
	return out
}

// readerReturning is an entryReader that hands back a fixed read.
func readerReturning(r drainResult) entryReader {
	return func(context.Context, string, logQuery) (drainResult, error) { return r, nil }
}

// readerFailing is an entryReader that fails.
func readerFailing(err error) entryReader {
	return func(context.Context, string, logQuery) (drainResult, error) { return drainResult{}, err }
}

// testEntry builds one normalized entry directly, for the arms whose subject is
// a pipeline stage rather than the provider.
func testEntry(offset time.Duration, severity, message string, labels map[string]string) logEntry {
	return logEntry{
		Timestamp: baseTime.Add(offset),
		Severity:  severity,
		Message:   message,
		Labels:    labels,
	}
}

// labels is a terse label-map literal for the fixtures.
func labels(pairs ...string) map[string]string {
	m := make(map[string]string, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return m
}
