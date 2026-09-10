// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"encoding/json"
	"fmt"
	"slices"

	_ "embed"

	"github.com/google/jsonschema-go/jsonschema"
)

// schema.go — the SCHEMAS THIS FRAMEWORK ADVERTISES, and why they are the
// checked-in contract files rather than schemas inferred from the Go types.
//
// THE CONTRACT SCHEMAS ARE CHECKED-IN JSON, here as they are on the client
// side, because a collector author writing a provider in another language reads
// the file. The two files below are a COPY of the client's
// cmd/knowledge/internal/externalcollector/contract pair: that package is
// client-internal and the toolchain refuses to import it from another module,
// and go:embed refuses a symlinked directory, so a copy is the only shape that
// embeds. The copy is kept honest by a test that opens the client's files
// through this module's own testdata symlink and compares the bytes, which is
// also that test's cache fence.
//
// WHY THE OUTPUT SCHEMA IS THE FILE AND NOT THE INFERRED ONE. The SDK's generic
// AddTool will infer an output schema from the Out type, and that inferred
// schema is REFUSED by the client's registration gate. jsonschema-go infers a
// Go slice as the UNION type ["null","array"], because a nil slice marshals to
// null; the client's comparator reads a scalar Type field, finds it empty on a
// union, and reports "the tool declares no declared type". Advertising the
// contract file verbatim closes that for every collector at once.
//
// WHY THE INPUT SCHEMA IS THE FILE WITH ONE PROPERTY SPLICED IN. The collector's
// own params schema has to reach the caller, or nothing validates a collect's
// params; but inferring the WHOLE input from a Go struct puts `params` on the
// advertised top-level required list, and the client omits params from the call
// arguments entirely when a collect carries none — so a required `params` would
// refuse every paramless collect before it was sent. Building the input from the
// contract file keeps its required list at ["id"] by construction.

//go:embed contract/collector_input.schema.json
var inputContractJSON []byte

//go:embed contract/collector_output.schema.json
var outputContractJSON []byte

//go:embed contract/collector_describe.schema.json
var describeContractJSON []byte

// InputContractJSON, OutputContractJSON and DescribeContractJSON expose this
// module's checked-in copies of the collector contract schemas verbatim. They
// return a clone, so a caller cannot mutate the embedded bytes.
func InputContractJSON() []byte    { return slices.Clone(inputContractJSON) }
func OutputContractJSON() []byte   { return slices.Clone(outputContractJSON) }
func DescribeContractJSON() []byte { return slices.Clone(describeContractJSON) }

// advertisedDescribeSchema is the checked-in describe contract schema VERBATIM,
// handed to the SDK as a json.RawMessage so the bytes reach the wire unchanged.
// The reason it is the file rather than a schema inferred from [Declaration] is
// the one stated at the top of this file: an inferred schema's union types are
// refused by the client's comparator.
func advertisedDescribeSchema() any { return json.RawMessage(DescribeContractJSON()) }

// describeInputSchema is the describe tool's advertised input: an object with no
// properties and nothing required. It is written here rather than inferred so
// the tool advertises the same empty object on both transports and in every SDK
// version, and so a caller reading the listing sees that describe takes no
// arguments rather than having to infer it from an absent schema.
func describeInputSchema() any {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}

// advertisedOutputSchema is the contract output schema VERBATIM. It is handed
// to the SDK as a json.RawMessage so the bytes reach the wire unchanged rather
// than being rebuilt from a decoded map.
func advertisedOutputSchema() any { return json.RawMessage(OutputContractJSON()) }

// advertisedInputSchema is the contract input schema with properties.params
// replaced by the schema inferred from the collector's params type P.
//
// The document is decoded into a mutable map rather than into the SDK's schema
// type: the splice is one property replacement, and a map keeps every keyword
// the contract file carries that neither this package nor the client names.
func advertisedInputSchema[P any]() (any, error) {
	params, err := paramsSchemaDocument[P]()
	if err != nil {
		return nil, err
	}

	var doc map[string]any
	if err := json.Unmarshal(inputContractJSON, &doc); err != nil {
		return nil, fmt.Errorf("framework: this module's checked-in input contract schema does not decode: %w", err)
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("framework: this module's checked-in input contract schema declares no properties object; the copy is corrupt")
	}
	if _, ok := props["params"]; !ok {
		return nil, fmt.Errorf("framework: this module's checked-in input contract schema declares no params property; the copy is corrupt")
	}
	props["params"] = params
	return doc, nil
}

// paramsSchemaDocument infers the JSON Schema of the collector's params type and
// returns it as a decoded document.
//
// IT REFUSES A PARAMS TYPE WHOSE SCHEMA IS NOT AN OBJECT, naming the type. The
// contract declares params as an object and the client's registration gate
// compares that type, so a collector declaring `string` or `any` for its params
// would build here and be refused at registration with an error about a schema
// it never wrote. A collector with no parameters declares an empty struct or a
// map, both of which infer to an object.
func paramsSchemaDocument[P any]() (map[string]any, error) {
	inferred, err := jsonschema.For[P](nil)
	if err != nil {
		var zero P
		return nil, fmt.Errorf("framework: the params type %T does not translate to a JSON Schema: %w", zero, err)
	}
	raw, err := json.Marshal(inferred)
	if err != nil {
		var zero P
		return nil, fmt.Errorf("framework: the inferred schema for params type %T does not marshal: %w", zero, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		var zero P
		return nil, fmt.Errorf("framework: the inferred schema for params type %T does not decode: %w", zero, err)
	}
	if doc["type"] != "object" {
		var zero P
		return nil, fmt.Errorf(
			"framework: the params type %T infers the JSON Schema type %v, and the collector contract declares params as an object; "+
				"declare a struct or a map[string]... params type (an empty struct is the shape for a collector with no parameters)",
			zero, doc["type"])
	}
	return doc, nil
}
