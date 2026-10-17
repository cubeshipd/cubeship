package server

import (
	"context"

	"cubeship/internal/bitbucket"
	"cubeship/internal/github"
)

type gitCredentials struct {
	github    *github.Service
	bitbucket *bitbucket.Service
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
	return "", "", nil
}
