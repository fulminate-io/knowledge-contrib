// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"
)

// defaults_table_test.go — the DECLARATION half of the production-defaults
// pinning: each constant against the built-in pipeline's own literal.
//
// It sits beside the behavioral cells rather than replacing them. A behavioral
// cell says what a value DOES and is the stronger evidence; this table says what
// it IS, and catches a constant that arrives before its cell does.

// TestEveryProductionDefaultMatchesItsBuiltinValue is the declaration half: each
// constant against the literal above. It is what makes a drift in a value no
// behavioral cell can straddle — there are none left, but a future constant may
// arrive before its cell does — a red rather than a silence.
func TestEveryProductionDefaultMatchesItsBuiltinValue(t *testing.T) {
	cfg := DefaultDrainConfig()
	for _, tc := range []struct {
		name string
		got  any
		want any
	}{
		{name: "DefaultCardinalityThreshold", got: DefaultCardinalityThreshold, want: builtinCardinalityThreshold},
		{name: "DefaultChunkWindow", got: DefaultChunkWindow, want: builtinChunkWindow},
		{name: "DrainConfig.SimThreshold", got: cfg.SimThreshold, want: builtinSimThreshold},
		{name: "DrainConfig.MaxDepth", got: cfg.MaxDepth, want: builtinMaxDepth},
		{name: "DrainConfig.MaxChildren", got: cfg.MaxChildren, want: builtinMaxChildren},
		{name: "DrainConfig.MaxClusters", got: cfg.MaxClusters, want: builtinMaxClusters},
		{name: "maxExampleVars", got: maxExampleVars, want: builtinMaxExampleVars},
		{name: "templateAliasTokenLimit", got: templateAliasTokenLimit, want: builtinAliasTokenLimit},
		{name: "goStackWindow", got: goStackWindow, want: builtinGoStackWindow},
		{name: "goStackMinGroup", got: goStackMinGroup, want: builtinGoStackMinGroup},
		{name: "pythonWindow", got: pythonWindow, want: builtinPythonWindow},
		{name: "pythonMinGroup", got: pythonMinGroup, want: builtinPythonMinGroup},
		{name: "pythonHeaderLimit", got: pythonHeaderLimit, want: builtinPythonHeaderLimit},
		{name: "streamNameLimit", got: streamNameLimit, want: builtinStreamNameLimit},
		{name: "streamNameKeep", got: streamNameKeep, want: builtinStreamNameKeep},
		{name: "severityPrefixMaxLen", got: severityPrefixMaxLen, want: builtinSeverityPrefixMaxLen},
		{name: "maxJSONNesting", got: maxJSONNesting, want: builtinMaxJSONNesting},
		{name: "embeddedSeverityScanLimit", got: embeddedSeverityScanLimit, want: builtinSeverityScanLimit},
		{name: "pageLimit", got: pageLimit, want: builtinPageLimit},
	} {
		if tc.got != tc.want {
			t.Errorf("%s is %v, and the built-in pipeline's value is %v; a divergence moves derived "+
				"identifiers off the parity target", tc.name, tc.got, tc.want)
		}
	}
}
