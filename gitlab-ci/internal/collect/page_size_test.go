// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
)

// page_size_test.go — the ABSOLUTE claim about the page size, kept apart from
// the boundary table because it is a different kind of statement.
//
// THE TABLE'S CLAIM IS RELATIVE: whatever size the collector asks for, it pages
// to exhaustion on the provider's marker and its arms sit at that size's own
// boundary. That holds at any size — which is why it cannot be the only thing
// said, since a collector asking for seven items a page would satisfy every row
// while making seventeen round trips where it needs one.

// TestThePageSizeIsTheProvidersOwnMaximum is the one ABSOLUTE claim about the
// page size, and it is deliberately separate from the table.
//
// THE TABLE ASSERTS A RELATIVE PROPERTY: whatever size the collector asks for, it
// pages to exhaustion on the provider's marker and the arms sit at that size's own
// boundary. That property holds at any size, which is exactly why it cannot be the
// only thing said — a collector that asked for seven items a page would satisfy
// every row while making seventeen round trips where it needs one.
//
// SO THE ABSOLUTE CLAIM IS MADE HERE, ONCE, AGAINST AN EXTERNAL FACT rather than
// against another of our own constants: 100 is the maximum per_page the GitLab API
// documents, and asking for less is asking for the same items in more requests.
// This is the one place the number is written down as a number, and it is written
// against the provider's documentation rather than copied from the collector.
func TestThePageSizeIsTheProvidersOwnMaximum(t *testing.T) {
	const providersDocumentedMaximum = 100
	if collect.PageSizeForTest != providersDocumentedMaximum {
		t.Errorf("every paginated read asks for %d items a page; the provider's documented maximum "+
			"is %d, and a smaller page is the same items in more round trips against a host that "+
			"counts them", collect.PageSizeForTest, providersDocumentedMaximum)
	}
}
