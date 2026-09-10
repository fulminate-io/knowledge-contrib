// SPDX-License-Identifier: Apache-2.0

package azure

import "testing"

// subs_test.go — the subcollector set, which is the walk's coverage of Azure's
// services stated as a list.

// TestBuildSubCollectors_IsTheDeclaredSetInTheDeclaredOrder.
//
// THE ORDER IS LOAD-BEARING, not cosmetic: the runner fans the subcollectors
// out concurrently and merges their results in this order, so it is what makes
// two collects of an unchanged subscription produce the same sequence.
func TestBuildSubCollectors_IsTheDeclaredSetInTheDeclaredOrder(t *testing.T) {
	subs := buildSubCollectors(fakeCredential{}, "sub-1")
	if len(subs) != len(subCollectorNames) {
		t.Fatalf("the walk builds %d subcollectors and declares %d", len(subs), len(subCollectorNames))
	}
	for i, sub := range subs {
		if got := sub.Name(); got != subCollectorNames[i] {
			t.Errorf("subcollector %d is %q, declared as %q", i, got, subCollectorNames[i])
		}
	}
	if len(subs) != 36 {
		t.Errorf("the walk runs %d subcollectors; the parity floor is 36 Azure services", len(subs))
	}

	// Every name is distinct: an incomplete walk names the subcollectors that
	// failed, and two sharing a name would be one line an operator cannot act
	// on.
	seen := map[string]bool{}
	for _, name := range subCollectorNames {
		if seen[name] {
			t.Errorf("two subcollectors are named %q", name)
		}
		seen[name] = true
	}
}

// TestBuildSubCollectors_EveryOneCarriesTheSubscriptionAndTheCredential. A
// subcollector built without either would fail at its first call with an error
// about Azure rather than about its construction.
func TestBuildSubCollectors_EveryOneCarriesTheSubscriptionAndTheCredential(t *testing.T) {
	const subscription = "0000-1111"
	for _, sub := range buildSubCollectors(fakeCredential{}, subscription) {
		base, ok := baseOf(sub)
		if !ok {
			t.Errorf("%s does not embed the shared subcollector state", sub.Name())
			continue
		}
		if base.subscriptionID != subscription {
			t.Errorf("%s walks %q rather than the subscription it was built for", sub.Name(), base.subscriptionID)
		}
		if base.cred == nil {
			t.Errorf("%s carries no credential", sub.Name())
		}
	}
}

// baseOf reads the shared state out of a subcollector, for the row above.
func baseOf(sub subCollector) (subBase, bool) {
	type embedder interface{ shared() subBase }
	if e, ok := sub.(embedder); ok {
		return e.shared(), true
	}
	return subBase{}, false
}
