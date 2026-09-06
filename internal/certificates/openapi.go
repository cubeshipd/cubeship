package certificates

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name: "Certificates",
			Description: "What TLS certificates this instance holds, and which of the names it serves have none.\n\n" +
				"Traefik issues them through Let's Encrypt and keeps them in its own store; this reads that store. " +
				"Nothing here issues, renews or deletes anything — renewal is automatic, thirty days before expiry.",
		}},
		Schemas: map[string]*openapi.Schema{
			"Certificate": openapi.Object(map[string]*openapi.Schema{
				"host":       openapi.String("The name it was issued for."),
				"sans":       openapi.Array(openapi.String("Any other name on the same certificate.")),
				"issuer":     openapi.String("The CA that signed it."),
				"not_before": openapi.String("RFC 3339."),
				"not_after":  openapi.String("RFC 3339. Traefik renews thirty days before this."),
				"serial":     openapi.String("The CA's serial, hex, colon separated."),
				"app":        openapi.String("The app served at that name, as its reference. Absent for the instance's own names."),
				"instance":   openapi.Bool("The name is the dashboard's or the registry's rather than an app's."),
				"orphan":     openapi.Bool("Nothing on this instance answers at that name any more. The certificate is still valid; it is simply unused."),
			}, "host", "issuer", "not_before", "not_after", "serial"),
			"MissingCertificate": openapi.Object(map[string]*openapi.Schema{
				"host":     openapi.String("A name this instance routes with no certificate behind it."),
				"app":      openapi.String("The app served there, as its reference. Absent for the instance's own names."),
				"instance": openapi.Bool("The name is the dashboard's or the registry's."),
				"reason": openapi.String("`tls_not_configured` — the instance has no domain, so Traefik asks for nothing. " +
					"`not_deployed` — nothing is running with that name in its labels: an app not deployed since the name was added, or the registry's container made before the instance had a domain. A container keeps the labels it was created with, so Traefik has never been told about it. " +
					"`pending` — Traefik knows the name and has not produced a certificate: normal for a minute after a deploy, and after that a name that does not resolve here or a challenge that failed."),
				"detail": openapi.String("The last thing Traefik's log said about that name, when it said anything. A quotation, not a contract."),
			}, "host", "reason"),
			"CertificateReport": openapi.Object(map[string]*openapi.Schema{
				"tls_enabled":  openapi.Bool("Whether certificates are possible at all, which needs both a domain and a contact address."),
				"acme_email":   openapi.String("The contact Let's Encrypt has, as the store recorded it — which is the one that counts if the setting was changed after registering."),
				"certificates": openapi.Array(openapi.Ref("Certificate")),
				"missing":      openapi.Array(openapi.Ref("MissingCertificate")),
				"traefik_says": openapi.Array(openapi.String("A line Traefik logged while failing to get a certificate, whether or not it names a host. Often the only place the real reason appears — a rate limit especially, which on a default install is shared with everyone else using the same wildcard DNS service.")),
			}, "tls_enabled", "certificates", "missing"),
		},
		Paths: map[string]openapi.PathItem{
			"/certificates": {
				"get": {
					OperationID: "getCertificates",
					Summary:     "List this instance's TLS certificates",
					Description: "Every certificate Traefik holds, with what it is for and when it runs out, plus every name this instance routes that has none and why.\n\n" +
						"Admin only: this is how the instance is wired, not an app's own configuration. Private keys are in the same store and are never read.",
					Tags: []string{"Certificates"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The report.", openapi.Ref("CertificateReport")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
			},
		},
	}
}
