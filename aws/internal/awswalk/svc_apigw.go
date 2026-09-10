// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
)

// svc_apigw.go — API Gateway, which is TWO services and FOUR resource types.
//
// THE V1 AND V2 APIS ARE NOT VERSIONS OF EACH OTHER. v1 serves REST APIs; v2
// serves HTTP and WebSocket APIs, and an account routinely runs both. A collector
// reading only one reports half the account's public surface, which is the half
// of the graph a security review is most interested in — so both are walked, and
// the resource type distinguishes all four kinds.

type apiGatewayAPI interface {
	GetRestApis(ctx context.Context, in *apigateway.GetRestApisInput, optFns ...func(*apigateway.Options)) (*apigateway.GetRestApisOutput, error)
	GetDomainNames(ctx context.Context, in *apigateway.GetDomainNamesInput, optFns ...func(*apigateway.Options)) (*apigateway.GetDomainNamesOutput, error)
	GetBasePathMappings(ctx context.Context, in *apigateway.GetBasePathMappingsInput, optFns ...func(*apigateway.Options)) (*apigateway.GetBasePathMappingsOutput, error)
}

type apiGatewayV2API interface {
	GetApis(ctx context.Context, in *apigatewayv2.GetApisInput, optFns ...func(*apigatewayv2.Options)) (*apigatewayv2.GetApisOutput, error)
}

// restAPIARN composes the ARN for a v1 REST API, which the API returns by id.
//
// AN API GATEWAY ARN CARRIES NO ACCOUNT SEGMENT — it is
// arn:aws:apigateway:<region>::/restapis/<id>, with the account field empty —
// which is why these three helpers take a region and not an account.
func restAPIARN(region, id string) string {
	return fmt.Sprintf("arn:aws:apigateway:%s::/restapis/%s", region, id)
}

// apiV2ARN composes the ARN for a v2 API. See restAPIARN for why it takes no
// account.
func apiV2ARN(region, id string) string {
	return fmt.Sprintf("arn:aws:apigateway:%s::/apis/%s", region, id)
}

// domainARN composes the ARN for a custom domain name. See restAPIARN for why it
// takes no account.
func domainARN(region, name string) string {
	return fmt.Sprintf("arn:aws:apigateway:%s::/domainnames/%s", region, name)
}

func walkAPIGateway(ctx context.Context, w *walkContext) error {
	if err := w.walkRestAPIs(ctx); err != nil {
		return err
	}
	return w.walkAPIDomains(ctx)
}

func (w *walkContext) walkRestAPIs(ctx context.Context) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*apigateway.GetRestApisOutput, error) {
			return w.clients.APIGateway.GetRestApis(ctx, &apigateway.GetRestApisInput{Position: token})
		},
		func(p *apigateway.GetRestApisOutput) *string { return p.Position },
		func(p *apigateway.GetRestApisOutput) error {
			for _, a := range p.Items {
				id := deref(a.Id)
				if id == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "description", deref(a.Description))
				put(detail, "api_id", id)
				w.sink.addNode(newNode(resource{
					id:           restAPIARN(w.region, id),
					resourceType: ResourceTypeAPIGWRestAPI,
					name:         deref(a.Name),
					summary:      fmt.Sprintf("API Gateway REST API %s (%s) in %s", deref(a.Name), id, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
			}
			return nil
		})
}

// walkAPIDomains emits each custom domain and BOUND_TO the API it maps to.
//
// THE MAPPING IS A SECOND CALL PER DOMAIN and there is no batch form. It is paid
// because a domain with no mapping edge is a node nobody can act on: "which API
// answers on api.example.com" is the whole question a custom domain raises.
func (w *walkContext) walkAPIDomains(ctx context.Context) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*apigateway.GetDomainNamesOutput, error) {
			return w.clients.APIGateway.GetDomainNames(ctx, &apigateway.GetDomainNamesInput{Position: token})
		},
		func(p *apigateway.GetDomainNamesOutput) *string { return p.Position },
		func(p *apigateway.GetDomainNamesOutput) error {
			for _, d := range p.Items {
				name := deref(d.DomainName)
				if name == "" {
					continue
				}
				arn := domainARN(w.region, name)
				detail := map[string]string{}
				put(detail, "domain_name", name)
				put(detail, "regional_domain_name", deref(d.RegionalDomainName))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeAPIGWDomain,
					name:         name,
					summary:      fmt.Sprintf("API Gateway custom domain %s in %s", name, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))

				// EITHER CERTIFICATE FIELD MAY CARRY THE ARN depending on the
				// endpoint type — edge-optimized domains use CertificateArn,
				// regional ones RegionalCertificateArn — and a walk reading only
				// one loses the edge for half of them.
				for _, cert := range []string{deref(d.CertificateArn), deref(d.RegionalCertificateArn)} {
					if cert != "" {
						w.sink.addEdge(arn, cert, EdgeUsesCert, map[string]string{"domain": name})
					}
				}
				w.addBasePathMappings(ctx, arn, name)
			}
			return nil
		})
}

