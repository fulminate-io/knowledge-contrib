// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"encoding/json"
	"maps"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

// sink_test.go — THE CONTENT ENCODER, tested DIFFERENTIALLY against
// encoding/json.
//
// The encoder is hand-written for two reasons stated at its declaration: to put
// the sorted-key requirement where it is needed rather than relying on a
// documented property of another package, and to have no failure path to absorb.
// Both are only worth having if the output is right, and "right" here has an
// external answer: encoding/json's. So the test does not assert on strings this
// file chose — it asserts that the two encoders AGREE, over inputs chosen to
// break a hand-written escaper.
//
// AN IDENTITY CHECK NEEDS AN EXTERNAL EXPECTATION, and this is it: the subject
// does not supply its own answer key.

// escapeCorpus is the input set. Each entry is a shape a hand-written escaper
// gets wrong, and the AWS values these maps carry — a tag, a description, a
// resource name — are arbitrary operator input that can hold any of them.
var escapeCorpus = []string{
	"",
	"plain",
	`a "quoted" value`,
	`a\backslash`,
	`both "quoted" and \escaped\`,
	"a\nnewline",
	"a\ttab",
	"a\rcarriage return",
	"\x00a null byte",
	"\x1fthe last control character",
	"\x20the first printable one",
	"unicode: héllo wörld",
	"emoji: \U0001F510",
	"cjk: 秘密鍵",
	"an invalid byte sequence: \xff\xfe",
	"a lone continuation byte: \x80",
	`{"looks":"like json"}`,
	`</script>`,
	strings.Repeat("long ", 200),
}

func TestSink_ContentEncodingAgreesWithEncodingJSON(t *testing.T) {
	for _, key := range escapeCorpus {
		for _, value := range escapeCorpus {
			m := map[string]string{key: value}
			got := marshalContent(m)
			want, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("the reference encoder failed on %q: %v", value, err)
			}
			if got != string(want) {
				t.Errorf("encoders disagree.\n key:  %q\n value: %q\n  got: %s\n want: %s",
					key, value, got, want)
			}
		}
	}
}

// TestSink_ContentEncodingRoundTrips is the second half: agreeing with
// encoding/json's BYTES is the strong claim, and this is the one that says the
// output is a JSON object carrying the same map.
func TestSink_ContentEncodingRoundTrips(t *testing.T) {
	m := map[string]string{}
	for i, s := range escapeCorpus {
		if s == "" {
			continue // an empty key is legal JSON but makes the failure message unreadable
		}
		m[s] = escapeCorpus[len(escapeCorpus)-1-i]
	}
	var back map[string]string
	if err := json.Unmarshal([]byte(marshalContent(m)), &back); err != nil {
		t.Fatalf("the encoded content does not parse as JSON: %v\n%s", err, marshalContent(m))
	}
	// THE COMPARISON IS AGAINST WHAT encoding/json ITSELF ROUND-TRIPS, not against
	// the input: both encoders replace an invalid byte sequence, so the input is
	// not what either one preserves.
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("reference marshal: %v", err)
	}
	var reference map[string]string
	if err := json.Unmarshal(raw, &reference); err != nil {
		t.Fatalf("reference unmarshal: %v", err)
	}
	if !maps.Equal(back, reference) {
		t.Errorf("the round-tripped map differs from encoding/json's:\n got %#v\nwant %#v", back, reference)
	}
}

// TestSink_ContentIsByteIdenticalRegardlessOfInsertionOrder is the property the
// encoder exists for.
//
// Go's map iteration order is randomized per run, so a builder that ranged the
// map in place would produce a different document on every collect — and the
// carry-forward diff, which keys on the node id and compares content, would write
// a row for every node every time. This builds the same map through many random
// insertion orders and requires one output.
func TestSink_ContentIsByteIdenticalRegardlessOfInsertionOrder(t *testing.T) {
	base := map[string]string{
		"zeta": "1", "alpha": "2", "mu": "3", "beta": "4",
		"omega": "5", "gamma": "6", "delta": "7", "epsilon": "8",
	}
	want := marshalContent(base)
	if want == "" {
		t.Fatal("the encoder produced nothing for a non-empty map")
	}
	keys := make([]string, 0, len(base))
	for k := range base {
		keys = append(keys, k)
	}
	for range 50 {
		shuffled := map[string]string{}
		rand.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
		for _, k := range keys {
			shuffled[k] = base[k]
		}
		if got := marshalContent(shuffled); got != want {
			t.Fatalf("the same map encoded two ways:\n got %s\nwant %s", got, want)
		}
	}
}

// TestSink_AbsentContentIsAnAbsentField pins the nil and empty arms: an absent
// detail is an absent field, never `null` or `{}`, because a key that appears
// with an empty value on one run and is absent on another is a change no resource
// had.
func TestSink_AbsentContentIsAnAbsentField(t *testing.T) {
	if got := marshalContent(nil); got != "" {
		t.Errorf("a nil detail map encoded to %q, want the empty string", got)
	}
	if got := marshalContent(map[string]string{}); got != "" {
		t.Errorf("an empty detail map encoded to %q, want the empty string", got)
	}
}

// TestSink_EdgeAndNodeDedupeKeyOnTheWholeIdentity covers the two dedupe keys
// directly, which the account-level tests only reach through one shape each.
func TestSink_EdgeAndNodeDedupeKeyOnTheWholeIdentity(t *testing.T) {
	s := newSink()

	// TWO EDGES BETWEEN ONE PAIR WITH DIFFERENT TYPES ARE TWO EDGES. A dedupe
	// keyed on the endpoints alone would silently drop the second, and a pair of
	// resources routinely carries several relationships.
	s.addEdge("a", "b", EdgeUsesSubnet, nil)
	s.addEdge("a", "b", EdgeUsesSecurityGroup, nil)
	s.addEdge("a", "b", EdgeUsesSubnet, map[string]string{"note": "a repeat with different evidence"})
	// AND DIRECTION IS PART OF IDENTITY: PEERED_WITH is emitted both ways
	// deliberately, so a dedupe that ignored direction would halve it.
	s.addEdge("b", "a", EdgeUsesSubnet, nil)

	_, edges := s.result()
	if len(edges) != 3 {
		t.Errorf("want 3 edges (two types one way, one the other), got %d: %+v", len(edges), edges)
	}

	// AN EDGE WITH AN EMPTY ENDPOINT OR TYPE IS NOT AN EDGE, and is dropped rather
	// than emitted malformed.
	before := len(edges)
	s.addEdge("", "b", EdgeUsesSubnet, nil)
	s.addEdge("a", "", EdgeUsesSubnet, nil)
	s.addEdge("a", "b", "", nil)
	if _, after := s.result(); len(after) != before {
		t.Errorf("a malformed edge was admitted: %d edges became %d", before, len(after))
	}
}

// TestSink_ResultIsSortedAndCloned pins the two properties the walk depends on:
// a stable order, and a result the caller can hold without the sink mutating it.
func TestSink_ResultIsSortedAndCloned(t *testing.T) {
	s := newSink()
	for _, id := range []string{"zzz", "aaa", "mmm"} {
		s.addNode(newNode(resource{id: id, resourceType: ResourceTypeVPC, name: id}, fixtureAccount))
	}
	nodes, _ := s.result()
	if len(nodes) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(nodes))
	}
	for i := 1; i < len(nodes); i++ {
		if nodes[i-1].ID > nodes[i].ID {
			t.Errorf("the result is not sorted by id: %q before %q", nodes[i-1].ID, nodes[i].ID)
		}
	}
	// A SECOND read is unaffected by mutating the first, which is what the clone
	// in result() is for: the walk hands its slices to the framework, and a sink
	// that shared its backing array would let a later append reach them.
	snapshot := append([]struct{ ID string }(nil), struct{ ID string }{nodes[0].ID})
	nodes[0].ID = "mutated"
	again, _ := s.result()
	if again[0].ID != snapshot[0].ID {
		t.Errorf("mutating a returned node changed the sink's own: got %q, want %q", again[0].ID, snapshot[0].ID)
	}
	if reflect.DeepEqual(nodes, again) {
		t.Error("the mutation above did not take, so the clone assertion proves nothing")
	}
}
