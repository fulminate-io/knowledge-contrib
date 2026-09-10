// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"slices"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// describe_env.go — WHAT AN INSTALLER MUST DO WITH EVERY ENVIRONMENT NAME THIS
// COLLECTOR READS, and the describe tool's environment declaration built from
// it.
//
// THE DECISION LIVES HERE, BESIDE THE COLLECTOR THAT READS THE NAME. It used to
// live in the installer as a hand-written per-collector table and in a
// disposition file beside it; both are now DERIVED from this map, so a variable
// this collector starts reading is one decision in one place rather than three
// edits in three repositories' worth of files. The disposition file remains the
// frozen review record, and the environment census pins this map against it.
//
// THE FOUR DISPOSITIONS ARE THE CONTRACT'S OWN, and each says what an installed
// entry carries: a path literal, a selector literal when set, a secret in NO
// state whatsoever, and not-carried for a name this collector reads that an
// installed entry deliberately does not declare. Declaring the last kind is the
// point: an operator whose variable is absent can tell a decision from an
// oversight.
// EnvDispositions is that decision, one row per name.
var EnvDispositions = map[string]string{
	"AWS_ACCESS_KEY":                             framework.EnvClassNotCarried,
	"AWS_ACCESS_KEY_ID":                          framework.EnvClassSecret,
	"AWS_ACCOUNT_ID":                             framework.EnvClassNotCarried,
	"AWS_ACCOUNT_ID_ENDPOINT_MODE":               framework.EnvClassNotCarried,
	"AWS_AUTH_SCHEME_PREFERENCE":                 framework.EnvClassNotCarried,
	"AWS_CA_BUNDLE":                              framework.EnvClassNotCarried,
	"AWS_CONFIG_FILE":                            framework.EnvClassPath,
	"AWS_CONTAINER_AUTHORIZATION_TOKEN":          framework.EnvClassNotCarried,
	"AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE":     framework.EnvClassNotCarried,
	"AWS_CONTAINER_CREDENTIALS_FULL_URI":         framework.EnvClassNotCarried,
	"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI":     framework.EnvClassNotCarried,
	"AWS_DEFAULTS_MODE":                          framework.EnvClassNotCarried,
	"AWS_DEFAULT_PROFILE":                        framework.EnvClassNotCarried,
	"AWS_DEFAULT_REGION":                         framework.EnvClassNotCarried,
	"AWS_DISABLE_REQUEST_COMPRESSION":            framework.EnvClassNotCarried,
	"AWS_EC2_METADATA_DISABLED":                  framework.EnvClassNotCarried,
	"AWS_EC2_METADATA_SERVICE_ENDPOINT":          framework.EnvClassNotCarried,
	"AWS_EC2_METADATA_SERVICE_ENDPOINT_MODE":     framework.EnvClassNotCarried,
	"AWS_EC2_METADATA_V1_DISABLED":               framework.EnvClassNotCarried,
	"AWS_ENABLE_ENDPOINT_DISCOVERY":              framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL":                           framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_ACM":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_APIGATEWAYV2":              framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_API_GATEWAY":               framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_CLOUDFRONT":                framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_CLOUDTRAIL":                framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_CLOUDWATCH":                framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_CLOUDWATCH_LOGS":           framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_DYNAMODB":                  framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_EC2":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_ECR":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_ECS":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_EFS":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_EKS":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_ELASTICACHE":               framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_ELASTIC_LOAD_BALANCING_V2": framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_EVENTBRIDGE":               framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_IAM":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_KINESIS":                   framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_KMS":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_LAMBDA":                    framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_OPENSEARCH":                framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_RDS":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_REDSHIFT":                  framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_ROUTE_53":                  framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_S3":                        framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_SECRETS_MANAGER":           framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_SESV2":                     framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_SFN":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_SIGNIN":                    framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_SNS":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_SQS":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_SSO":                       framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_SSO_OIDC":                  framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_STS":                       framework.EnvClassNotCarried,
	"AWS_EXECUTION_ENV":                          framework.EnvClassNotCarried,
	"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS":        framework.EnvClassNotCarried,
	"AWS_LOGIN_CACHE_DIRECTORY":                  framework.EnvClassNotCarried,
	"AWS_MAX_ATTEMPTS":                           framework.EnvClassNotCarried,
	"AWS_PROFILE":                                framework.EnvClassSelector,
	"AWS_REGION":                                 framework.EnvClassSelector,
	"AWS_REQUEST_CHECKSUM_CALCULATION":           framework.EnvClassNotCarried,
	"AWS_REQUEST_MIN_COMPRESSION_SIZE_BYTES":     framework.EnvClassNotCarried,
	"AWS_RESPONSE_CHECKSUM_VALIDATION":           framework.EnvClassNotCarried,
	"AWS_RETRY_MODE":                             framework.EnvClassNotCarried,
	"AWS_ROLE_ARN":                               framework.EnvClassNotCarried,
	"AWS_ROLE_SESSION_NAME":                      framework.EnvClassNotCarried,
	"AWS_S3_DISABLE_EXPRESS_SESSION_AUTH":        framework.EnvClassNotCarried,
	"AWS_S3_DISABLE_MULTIREGION_ACCESS_POINTS":   framework.EnvClassNotCarried,
	"AWS_S3_USE_ARN_REGION":                      framework.EnvClassNotCarried,
	"AWS_SDK_UA_APP_ID":                          framework.EnvClassNotCarried,
	"AWS_SECRET_ACCESS_KEY":                      framework.EnvClassSecret,
	"AWS_SECRET_KEY":                             framework.EnvClassNotCarried,
	"AWS_SESSION_TOKEN":                          framework.EnvClassSecret,
	"AWS_SHARED_CREDENTIALS_FILE":                framework.EnvClassPath,
	"AWS_USE_DUALSTACK_ENDPOINT":                 framework.EnvClassNotCarried,
	"AWS_USE_FIPS_ENDPOINT":                      framework.EnvClassNotCarried,
	"AWS_WEB_IDENTITY_TOKEN_FILE":                framework.EnvClassNotCarried,
	"HOME":                                       framework.EnvClassPath,
	"HTTPS_PROXY":                                framework.EnvClassNotCarried,
	"HTTP_PROXY":                                 framework.EnvClassNotCarried,
	"NO_PROXY":                                   framework.EnvClassNotCarried,
	"SSL_CERT_DIR":                               framework.EnvClassNotCarried,
	"SSL_CERT_FILE":                              framework.EnvClassNotCarried,
	"USERPROFILE":                                framework.EnvClassNotCarried,
	"http_proxy":                                 framework.EnvClassNotCarried,
	"https_proxy":                                framework.EnvClassNotCarried,
	"no_proxy":                                   framework.EnvClassNotCarried,
}

// DescribedEnvironment renders the describe tool's environment declaration: every
// name above, sorted, each with its disposition.
//
// SORTED so one unchanged collector renders one byte-identical declaration, and
// so the installer table generated from it does not reorder itself between runs.
func DescribedEnvironment() []framework.EnvDeclaration {
	names := make([]string, 0, len(EnvDispositions))
	for name := range EnvDispositions {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]framework.EnvDeclaration, 0, len(names))
	for _, name := range names {
		out = append(out, framework.EnvDeclaration{Name: name, Class: EnvDispositions[name]})
	}
	return out
}
