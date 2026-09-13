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

type TemplateResource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// TemplateRun is one install, update or uninstall of an installation.
type TemplateRun struct {
	ID          int64              `json:"id"`
	Kind        string             `json:"kind"`
	FromRelease string             `json:"from_release"`
	ToRelease   string             `json:"to_release"`
	Status      string             `json:"status"`
	Step        string             `json:"step"`
	Error       string             `json:"error"`
	Created     []TemplateResource `json:"created"`
}

// TemplateInstall is a template installed on the instance.
type TemplateInstall struct {
	ID              int64              `json:"id"`
	Owner           string             `json:"owner"`
	Repo            string             `json:"repo"`
	Release         string             `json:"release"`
	Project         string             `json:"project"`
	Environment     string             `json:"environment"`
	Status          string             `json:"status"`
	Resources       []TemplateResource `json:"resources"`
	Runs            []TemplateRun      `json:"runs"`
	Busy            bool               `json:"busy"`
	UpdateAvailable *string            `json:"update_available"`
}

func (c *Client) ListTemplateInstalls(ctx context.Context) ([]TemplateInstall, error) {
	return request[[]TemplateInstall](ctx, c, "list installations", http.MethodGet, "/template-installs", nil, http.StatusOK, DefaultTimeout)
}

// TemplateChange is one line of an update's preview.
type TemplateChange struct {
	Action string `json:"action"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

type TemplateUpdatePreview struct {
	From    string           `json:"from"`
	To      string           `json:"to"`
	Changes []TemplateChange `json:"changes"`
	Inputs  []struct {
		Key   string `json:"key"`
		Label string `json:"label"`
		Type  string `json:"type"`
	} `json:"inputs"`
}

func (c *Client) PreviewTemplateUpdate(ctx context.Context, id int64, release string) (TemplateUpdatePreview, error) {
	path := fmt.Sprintf("/template-installs/%d/update", id)
	if release != "" {
		path += "?release=" + url.QueryEscape(release)
	}
	return request[TemplateUpdatePreview](ctx, c, "preview update", http.MethodGet, path, nil, http.StatusOK, DefaultTimeout)
}

type RunStarted struct {
	Run     TemplateRun       `json:"run"`
	Secrets map[string]string `json:"secrets"`
}

func (c *Client) UpdateTemplateInstall(ctx context.Context, id int64, release string, inputs map[string]string) (RunStarted, error) {
	return request[RunStarted](ctx, c, "update installation", http.MethodPost,
		fmt.Sprintf("/template-installs/%d/update", id),
		map[string]any{"release": release, "inputs": inputs}, http.StatusAccepted, DefaultTimeout)
}

func (c *Client) UninstallTemplateInstall(ctx context.Context, id int64, keepData bool) (RunStarted, error) {
	return request[RunStarted](ctx, c, "uninstall", http.MethodPost,
		fmt.Sprintf("/template-installs/%d/uninstall", id),
		map[string]any{"keep_data": keepData}, http.StatusAccepted, DefaultTimeout)
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

// TemplateRelease is a version a template can be installed at.
type TemplateRelease struct {
	Tag         string `json:"tag"`
	Commit      string `json:"commit"`
	PublishedAt string `json:"published_at"`
}

// ListTemplateReleases is every release of a template the catalog
// accepted, newest first.
func (c *Client) ListTemplateReleases(ctx context.Context, owner, repo string) ([]TemplateRelease, error) {
	page, err := request[struct {
		Releases []TemplateRelease `json:"releases"`
	}](ctx, c, "list template releases", http.MethodGet,
		fmt.Sprintf("/templates/%s/%s/releases", url.PathEscape(owner), url.PathEscape(repo)),
		nil, http.StatusOK, DefaultTimeout)
	return page.Releases, err
}
