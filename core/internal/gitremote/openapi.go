package gitremote

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	connection := openapi.Object(map[string]*openapi.Schema{
		"id":       openapi.Integer("Connection ID"),
		"provider": openapi.String("gitlab or gitea"),
		"base_url": openapi.String("HTTPS URL of the Git instance"),
		"account":  openapi.String("Connected account"),
	}, "id", "provider", "base_url", "account")
	repository := openapi.Object(map[string]*openapi.Schema{
		"full_name":      openapi.String("Namespace and repository name"),
		"url":            openapi.String("HTTPS clone URL"),
		"provider":       openapi.String("gitlab or gitea"),
		"private":        openapi.Bool("Whether the repository is private"),
		"default_branch": openapi.String("Default branch"),
	}, "full_name", "url", "provider")
	branch := openapi.Object(map[string]*openapi.Schema{"name": openapi.String("Branch name")}, "name")
	return openapi.Spec{Tags: []openapi.Tag{{Name: "Git remotes", Description: "GitLab and Gitea connections, including self-hosted instances."}}, Paths: map[string]openapi.PathItem{
		"/gitremotes": {
			"get": {OperationID: "listGitRemotes", Summary: "List GitLab and Gitea connections", Tags: []string{"Git remotes"}, Responses: openapi.Responses{"200": openapi.JSONResponse("connections", openapi.Array(connection))}},
			"post": {OperationID: "connectGitRemote", Summary: "Connect a GitLab or Gitea instance", Tags: []string{"Git remotes"}, RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
				"provider": openapi.String("gitlab or gitea"), "base_url": openapi.String("HTTPS instance URL"), "api_token": openapi.String("Personal access token"),
			}, "provider", "base_url", "api_token")), Responses: openapi.Responses{"201": openapi.JSONResponse("connection", connection)}},
		},
		"/gitremotes/{id}":         {"delete": {OperationID: "disconnectGitRemote", Summary: "Disconnect GitLab or Gitea", Tags: []string{"Git remotes"}, Parameters: []openapi.Parameter{openapi.PathParam("id", "Connection ID")}, Responses: openapi.Responses{"204": openapi.Empty("disconnected")}}},
		"/gitremotes/{id}/webhook": {"get": {OperationID: "getGitRemoteWebhookSecret", Summary: "Read the webhook secret", Tags: []string{"Git remotes"}, Parameters: []openapi.Parameter{openapi.PathParam("id", "Connection ID")}, Responses: openapi.Responses{"200": openapi.JSONResponse("secret", openapi.Object(map[string]*openapi.Schema{"secret": openapi.String("Webhook secret")}, "secret"))}}},
		"/gitremotes/repositories": {"get": {OperationID: "listGitRemoteRepositories", Summary: "List GitLab and Gitea repositories", Tags: []string{"Git remotes"}, Responses: openapi.Responses{"200": openapi.JSONResponse("repositories", openapi.Array(repository))}}},
		"/gitremotes/branches":     {"get": {OperationID: "listGitRemoteBranches", Summary: "List repository branches", Tags: []string{"Git remotes"}, Parameters: []openapi.Parameter{openapi.QueryParam("repo", "HTTPS clone URL")}, Responses: openapi.Responses{"200": openapi.JSONResponse("branches", openapi.Array(branch))}}},
	}}
}
