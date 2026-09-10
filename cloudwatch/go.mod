module github.com/fulminate-io/knowledge-contrib/cloudwatch

go 1.26.4

toolchain go1.26.8

// Dependency-purity lock: this module requires the collector framework, the
// MCP Go SDK (transitively, through the framework) and exactly three
// aws-sdk-go-v2 modules — core, config and service/cloudwatchlogs.
//
// It MUST NOT require cmd/knowledge, cmd/knowledge-server or the root contract
// module: the collector contract crossing is JSON over MCP, never a Go type, so
// this module needs no gen/ dependency at all. The client-side implementation of
// that contract lives under cmd/knowledge/internal, which the toolchain refuses
// to import from here.
//
// The common correlation module is consumed the same way, through
// `../common/correlation`: it is NOT a sibling collector but shared code every
// logs collector requires, and the isolation census admits cmd/collectors/common/*
// on exactly that ground.
//
// It MUST NOT IMPORT aws-sdk-go-v2/credentials. That module exists for the
// static-credentials provider a configuration record can populate, and this
// collector reads credentials ONLY from the AWS default chain (ticket
// requirement R4), so a static provider would be a second credential route the
// requirement forbids. It appears below as an INDIRECT dependency and that is
// not a violation and not avoidable: the configuration package builds the
// single-sign-on and web-identity providers out of it while resolving the chain.
// The rule is about what this module imports, which purity_test.go enforces.
require (
	github.com/aws/aws-sdk-go-v2 v1.41.5
	github.com/aws/aws-sdk-go-v2/config v1.32.14
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.68.0
	github.com/fulminate-io/knowledge-contrib/common/correlation v0.0.0
	github.com/fulminate-io/knowledge-contrib/framework v0.0.0
	github.com/modelcontextprotocol/go-sdk v1.7.0
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.8 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.14 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.21 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.41.10 // indirect
	github.com/aws/smithy-go v1.24.2 // indirect
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
