// SPDX-License-Identifier: Apache-2.0

package awswalk

import "strings"

// derive_certs.go — VALIDATED_BY, from an ACM certificate to the Route 53 hosted
// zone that proves its domain.
//
// IT IS A DERIVED PASS FOR THE SAME REASON PEERING IS: the two sides come from
// two service walks that run concurrently, so a match attempted inside either one
// sees whichever half of the account happened to arrive first. Everything here
// runs after the fan-out has drained.
//
// THE MATCH IS AN INFERENCE AND IS LABELED AS ONE. Nothing in either API states
// the relationship: ACM names a domain, Route 53 names a zone, and the only thing
// connecting them is that one is a suffix of the other. A certificate whose domain
// matches no zone in this account yields no edge, which is the right answer for a
// domain hosted somewhere else.

// certificateRecord is one certificate's ARN and the DNS-validated domains it
// carries.
type certificateRecord struct {
	arn     string
	domains []string
}

// hostedZoneRecord is one hosted zone's node id and its DNS name.
type hostedZoneRecord struct {
	id   string
	name string
}

func (w *walkContext) recordCertificate(arn string, domains []string) {
	if arn == "" || len(domains) == 0 {
		return
	}
	w.derived.mu.Lock()
	defer w.derived.mu.Unlock()
	w.derived.certificates = append(w.derived.certificates, certificateRecord{arn: arn, domains: domains})
}

func (w *walkContext) recordHostedZone(id, name string) {
	if id == "" || name == "" {
		return
	}
	w.derived.mu.Lock()
	defer w.derived.mu.Unlock()
	w.derived.hostedZones = append(w.derived.hostedZones, hostedZoneRecord{id: id, name: name})
}

func (w *walkContext) deriveCertificateValidation() {
	for _, c := range w.derived.certificates {
		for _, domain := range c.domains {
			zone := w.hostedZoneForDomain(domain)
			if zone == "" {
				continue
			}
			w.sink.addEdge(c.arn, zone, EdgeValidatedBy,
				map[string]string{"domain": domain, "method": "DNS", "match": "zone-name-suffix"})
		}
	}
}

// hostedZoneForDomain returns the node id of the hosted zone whose name is the
// LONGEST suffix of domain, or empty when none is.
//
// LONGEST WINS because an account holding both example.com and
// internal.example.com must attribute a certificate for api.internal.example.com
// to the more specific zone, which is the one that actually serves it. A shortest
// or first match would attribute it to the apex and say the wrong zone proves it.
//
// THE TRAILING DOT IS STRIPPED: Route 53 returns a zone name in fully-qualified
// form (example.com.) and a certificate domain is not, so a comparison without it
// matches nothing at all.
func (w *walkContext) hostedZoneForDomain(domain string) string {
	best, bestLen := "", 0
	for _, z := range w.derived.hostedZones {
		suffix := strings.TrimSuffix(z.name, ".")
		if suffix == "" {
			continue
		}
		if domain == suffix || strings.HasSuffix(domain, "."+suffix) {
			if len(suffix) > bestLen {
				best, bestLen = z.id, len(suffix)
			}
		}
	}
	return best
}
