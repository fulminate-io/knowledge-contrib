// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
)

// svc_ses.go — SES identities.
//
// SESv2 CARRIES NO RECEIPT-RULE OPERATION, so this collector emits no
// ses-receipt-rule node and the parity floor records that type as a GAP rather
// than as coverage. Receipt rules live in the v1 `ses` package, which this module
// does not depend on; the built-in collector reads them there through
// DescribeActiveReceiptRuleSet. Verified against the pinned SDK rather than
// against documentation: in
// $GOMODCACHE/github.com/aws/aws-sdk-go-v2/service/sesv2@v1.72.0,
// `ls api_op_*Receipt*` matches nothing while the same-run control
// `ls api_op_*Template*` returns ten operations.
//
// THE GAP IS DELIBERATE AND IT IS THE OWNER'S CALL. Adding the v1 SDK to reach
// parity was considered and declined; substituting a different object to keep the
// count green — emitting email templates under the receipt-rule type — is the
// option that is never available, and it is what an earlier revision of this file
// did.

func walkSES(ctx context.Context, w *walkContext) error {
	return w.walkSESIdentities(ctx)
}

func (w *walkContext) walkSESIdentities(ctx context.Context) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*sesv2.ListEmailIdentitiesOutput, error) {
			return w.clients.SES.ListEmailIdentities(ctx, &sesv2.ListEmailIdentitiesInput{NextToken: token})
		},
		func(p *sesv2.ListEmailIdentitiesOutput) *string { return p.NextToken },
		func(p *sesv2.ListEmailIdentitiesOutput) error {
			for _, id := range p.EmailIdentities {
				name := deref(id.IdentityName)
				if name == "" {
					continue
				}
				arn := fmt.Sprintf("arn:aws:ses:%s:%s:identity/%s", w.region, w.account, name)
				detail := map[string]string{}
				put(detail, "identity_type", string(id.IdentityType))
				detail["sending_enabled"] = fmt.Sprintf("%t", id.SendingEnabled)
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeSESIdentity,
					name:         name,
					summary:      fmt.Sprintf("SES identity %s in %s", name, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
			}
			return nil
		})
}
