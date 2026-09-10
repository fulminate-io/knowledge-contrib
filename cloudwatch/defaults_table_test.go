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
		{"DefaultCardinalityThreshold", DefaultCardinalityThreshold, builtinCardinalityThreshold},
		{"DefaultChunkWindow", DefaultChunkWindow, builtinChunkWindow},
		{"DrainConfig.SimThreshold", cfg.SimThreshold, builtinSimThreshold},
		{"DrainConfig.MaxDepth", cfg.MaxDepth, builtinMaxDepth},
		{"DrainConfig.MaxChildren", cfg.MaxChildren, builtinMaxChildren},
		{"DrainConfig.MaxClusters", cfg.MaxClusters, builtinMaxClusters},
		{"maxExampleVars", maxExampleVars, builtinMaxExampleVars},
		{"templateAliasTokenLimit", templateAliasTokenLimit, builtinAliasTokenLimit},
		{"goStackWindow", goStackWindow, builtinGoStackWindow},
		{"goStackMinGroup", goStackMinGroup, builtinGoStackMinGroup},
		{"pythonWindow", pythonWindow, builtinPythonWindow},
		{"pythonMinGroup", pythonMinGroup, builtinPythonMinGroup},
		{"pythonHeaderLimit", pythonHeaderLimit, builtinPythonHeaderLimit},
		{"streamNameLimit", streamNameLimit, builtinStreamNameLimit},
		{"streamNameKeep", streamNameKeep, builtinStreamNameKeep},
		{"severityPrefixMaxLen", severityPrefixMaxLen, builtinSeverityPrefixMaxLen},
		{"maxJSONNesting", maxJSONNesting, builtinMaxJSONNesting},
		{"embeddedSeverityScanLimit", embeddedSeverityScanLimit, builtinSeverityScanLimit},
		{"pageLimit", pageLimit, builtinPageLimit},
	} {
		if tc.got != tc.want {
			t.Errorf("%s is %v, and the built-in pipeline's value is %v; a divergence moves derived "+
				"identifiers off the parity target", tc.name, tc.got, tc.want)
		}
	}
}
