module github.com/fulminate-io/knowledge-contrib/github-actions

go 1.26.4

toolchain go1.26.8

// Dependency-purity lock: this module is a GREENFIELD MCP collector. It speaks
// the collector contract over MCP and it does NOT speak the knowledge wire, so
// the ONLY thing of ours it may require is the collector framework:
//
//	require  github.com/fulminate-io/knowledge-contrib/framework
//	replace  github.com/fulminate-io/knowledge-contrib/framework => ../framework
//
// It MUST NOT require the root `github.com/fulminate-io/knowledge` contract
// module, cmd/knowledge, or cmd/knowledge-server. The import half of that rule
// enforces itself — Go refuses another module's `internal` outright — but the
// `require` half does not, which is why modpurity_test.go parses this file and
// asserts it. Requiring the client would put the collector back inside the
// binary this project exists to take it out of.
require (
	github.com/fulminate-io/knowledge-contrib/framework v0.0.0
	github.com/google/go-github/v68 v68.0.0
	golang.org/x/oauth2 v0.36.0
)

require (
	github.com/modelcontextprotocol/go-sdk v1.7.0
	golang.org/x/mod v0.41.0
)

require (
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

replace github.com/fulminate-io/knowledge-contrib/framework => ../framework
