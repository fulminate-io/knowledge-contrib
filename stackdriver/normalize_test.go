// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"
	"time"

	"cloud.google.com/go/logging"
	"google.golang.org/protobuf/types/known/structpb"
)

// normalize_test.go — one entry to one normalized entry: the severity
// reclassification, the two label passes, the message extraction and the log-id
// decode.

// TestGKEReclassificationIsDownwardOnly covers both directions of the one rule
// whose inversion is invisible in ordinary output. The control is the second
// case: an embedded severity HIGHER than the wrapper's must NOT upgrade, or any
// line containing the word "error" promotes itself.
func TestGKEReclassificationIsDownwardOnly(t *testing.T) {
	down := normalizeEntry(gcpEntry(0, logging.Error, `level=info request served`, "", nil, nil), "p")
	if down.Severity != severityInfo {
		t.Errorf("an INFO-marked body under an ERROR wrapper stayed %s, want %s", down.Severity, severityInfo)
	}

	up := normalizeEntry(gcpEntry(0, logging.Info, `level=error something broke`, "", nil, nil), "p")
	if up.Severity != severityInfo {
		t.Errorf("an ERROR-marked body under an INFO wrapper was UPGRADED to %s; "+
			"the reclassification is downward only", up.Severity)
	}
}

// TestNormalizeMapsResourceLabelsThroughTheirFallbackChains covers each
// first-match-wins chain, one arm per link, because a chain implemented as a
// series of assignments would let a later key overwrite an earlier one.
func TestNormalizeMapsResourceLabelsThroughTheirFallbackChains(t *testing.T) {
	for _, tc := range []struct {
		name           string
		resourceLabels map[string]string
		wantService    string
		wantHost       string
	}{
		{"container name wins over service name",
			labels("container_name", "api", "service_name", "run-svc"), "api", ""},
		{"service name when there is no container",
			labels("service_name", "run-svc"), "run-svc", ""},
		{"module id last",
			labels("module_id", "default"), "default", ""},
		{"instance id wins over pod name for host",
			labels("instance_id", "i-1", "pod_name", "p-1"), "", "i-1"},
		{"pod name when there is no instance",
			labels("pod_name", "p-1"), "", "p-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			le := normalizeEntry(gcpEntry(0, logging.Info, "m", "k8s_container", tc.resourceLabels, nil), "p")
			if le.Labels["service"] != tc.wantService {
				t.Errorf("service = %q, want %q", le.Labels["service"], tc.wantService)
			}
			if le.Labels["host"] != tc.wantHost {
				t.Errorf("host = %q, want %q", le.Labels["host"], tc.wantHost)
			}
		})
	}
}

// TestResourceLabelsWinOverEntryLabels is the collision rule: the monitored
// resource is the authoritative source.
func TestResourceLabelsWinOverEntryLabels(t *testing.T) {
	le := normalizeEntry(gcpEntry(0, logging.Info, "m", "k8s_container",
		labels("pod_name", "from-resource"), labels("pod_name", "from-entry", "team", "payments")), "p")
	if le.Labels["pod_name"] != "from-resource" {
		t.Errorf("pod_name = %q, want the resource value", le.Labels["pod_name"])
	}
	// KNOWN POSITIVE: an entry label the resource did not claim still arrives.
	if le.Labels["team"] != "payments" {
		t.Errorf("an unclaimed entry label was dropped: %v", le.Labels)
	}
}

// TestEmptyValuedEntryLabelsAreSkipped is the half of the copy guard that is
// easiest to drop and most expensive to drop: an empty-valued key still enters
// the stream fingerprint's preimage, so admitting one moves every stream id.
func TestEmptyValuedEntryLabelsAreSkipped(t *testing.T) {
	le := normalizeEntry(gcpEntry(0, logging.Info, "m", "k8s_container",
		labels("pod_name", "p-1"), labels("reason", "", "team", "payments")), "p")
	if _, present := le.Labels["reason"]; present {
		t.Errorf("an empty-valued entry label was copied into the label set: %v", le.Labels)
	}
	if le.Labels["team"] != "payments" {
		t.Errorf("the non-empty entry label was dropped too: %v", le.Labels)
	}
}

// TestKubernetesAppLabelPromotesToServiceOnlyWhenNoServiceWasFound covers the
// promotion and its control.
func TestKubernetesAppLabelPromotesToServiceOnlyWhenNoServiceWasFound(t *testing.T) {
	promoted := normalizeEntry(gcpEntry(0, logging.Info, "m", "gce_instance",
		nil, labels("k8s-pod/app", "checkout")), "p")
	if promoted.Labels["service"] != "checkout" {
		t.Errorf("k8s-pod/app was not promoted: %v", promoted.Labels)
	}

	notPromoted := normalizeEntry(gcpEntry(0, logging.Info, "m", "k8s_container",
		labels("container_name", "api"), labels("k8s-pod/app", "checkout")), "p")
	if notPromoted.Labels["service"] != "api" {
		t.Errorf("k8s-pod/app overwrote a resource-derived service: %v", notPromoted.Labels)
	}
}

