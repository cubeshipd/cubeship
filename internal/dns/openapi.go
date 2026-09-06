package dns

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	orgParam := openapi.PathParam("orgSlug", "Organization slug.")
	idParam := openapi.PathParam("id", "DNS provider id.")
	zoneParam := openapi.QueryParam("zone", "The provider's own id for the zone, as the zone listing gives it.")

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name: "DNS",
			Description: "The DNS accounts an organization manages its records through — Cloudflare and Route 53. " +
				"Cubeship already asks you to point a name at this host; these credentials are what let that happen here " +
				"rather than in somebody else's control panel.\n\n" +
				"A credential belongs to the organization, not to a domain: one account holds every zone in it. " +
				"Organization admins only, throughout — a DNS credential moves where a name points for every name on " +
				"the account, including names that have nothing to do with Cubeship.",
		}},
		Schemas: map[string]*openapi.Schema{
			"DNSZone": openapi.Object(map[string]*openapi.Schema{
				"id":   openapi.String("The provider's own id for the zone, which is what every later call addresses it by. Not derivable from the name."),
				"name": openapi.String("The domain, lowercase and with no trailing dot."),
			}, "id", "name"),
			"DNSRecord": openapi.Object(map[string]*openapi.Schema{
				"id":     openapi.String("Cloudflare's row id, where there is one. Route 53 addresses a record by its name and type and has none, so nothing should depend on this."),
				"name":   openapi.String("The full name, lowercase and with no trailing dot."),
				"type":   openapi.String("A, AAAA, CNAME, TXT, MX, NS, SRV or CAA."),
				"values": openapi.Array(openapi.String("One value. A record is a list at both providers: two A records for one name are one record with two values here.")),
				"ttl":    openapi.Integer("Seconds. 300 when unset."),
				"proxied": openapi.Bool("Cloudflare only, and only for A, AAAA and CNAME: whether Cloudflare answers for the name itself " +
					"rather than handing back the value. Ignored elsewhere."),
			}, "name", "type", "values", "ttl"),
		},
		Paths: map[string]openapi.PathItem{
			"/dns/{id}/status": {
				"get": {
					OperationID: "probeDNSProvider",
					Summary:     "Ask whether this credential still works",
					Description: "A live call to the provider, not something recorded when the credential was stored. The interesting case is one that used to work — a revoked Cloudflare token, an access key deleted in IAM — neither of which tells Cubeship anything: the first sign would be a record edit failing.\n\n" +
						"`unauthorized` is fixed by storing a new credential; `unreachable` is the provider's API being down.",
					Tags:       []string{"DNS"},
					Parameters: []openapi.Parameter{orgParam, idParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("What the probe found.", openapi.Object(map[string]*openapi.Schema{
							"state": {
								Type:        "string",
								Description: "available, unauthorized, or unreachable.",
								Enum:        []string{"available", "unauthorized", "unreachable"},
							},
							"detail": openapi.String("Why, for anything but available."),
						}, "state")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
			"/dns/{id}/zones": {
				"get": {
					OperationID: "listDNSZones",
					Summary:     "List the domains this credential can reach",
					Description: "Exactly the zones the credential is scoped to, which for a narrowly-scoped Cloudflare token may be one.",
					Tags:        []string{"DNS"},
					Parameters:  []openapi.Parameter{orgParam, idParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The zones.", openapi.Array(openapi.Ref("DNSZone"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
			"/dns/{id}/records": {
				"get": {
					OperationID: "listDNSRecords",
					Summary:     "List a zone's records",
					Description: "Cloudflare stores one row per value and Route 53 one record per name and type; both come back in the second shape, because a set is what a person edits.\n\n" +
						"A Route 53 alias — a record pointing at an AWS resource, with no value of its own — is listed with its target prefixed `ALIAS `, so the zone adds up. It is not something this API can write.",
					Tags:       []string{"DNS"},
					Parameters: []openapi.Parameter{orgParam, idParam, zoneParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The records.", openapi.Array(openapi.Ref("DNSRecord"))),
						"400": openapi.TextResponse("No zone was named."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"put": {
					OperationID: "putDNSRecord",
					Summary:     "Write a record",
					Description: "Creates it, or replaces whatever is at that name and type. One operation rather than a create and an update, because that is what one of the two providers offers: Route 53's UPSERT replaces the set whole, and splitting it would be two names for one call there and a race between them at Cloudflare.\n\n" +
						"**It replaces every value at that name and type.** Sending one value where there were three leaves one. Read the record first if you meant to add to it.",
					Tags:        []string{"DNS"},
					Parameters:  []openapi.Parameter{orgParam, idParam, zoneParam},
					RequestBody: openapi.Body(openapi.Ref("DNSRecord")),
					Responses: openapi.Responses{
						"204": openapi.Empty("Written."),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"delete": {
					OperationID: "deleteDNSRecord",
					Summary:     "Delete a record",
					Description: "Everything at that name and type goes. Whatever was resolving through it stops.",
					Tags:        []string{"DNS"},
					Parameters: []openapi.Parameter{
						orgParam, idParam, zoneParam,
						openapi.QueryParam("name", "The record's full name."),
						openapi.QueryParam("type", "Its type."),
					},
					Responses: openapi.Responses{
						"204": openapi.Empty("Deleted."),
						"400": openapi.TextResponse("The zone, the name or the type was missing."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
		},
	}
}
