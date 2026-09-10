module github.com/fulminate-io/knowledge-contrib/framework

go 1.26.4

toolchain go1.26.8

// Dependency-purity lock: this module is imported BY every custom collector
// module and imports NOTHING of ours. It requires neither the root contract
// module, nor cmd/knowledge, nor cmd/knowledge-server — the collector contract
// crossing is JSON over MCP, not a Go type, and the client's own
// implementation of it lives under cmd/knowledge/internal, which the toolchain
// refuses to import from here.
//
// A collector module consumes this one with a `require` AND a
// `replace ../framework`. The bare workspace-resolved import builds but cannot
// be tidied and writes no go.sum, and the CI cache family names that go.sum by
// explicit path.
require (
	github.com/google/jsonschema-go v0.4.3
	github.com/modelcontextprotocol/go-sdk v1.7.0
	go.uber.org/goleak v1.3.0
)

require (
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)
