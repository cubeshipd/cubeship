package dns

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	idParam := openapi.PathParam("id", "DNS provider id.")
	zoneParam := openapi.QueryParam("zone", "The provider's own id for the zone, as the zone listing gives it.")

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name: "DNS",
			Description: "The DNS providers this instance manages its records through — Cloudflare and Route 53. " +
				"Cubeship already asks you to point a name at this host; these are what let that happen here " +
				"rather than in somebody else's control panel.\n\n" +
				"A provider is which API to speak and which stored credential to speak it with. The secret itself is a " +
				"credential, so the same AWS access key can write Route 53 records here and be pulled from ECR with " +
				"through a registry. Admins only, throughout — a DNS provider moves where a name points for every name " +
				"on the account, including names that have nothing to do with Cubeship.",
		}},
		Schemas: map[string]*openapi.Schema{
			"DNSProvider": openapi.Object(map[string]*openapi.Schema{
				"id":            openapi.Integer(""),
				"provider":      {Type: "string", Enum: []string{"aws", "cloudflare"}, Description: "Which API is spoken. Permanent: it is what the provider *is*, and changing it in place would be a different provider wearing the same id."},
				"provider_name": openapi.String("The provider as a person calls it, so a client needs no table of its own."),
				"credential_id": openapi.Integer("The stored credential this provider authenticates with. Re-pointable, so a rotated key can be swapped for a new one without the zones moving."),
				"label":         openapi.String("The credential's label — what a person picked it out of a list by."),
				"username":      openapi.String("The first half of the credential's secret, where it has one. Never the secret itself."),
				"created_at":    {Type: "string", Format: "date-time"},
				"updated_at":    {Type: "string", Format: "date-time"},
			}, "id", "provider", "provider_name", "credential_id", "label", "created_at", "updated_at"),
			"DNSProviderKind": openapi.Object(map[string]*openapi.Schema{
				"provider":       openapi.String(""),
				"name":           openapi.String("The provider as a person calls it."),
				"username_label": openapi.String("What to call the first field when a login is typed rather than picked. Absent where the secret is a single value — then there is no first field."),
				"password_label": openapi.String("What to call the secret field."),
				"hint":           openapi.String("Where to get one, and what it needs to be allowed to do."),
			}, "provider", "name", "password_label", "hint"),
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
			"/dns": {
				"get": {
					OperationID: "listDNSProviders",
					Summary:     "List the DNS providers this instance reaches",
					Tags:        []string{"DNS"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The providers.", openapi.Array(openapi.Ref("DNSProvider"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
				"post": {
					OperationID: "connectDNSProvider",
					Summary:     "Connect a DNS provider",
					Description: "The login comes from one of two places, and neither is privileged over the other: `credential_id` names a secret this instance already holds, or `password` (with `username` where the provider's secret has two halves) types one and the credential is created from it in the same transaction — a secret stored beside a provider that failed to be created is a secret nobody asked to keep.\n\n" +
						"Both at once is refused: it has no obvious reading, and guessing which was meant is how the wrong secret gets stored.",
					Tags: []string{"DNS"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"provider":      {Type: "string", Enum: []string{"aws", "cloudflare"}, Description: "GET /dns/providers lists these, and what each one asks for."},
						"credential_id": openapi.Integer("A stored credential to write through."),
						"label":         openapi.String("What the typed login is called under Credentials. Named after the provider when empty."),
						"username":      openapi.String("The first half of a typed login, where the provider's secret has one."),
						"password":      openapi.String("A typed secret."),
					}, "provider")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The connected provider.", openapi.Ref("DNSProvider")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"409": openapi.TextResponse("This instance already reaches that provider with that credential."),
					},
				},
			},
			"/dns/providers": {
				"get": {
					OperationID: "listDNSProviderKinds",
					Summary:     "List the providers a DNS account can be created for",
					Description: "What to put in the connect form: what each provider is called, and what its login's two fields are called — or its one. Read it rather than hard-coding a list, since which providers work is decided by which clients this release actually has.",
					Tags:        []string{"DNS"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The providers.", openapi.Array(openapi.Ref("DNSProviderKind"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
			},
			"/dns/{id}": {
				"patch": {
					OperationID: "repointDNSProvider",
					Summary:     "Point a DNS provider at another credential",
					Description: "Which API is spoken is not editable — that is what the provider *is* — and neither is the secret: rotating one is an edit to the credential, in one place, and everything using it follows.",
					Tags:        []string{"DNS"},
					Parameters:  []openapi.Parameter{idParam},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"credential_id": openapi.Integer("The stored credential to write through from now on."),
					}, "credential_id")),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The updated provider.", openapi.Ref("DNSProvider")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("That credential already reaches this provider."),
					},
				},
				"delete": {
					OperationID: "disconnectDNSProvider",
					Summary:     "Disconnect a DNS provider",
					Description: "Your records stay exactly as they are. What goes is this instance's ability to read or write them.\n\n" +
						"The credential is left alone: the same secret may be pulling images from a registry. Refused while the instance writes its own records through this provider — removing it silently would leave the domain screen pointed at nothing.",
					Tags:       []string{"DNS"},
					Parameters: []openapi.Parameter{idParam},
					Responses: openapi.Responses{
						"204": openapi.Empty("Disconnected."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("This instance writes its own records through it."),
					},
				},
			},
			"/dns/{id}/status": {
				"get": {
					OperationID: "probeDNSProvider",
					Summary:     "Ask whether this credential still works",
					Description: "A live call to the provider, not something recorded when the credential was stored. The interesting case is one that used to work — a revoked Cloudflare token, an access key deleted in IAM — neither of which tells Cubeship anything: the first sign would be a record edit failing.\n\n" +
						"`unauthorized` is fixed by storing a new credential; `unreachable` is the provider's API being down.",
					Tags:       []string{"DNS"},
					Parameters: []openapi.Parameter{idParam},
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
					Parameters:  []openapi.Parameter{idParam},
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
					Parameters: []openapi.Parameter{idParam, zoneParam},
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
					Parameters:  []openapi.Parameter{idParam, zoneParam},
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
						idParam, zoneParam,
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
