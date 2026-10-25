module github.com/fulminate-io/knowledge-contrib/common/cdpattach

go 1.26.4

toolchain go1.27.3

// Dependency-purity lock: this module is imported BY the collectors that read
// through the user's own running Chrome, and it imports nothing of ours except
// the collector framework, whose EnvDeclaration type is the shape of the
// environment entries it exports. It MUST NOT require cmd/knowledge,
// cmd/knowledge-server, the root module or any cmd/collectors/<provider> or
// cmd/collectors-internal/<provider> module.
//
// github.com/gobwas/ws is the one third-party require: the websocket client the
// DevTools connection rides.
require (
	github.com/fulminate-io/knowledge-contrib/framework v0.0.0
	github.com/gobwas/ws v1.4.0
)

require (
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/modelcontextprotocol/go-sdk v1.7.0 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

replace github.com/fulminate-io/knowledge-contrib/framework => ../../framework