// addBasePathMappings emits BOUND_TO from a domain to each API mapped onto it.
//
// THE FAILURE DOES NOT ABORT THE WALK AND IS NOT SWALLOWED: one domain's
// mappings going unread costs that domain's edges, so the read records into the
// account's failure set and the account asserts INCOMPLETE naming this
// operation. Failing the whole walk would cost every other domain's mappings;
// saying nothing would let the next collect delete these.
//
// IT PAGINATES. GetBasePathMappingsOutput carries a Position, which is API
// Gateway's spelling of a continuation token, so a domain with many mappings
// returns a truncated set indistinguishable from a domain with few.
func (w *walkContext) addBasePathMappings(ctx context.Context, domainNodeARN, domain string) {
	err := paginate(ctx,
		func(ctx context.Context, token *string) (*apigateway.GetBasePathMappingsOutput, error) {
			return w.clients.APIGateway.GetBasePathMappings(ctx,
				&apigateway.GetBasePathMappingsInput{DomainName: &domain, Position: token})
		},
		func(p *apigateway.GetBasePathMappingsOutput) *string { return p.Position },
		func(p *apigateway.GetBasePathMappingsOutput) error {
			for _, m := range p.Items {
				apiID := deref(m.RestApiId)
				if apiID == "" {
					continue
				}
				w.sink.addEdge(domainNodeARN, restAPIARN(w.region, apiID), EdgeBoundTo,
					map[string]string{"base_path": deref(m.BasePath), "stage": deref(m.Stage)})
			}
			return nil
		})
	w.recordSubreadFailure("apigateway.GetBasePathMappings", domain, err)
}

// walkAPIGatewayV2 emits HTTP and WebSocket APIs, which the same list returns and
// only the protocol type tells apart.
func walkAPIGatewayV2(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*apigatewayv2.GetApisOutput, error) {
			return w.clients.APIGatewayV2.GetApis(ctx, &apigatewayv2.GetApisInput{NextToken: token})
		},
		func(p *apigatewayv2.GetApisOutput) *string { return p.NextToken },
		func(p *apigatewayv2.GetApisOutput) error {
			for _, a := range p.Items {
				id := deref(a.ApiId)
				if id == "" {
					continue
				}
				resourceType := resourceTypeForProtocol(string(a.ProtocolType))
				if resourceType == "" {
					// A PROTOCOL THIS COLLECTOR DOES NOT KNOW yields no node
					// rather than a node under a guessed type: a consumer's query
					// selects on resource_type, and putting a future protocol
					// under the wrong one is worse than omitting it, which the
					// parity census would catch as a missing type.
					continue
				}
				detail := map[string]string{}
				put(detail, "protocol_type", string(a.ProtocolType))
				put(detail, "endpoint", deref(a.ApiEndpoint))
				put(detail, "api_id", id)
				w.sink.addNode(newNode(resource{
					id:           apiV2ARN(w.region, id),
					resourceType: resourceType,
					name:         deref(a.Name),
					summary: fmt.Sprintf("API Gateway %s API %s (%s) in %s",
						string(a.ProtocolType), deref(a.Name), id, w.region),
					detail: detail,
					region: w.region,
				}, w.account))
			}
			return nil
		})
}

// resourceTypeForProtocol maps a v2 API's protocol onto its resource type.
func resourceTypeForProtocol(protocol string) string {
	switch protocol {
	case "HTTP":
		return ResourceTypeAPIGWHTTPAPI
	case "WEBSOCKET":
		return ResourceTypeAPIGWWSAPI
	default:
		return ""
	}
}
