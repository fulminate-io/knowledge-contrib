module github.com/fulminate-io/knowledge-contrib/loki

go 1.26.4

toolchain go1.26.8

// Dependency-purity lock: this module is a CONTRIB COLLECTOR. It requires the
// collector framework and nothing else of ours — not the root contract module,
// not cmd/knowledge, not cmd/knowledge-server. The collector contract crossing
// is JSON over MCP, not a Go type, and the client's own implementation of it
// lives under cmd/knowledge/internal, which the toolchain refuses to import
// from here. census_test.go asserts that by parsing this module's own sources,
// because a workspace build resolves such an import happily.
//
// The framework arrives by a `require` AND a `replace ../framework`. The bare
// workspace-resolved import builds but cannot be tidied and writes no go.sum,
// and the CI cache family names this module's go.sum by explicit path. The
// common correlation module is consumed the same way, through
// `../common/correlation`: it is NOT a sibling collector but shared code every
// logs collector requires, and it imports nothing under our own module root
// except the framework.
//
// github.com/klauspost/compress is a NON-TEST dependency and is forced: a
// log-chunk's Content is a zstd frame, and Go's standard library carries no
// usable zstd (internal/zstd is decode-only and importable only inside std).
// It is pinned at v1.18.5 to match the client module the parity target is
// measured against.
require (
	github.com/fulminate-io/knowledge-contrib/common/correlation v0.0.0
	github.com/fulminate-io/knowledge-contrib/framework v0.0.0
	github.com/klauspost/compress v1.18.5
)

require github.com/modelcontextprotocol/go-sdk v1.7.0

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

replace github.com/fulminate-io/knowledge-contrib/framework => ../framework

replace github.com/fulminate-io/knowledge-contrib/common/correlation => ../common/correlation
