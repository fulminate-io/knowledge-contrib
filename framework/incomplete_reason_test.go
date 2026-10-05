// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"
)

// incomplete_reason_test.go — an incomplete walk's REASON leaves the provider.
//
// framework.Incomplete requires a reason and refuses an empty one, yet the
// encoded result carried only walk_complete=false: the reason was checked for
// presence and then dropped, so the client's deletion refusal told the operator
// to "re-collect once the reason is resolved" with no channel that named it.
//
// THE ASSERTION IS ON THE ENVELOPE KEY the client decodes,
// walk_incomplete_reason, not on the reason appearing anywhere in the call
// result: a misspelled key or a reason carried only in the text content would
// reach no operator.

// TestIncompleteWalkReasonLeavesTheProvider serves the incomplete fixture on
// both transports and requires its reason under walk_incomplete_reason in the
// envelope the client receives.
func TestIncompleteWalkReasonLeavesTheProvider(t *testing.T) {
	const reason = "the source rate-limited the walk" // fixture_test.go, fixtureIncompleteWalk
	for _, transport := range []string{overStdio, overHTTP} {
		t.Run(transport, func(t *testing.T) {
			p := dial(t, transport, fixtureIncompleteWalk, "")
			res, err := p.call(t, map[string]any{"id": "inst-7"})
			if err != nil {
				t.Fatalf("an incomplete walk failed the whole collect: %v", err)
			}
			if res.IsError {
				t.Fatalf("an incomplete walk was refused: %s", resultText(res))
			}
			doc := envelopeOf(t, res)
			// CONTROL: this is the incomplete arm, so a missing reason is not
			// explained by a walk that asserted completeness.
			if doc["walk_complete"] != false {
				t.Fatalf("fixture control: walk_complete is %v, want false", doc["walk_complete"])
			}
			if got := doc["walk_incomplete_reason"]; got != reason {
				t.Errorf("walk_incomplete_reason is %v, want %q; the envelope the client receives: %s",
					got, reason, mustJSON(t, doc))
			}
		})
	}
}

// TestCompleteWalkCarriesNoIncompleteReason is the complete arm: the key is
// ABSENT, not an empty string, so a client that predates the key keeps reading
// a new collector's complete walks, and a client that knows it never reads a
// reason on a walk that asserted completeness.
func TestCompleteWalkCarriesNoIncompleteReason(t *testing.T) {
	for _, transport := range []string{overStdio, overHTTP} {
		t.Run(transport, func(t *testing.T) {
			p := dial(t, transport, fixtureConforming, "")
			res, err := p.call(t, map[string]any{"id": "inst-7"})
			if err != nil {
				t.Fatalf("a complete walk failed the whole collect: %v", err)
			}
			if res.IsError {
				t.Fatalf("a complete walk was refused: %s", resultText(res))
			}
			doc := envelopeOf(t, res)
			if doc["walk_complete"] != true {
				t.Fatalf("fixture control: walk_complete is %v, want true", doc["walk_complete"])
			}
			if got, present := doc["walk_incomplete_reason"]; present {
				t.Errorf("a complete walk's envelope carries walk_incomplete_reason=%v: %s", got, mustJSON(t, doc))
			}
		})
	}
}
