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
			"DNSProvider": openapi.Object(map[string]*openapi.Schema{
				"id": openapi.Integer(""),
				"provider": {
					Type:        "string",
					Description: "cloudflare or route53.",
					Enum:        []string{"cloudflare", "route53"},
				},
				"label":      openapi.String("What tells two accounts on one provider apart. Unique within the organization."),
				"username":   openapi.String("Route 53's access key id. Absent for Cloudflare, whose token is a single value. Never the secret half, whichever provider."),
				"created_at": openapi.String(""),
				"updated_at": openapi.String(""),
			}, "id", "provider", "label", "created_at", "updated_at"),
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
			"/orgs/{orgSlug}/dns": {
				"get": {
					OperationID: "listDNSProviders",
					Summary:     "List this organization's DNS providers",
					Tags:        []string{"DNS"},
					Parameters:  []openapi.Parameter{orgParam},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The providers.", openapi.Array(openapi.Ref("DNSProvider"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"post": {
					OperationID: "createDNSProvider",
					Summary:     "Store a DNS credential",
					Description: "Cloudflare takes one API token, in `password`. Route 53 takes an access key, which is two halves: the key id in `username` and the secret in `password`.\n\n" +
						"Cloudflare's older scheme — an email and a global key that can do anything to the account — is not accepted: a token is scoped to what someone chose, and the other kind is a credential that can close the account.",
					Tags:       []string{"DNS"},
					Parameters: []openapi.Parameter{orgParam},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"provider": {
							Type:        "string",
							Description: "cloudflare or route53.",
							Enum:        []string{"cloudflare", "route53"},
						},
						"label":    openapi.String("What tells this account from another on the same provider."),
						"username": openapi.String("Route 53's access key id. Ignored for Cloudflare."),
						"password": openapi.String("Cloudflare's API token, or Route 53's secret access key."),
					}, "provider", "label", "password")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The provider as stored.", openapi.Ref("DNSProvider")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("This organization already has a DNS provider with that label."),
					},
				},
			},
			"/orgs/{orgSlug}/dns/{id}": {
				"patch": {
					OperationID: "updateDNSProvider",
					Summary:     "Rename a DNS provider, or rotate its credential",
					Description: "Omit a field to leave it alone. The provider itself is not editable: a credential is *for* one provider — how it authenticates and what its secret even is both follow from that — so changing it in place would be a different credential wearing the old one's id.\n\n" +
						"For Route 53 a new secret must travel with its key id: a new secret against the old id is not a credential anyone chose, and it fails in a way that reads as the secret being wrong.",
					Tags:       []string{"DNS"},
					Parameters: []openapi.Parameter{orgParam, idParam},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"label":    openapi.String("A new label."),
						"username": openapi.String("Route 53's access key id."),
						"password": openapi.String("The new token or secret."),
					})),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The provider as it now stands.", openapi.Ref("DNSProvider")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("Another provider in this organization already has that label."),
					},
				},
				"delete": {
					OperationID: "deleteDNSProvider",
					Summary:     "Forget a DNS credential",
					Description: "Nothing at the provider is touched, and no record changes. What goes is this instance's ability to read or write them. Organization admins only.",
					Tags:        []string{"DNS"},
					Parameters:  []openapi.Parameter{orgParam, idParam},
					Responses: openapi.Responses{
						"204": openapi.Empty("Deleted."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
			"/orgs/{orgSlug}/dns/{id}/status": {
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
			"/orgs/{orgSlug}/dns/{id}/zones": {
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
			"/orgs/{orgSlug}/dns/{id}/records": {
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
