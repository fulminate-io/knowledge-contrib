module github.com/fulminate-io/knowledge-contrib/common/correlation

go 1.26.4

toolchain go1.26.8

// Dependency-purity lock: this module is imported BY logs collectors and imports
// NOTHING of ours except the collector framework, whose Edge type is the shape
// its materializer emits. It MUST NOT require cmd/knowledge, cmd/knowledge-server
// or the root module's gen/ package — the collector contract crossing is JSON
// over MCP, not a Go type — and it MUST NOT require any cmd/collectors/<provider>
// module. A collector reaching a sibling THROUGH this module would be the very
// coupling this module exists to prevent, and it is the reason nothing here is
// provider-specific: the detector takes the caller's own data as values.
// scripts/collectors-isolation-census.sh asserts the sibling rule over the whole
// tree, in both directions.
//
// The framework is consumed with a `require` AND a `replace ../../framework`,
// two levels up because this module is nested one deeper than a provider
// collector. The bare workspace-resolved import builds but cannot be tidied and
// writes no go.sum, and the CI cache family names that go.sum by explicit path.
require github.com/fulminate-io/knowledge-contrib/framework v0.0.0

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/modelcontextprotocol/go-sdk v1.7.0 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

replace github.com/fulminate-io/knowledge-contrib/framework => ../../framework
