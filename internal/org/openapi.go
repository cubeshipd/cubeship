package org

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Organizations",
			Description: "Tenants. Every app, project and environment belongs to one, and membership in it is what authorizes everything below.",
		}},
		Schemas: map[string]*openapi.Schema{
			"Organization": openapi.Object(map[string]*openapi.Schema{
				"slug": openapi.String("Becomes a path component of every registry image this organization's apps push to."),
			}, "slug"),

			"NewOrgUser": openapi.Object(map[string]*openapi.Schema{
				"username": openapi.String(""),
				"org":      openapi.String(""),
				"role":     {Type: "string", Enum: []string{"admin", "member"}},
				"api_key":  openapi.String("The new user's key, shown once. Absent when an existing user was added to a further organization — they keep the key they already have."),
			}, "username", "org", "role"),
		},
		Paths: map[string]openapi.PathItem{
			"/orgs": {
				"post": {
					OperationID: "createOrg",
					Summary:     "Create an organization",
					Description: "Super-admin only. An organization is a tenant boundary, so handing this out would let any user mint namespaces the operator never approved.",
					Tags:        []string{"Organizations"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"slug": openapi.String("Lowercase letters, digits and dashes."),
					}, "slug")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The new organization.", openapi.Ref("Organization")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.TextResponse("You are not a super-admin."),
						"409": openapi.TextResponse("That slug is already taken."),
					},
				},
				"get": {
					OperationID: "listOrgs",
					Summary:     "List your organizations",
					Description: "The organizations you belong to — or every organization on the instance, if you are a super-admin.",
					Tags:        []string{"Organizations"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("Organizations you can see.", openapi.Array(openapi.Ref("Organization"))),
						"401": openapi.Unauthorized,
					},
				},
			},
			"/orgs/{orgSlug}": {
				"delete": {
					OperationID: "deleteOrg",
					Summary:     "Delete an organization",
					Description: "Removes the organization and every membership in it; the users themselves stay, since they may belong to others. **Refused while any project remains.** Super-admin only, like creating one.",
					Tags:        []string{"Organizations"},
					Parameters:  []openapi.Parameter{openapi.PathParam("orgSlug", "The organization's slug.")},
					Responses: openapi.Responses{
						"200": openapi.Empty("The organization is gone."),
						"401": openapi.Unauthorized,
						"403": openapi.TextResponse("You are not a super-admin."),
						"404": openapi.NotFound,
						"409": openapi.TextResponse("The organization still has projects in it."),
					},
				},
			},
			"/orgs/{orgSlug}/users": {
				"post": {
					OperationID: "createOrgUser",
					Summary:     "Add a user to an organization",
					Description: "Creates the user if this is the first organization they are added to, and issues their first API key. An existing username gains a membership instead — a user belongs to as many organizations as they are added to, each with its own role, and keeps one key throughout.\n\nRequires the admin role in the organization.",
					Tags:        []string{"Organizations"},
					Parameters:  []openapi.Parameter{openapi.PathParam("orgSlug", "The organization's slug.")},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"username": openapi.String(""),
						"role":     {Type: "string", Enum: []string{"admin", "member"}, Description: "admin can add users and create projects; member can create, deploy and read the logs of apps."},
					}, "username", "role")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The membership, and the user's key if they are new.", openapi.Ref("NewOrgUser")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("That user is already a member of this organization."),
					},
				},
			},
		},
	}
}
