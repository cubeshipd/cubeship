package bitbucket

import "cubeship/internal/platform/openapi"

func obj(fields map[string]*openapi.Schema, req ...string) *openapi.Schema {
	return openapi.Object(fields, req...)
}
func (h *Handler) OpenAPI() openapi.Spec {
	con := obj(map[string]*openapi.Schema{"id": openapi.Integer("connection id")}, "id")
	repo := obj(map[string]*openapi.Schema{"full_name": openapi.String("workspace/repository")}, "full_name")
	branch := obj(map[string]*openapi.Schema{"name": openapi.String("branch")}, "name")
	return openapi.Spec{Tags: []openapi.Tag{{Name: "Bitbucket", Description: "Bitbucket Cloud connections."}}, Paths: map[string]openapi.PathItem{
		"/bitbucket/start":        {"get": {OperationID: "startBitbucketOAuth", Summary: "Start Bitbucket OAuth", Tags: []string{"Bitbucket"}, Responses: openapi.Responses{"302": openapi.Empty("redirect to Bitbucket")}}},
		"/bitbucket":              {"get": {OperationID: "listBitbucketConnections", Summary: "List Bitbucket connections", Tags: []string{"Bitbucket"}, Responses: openapi.Responses{"200": openapi.JSONResponse("connections", openapi.Array(con))}}, "post": {OperationID: "connectBitbucket", Summary: "Connect Bitbucket with a scoped API token", Tags: []string{"Bitbucket"}, RequestBody: openapi.Body(obj(map[string]*openapi.Schema{"api_token": openapi.String("Scoped Bitbucket API token")}, "api_token")), Responses: openapi.Responses{"201": openapi.JSONResponse("connection", con)}}},
		"/bitbucket/repositories": {"get": {OperationID: "listBitbucketRepositories", Summary: "List Bitbucket repositories", Tags: []string{"Bitbucket"}, Responses: openapi.Responses{"200": openapi.JSONResponse("repositories", openapi.Array(repo))}}},
		"/bitbucket/branches":     {"get": {OperationID: "listBitbucketBranches", Summary: "List Bitbucket branches", Tags: []string{"Bitbucket"}, Parameters: []openapi.Parameter{openapi.QueryParam("repo", "workspace/repository")}, Responses: openapi.Responses{"200": openapi.JSONResponse("branches", openapi.Array(branch))}}},
		"/bitbucket/{id}":         {"delete": {OperationID: "disconnectBitbucket", Summary: "Disconnect Bitbucket", Tags: []string{"Bitbucket"}, Parameters: []openapi.Parameter{openapi.PathParam("id", "connection id")}, Responses: openapi.Responses{"204": openapi.Empty("disconnected")}}},
	}}
}
