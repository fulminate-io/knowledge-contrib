module github.com/fulminate-io/knowledge-contrib/stackdriver

go 1.26.4

toolchain go1.26.8

// Dependency-purity lock: this module requires the Cloud Logging read client,
// the compressor the log-chunk payload format is defined in, and the collector
// framework. It MUST NOT require cmd/knowledge, cmd/knowledge-server or the
// root module's gen/ package — the collector contract crossing is JSON over
// MCP, not a Go type, and the client's own half of it lives under
// cmd/knowledge/internal, which the toolchain refuses to import from here.
// cmd/collectors/stackdriver/census_test.go asserts all three refusals and the
// framework's admission, in both directions.
//
// The framework is consumed with a `require` AND a `replace ../framework`. The
// bare workspace-resolved import builds but cannot be tidied and writes no
// go.sum, and the CI cache family names that go.sum by explicit path. The common
// correlation module is consumed the same way, through `../common/correlation`:
// it is NOT a sibling collector but shared code every logs collector requires,
// and it imports nothing under our own module root except the framework.
//
// TWO OF THE DIRECT REQUIRES ARE THE SUITE'S RATHER THAN THE BINARY'S, which is
// worth saying because neither appears in a non-test file: the MCP SDK is the
// CLIENT side, used by the arm that spawns this test binary as a real stdio
// collector and speaks JSON-RPC to it, and the monitored-resource proto is the
// type a recorded Cloud Logging entry's Resource field carries. The serving side
// of MCP reaches this module only through the framework.
require (
	cloud.google.com/go/logging v1.15.0
	github.com/fulminate-io/knowledge-contrib/common/correlation v0.0.0
	github.com/fulminate-io/knowledge-contrib/framework v0.0.0
	github.com/klauspost/compress v1.18.7
	github.com/modelcontextprotocol/go-sdk v1.7.0
	google.golang.org/api v0.275.0
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af
)

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.8.0 // indirect
	cloud.google.com/go/longrunning v0.9.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.14 // indirect
	github.com/googleapis/gax-go/v2 v2.21.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.67.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.67.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/genproto v0.0.0-20260319201613-d00831a3d3e7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.2 // indirect
)

replace github.com/fulminate-io/knowledge-contrib/framework => ../framework

replace github.com/fulminate-io/knowledge-contrib/common/correlation => ../common/correlation
