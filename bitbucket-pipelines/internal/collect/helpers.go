// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// helpers.go — the small shared conversions, in one place so six converters
// spell them the same way.

// renderContent renders one API value as a node's Content, and reports whether
// the caller may emit the node at all.
//
// THE ERROR IS NOT DISCARDED, AND "IT CANNOT FAIL" IS NOT THE REASON IT MIGHT
// BE. Every value passed here today is one of this package's own API structs,
// whose fields are strings, bools, ints and other such structs, and
// encoding/json cannot fail on those. That stops being true the moment a
// marshaled type gains a channel, a function, a cyclic pointer or a
// json.Marshaler of its own — and the failure mode of that change is neither a
// compile error nor a red test. It is a node emitted with an EMPTY content, in
// production, silently: indistinguishable downstream from a resource the
// provider said nothing about, sent to a summarizer as nothing and indexed as
// nothing, on a collect that reported success.
//
// SO A RESOURCE WHOSE DETAIL CANNOT BE RENDERED IS REFUSED RATHER THAN
// FLATTENED. The failure is recorded against the enumeration's own scope, which
// makes the walk incomplete and names what it dropped; a collect an operator can
// act on is worth more than one more node with nothing in it.
func renderContent(failures *reads, subject string, v any) (string, bool) {
	raw, err := json.Marshal(v)
	if err != nil {
		failures.record(subject, fmt.Errorf("rendering its detail: %w", err))
		return "", false
	}
	return string(raw), true
}

// putIfSet writes a metadata key only when the value is non-empty.
//
// THE DISTINCTION IS REAL RATHER THAN TIDY: a key present and empty and a key
// absent are different inputs to a consumer that filters on the key, and the
// source provider writes these keys conditionally.
func putIfSet(metadata map[string]string, key, value string) {
	if value != "" {
		metadata[key] = value
	}
}

// putIfPositive is putIfSet for the numeric keys, which are omitted at zero and
// below rather than written as "0".
func putIfPositive(metadata map[string]string, key string, value int) {
	if value > 0 {
		metadata[key] = strconv.Itoa(value)
	}
}

// boolText is the spelling a boolean metadata value takes.
func boolText(v bool) string { return strconv.FormatBool(v) }