// TestProvenanceLabelsAreUnconditionalAndProjectIDIsTheCollectsOwn covers both
// labels and the OVERWRITE, which is what makes an entry read out of one project
// carry that project whatever its resource claims.
func TestProvenanceLabelsAreUnconditionalAndProjectIDIsTheCollectsOwn(t *testing.T) {
	le := normalizeEntry(gcpEntry(0, logging.Info, "m", "k8s_container",
		labels("project_id", "resource-project"), nil), "collect-project")
	if le.Labels["project_id"] != "collect-project" {
		t.Errorf("project_id = %q, want the collect's own project", le.Labels["project_id"])
	}
	if le.Labels["log_name"] != "stderr" {
		t.Errorf("log_name = %q, want the decoded log id", le.Labels["log_name"])
	}
}

// TestExtractMessageCoversEveryPayloadShape is the polymorphic-payload matrix.
func TestExtractMessageCoversEveryPayloadShape(t *testing.T) {
	structPayload, err := structpb.NewStruct(map[string]any{"message": "from a struct"})
	if err != nil {
		t.Fatalf("building the struct payload: %v", err)
	}
	for _, tc := range []struct {
		name    string
		payload any
		want    string
	}{
		{"string", "plain text", "plain text"},
		{"struct", structPayload, "from a struct"},
		{"map", map[string]any{"msg": "from a map"}, "from a map"},
		{"nested map", map[string]any{"message": map[string]any{"body": "nested"}}, "nested"},
		{"nil", nil, ""},
		{"nil struct pointer", (*structpb.Struct)(nil), ""},
		{"unknown type marshals", []int{1, 2}, "[1,2]"},
		{"map with no known key marshals", map[string]any{"other": "x"}, `{"other":"x"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractMessage(&logging.Entry{Payload: tc.payload}); got != tc.want {
				t.Errorf("extractMessage = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestExtractLogIDStripsThePrefixAndDecodesTheEncodedSlash covers the real-world
// shape (an audit log id carries a slash, which Cloud Logging percent-encodes)
// and the pass-through for a name with no prefix.
func TestExtractLogIDStripsThePrefixAndDecodesTheEncodedSlash(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"projects/p/logs/stderr", "stderr"},
		{"projects/p/logs/cloudaudit.googleapis.com%2Factivity", "cloudaudit.googleapis.com/activity"},
		{"stderr", "stderr"},
	} {
		if got := extractLogID(tc.in); got != tc.want {
			t.Errorf("extractLogID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNormalizeHandlesEntriesMissingEveryOptionalField is the wrong-shape input
// class: no payload, no timestamp, no resource, no labels. It must produce a
// well-formed entry rather than panicking.
func TestNormalizeHandlesEntriesMissingEveryOptionalField(t *testing.T) {
	le := normalizeEntry(&logging.Entry{}, "p")
	if le.Message != "" {
		t.Errorf("message = %q, want empty", le.Message)
	}
	if !le.Timestamp.IsZero() {
		t.Errorf("timestamp = %v, want the zero time", le.Timestamp)
	}
	if le.Labels["project_id"] != "p" {
		t.Errorf("the provenance label is missing: %v", le.Labels)
	}
	if _, present := le.Labels["log_name"]; present {
		t.Errorf("log_name was written for an entry with no log name: %v", le.Labels)
	}
}

// TestDetectEmbeddedSeverityReadsOnlyTheHeadOfALine is the scan-limit boundary:
// a marker inside the first 200 bytes is the line's level, one past it is prose.
func TestDetectEmbeddedSeverityReadsOnlyTheHeadOfALine(t *testing.T) {
	pad := make([]byte, detectEmbeddedSeverityScanLimit)
	for i := range pad {
		pad[i] = 'x'
	}
	if got := detectEmbeddedSeverity("level=warn " + string(pad)); got != severityWarn {
		t.Errorf("a marker at the head was missed: %q", got)
	}
	if got := detectEmbeddedSeverity(string(pad) + " level=warn"); got != "" {
		t.Errorf("a marker past the scan limit was read as %q, want none", got)
	}
}

// TestNormalizedLabelSetIsStableAcrossTwoNormalizationsOfTheSameEntry is the
// property the reconciliation rests on, asserted at the normalizer rather than
// only end to end.
func TestNormalizedLabelSetIsStableAcrossTwoNormalizationsOfTheSameEntry(t *testing.T) {
	e := gcpEntry(time.Second, logging.Error, "boom", "k8s_container",
		labels("container_name", "api", "pod_name", "api-7b6"), labels("team", "payments"))
	first := fingerprintLabels(normalizeEntry(e, "p").Labels)
	second := fingerprintLabels(normalizeEntry(e, "p").Labels)
	if first != second {
		t.Fatalf("two normalizations of one entry produced different fingerprints: %s vs %s", first, second)
	}
}
