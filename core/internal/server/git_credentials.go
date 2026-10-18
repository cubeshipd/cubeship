package server

import (
	"context"

	"cubeship/internal/bitbucket"
	"cubeship/internal/github"
	"cubeship/internal/gitremote"
)

type gitCredentials struct {
	github    *github.Service
	bitbucket *bitbucket.Service
	remotes   *gitremote.Service
}

func (c gitCredentials) CredentialsForRepository(ctx context.Context, raw string) (string, string, error) {
	if _, ok := github.ParseRepositoryURL(raw); ok {
		token, _, err := c.github.TokenForRepository(ctx, raw)
		return "x-access-token", token, err
	}
	if _, ok := bitbucket.ParseRepositoryURL(raw); ok {
		token, _, err := c.bitbucket.TokenForRepository(ctx, raw)
		return "x-token-auth", token, err
	}
	username, token, _, err := c.remotes.CredentialsForRepository(ctx, raw)
	if err != nil || token != "" {
		return username, token, err
	}
	return "", "", nil
}
