package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// TemplateSummary is one template in the catalog's listing.
type TemplateSummary struct {
	Owner       string   `json:"owner"`
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Stars       int      `json:"stars"`
	Tags        []string `json:"tags"`
	Verified    bool     `json:"verified"`
	Release     struct {
		Tag string `json:"tag"`
	} `json:"release"`
}

type TemplatePage struct {
	Templates  []TemplateSummary `json:"templates"`
	NextCursor *string           `json:"next_cursor"`
}

func (c *Client) ListTemplates(ctx context.Context, q, tag string) (TemplatePage, error) {
	query := url.Values{}
	if q != "" {
		query.Set("q", q)
	}
	if tag != "" {
		query.Set("tag", tag)
	}
	path := "/templates"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return request[TemplatePage](ctx, c, "list templates", http.MethodGet, path, nil, http.StatusOK, DefaultTimeout)
}

// TemplateInstall is one install and how it is going.
type TemplateInstall struct {
	ID          int64  `json:"id"`
	Owner       string `json:"owner"`
	Repo        string `json:"repo"`
	Release     string `json:"release"`
	Project     string `json:"project"`
	Environment string `json:"environment"`
	Status      string `json:"status"`
	Step        string `json:"step"`
	Error       string `json:"error"`
	Resources   []struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"resources"`
}

type InstallTemplateRequest struct {
	Release     string `json:"release,omitempty"`
	Project     string `json:"project,omitempty"`
	Environment string `json:"environment,omitempty"`
	Names       struct {
		Databases map[string]string `json:"databases,omitempty"`
		Stores    map[string]string `json:"stores,omitempty"`
		Apps      map[string]string `json:"apps,omitempty"`
	} `json:"names"`
	Inputs map[string]string `json:"inputs,omitempty"`
}

type InstallStarted struct {
	Install TemplateInstall   `json:"install"`
	Secrets map[string]string `json:"secrets"`
}

func (c *Client) InstallTemplate(ctx context.Context, owner, repo string, req InstallTemplateRequest) (InstallStarted, error) {
	return request[InstallStarted](ctx, c, "install template", http.MethodPost,
		"/templates/"+segment(owner)+"/"+segment(repo)+"/installs", req, http.StatusAccepted, DefaultTimeout)
}

func (c *Client) GetTemplateInstall(ctx context.Context, id int64) (TemplateInstall, error) {
	return request[TemplateInstall](ctx, c, "read install", http.MethodGet,
		fmt.Sprintf("/template-installs/%d", id), nil, http.StatusOK, DefaultTimeout)
}
