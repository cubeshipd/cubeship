package buildkit

import "testing"

// A build runs whatever the repository contains, and a Dockerfile may
// name any registry it likes — `FROM somewhere-else.example.com/x`. The
// credential this instance pushes with answers for one host and nothing
// else, so a build cannot make BuildKit offer it to a registry somebody
// wrote into a file.
func TestThePushCredentialIsOfferedToOneHostOnly(t *testing.T) {
	login := Login{Host: "registry.cubeship.example.com", Username: "cubeship-builder", Password: "secret"}

	if got := authConfigFor(login, login.Host); got.Password != "secret" || got.Username != login.Username {
		t.Errorf("the instance's own registry got %+v, and it is the one that has to be able to log in", got)
	}
	for _, host := range []string{"index.docker.io", "ghcr.io", "registry.cubeship.example.com.evil.test", ""} {
		if got := authConfigFor(login, host); got.Username != "" || got.Password != "" {
			t.Errorf("%q was offered %+v", host, got)
		}
	}
}

// Nothing to log in with is not a login at all: an instance that has
// never generated a builder credential must not attach an empty one,
// which reads to a registry as an anonymous push and fails somewhere
// less obvious.
func TestNoBuilderCredentialAttachesNothing(t *testing.T) {
	for _, login := range []Login{{}, {Host: "registry.example.com"}, {Username: "cubeship-builder"}} {
		if got := pushAuth(login); got != nil {
			t.Errorf("pushAuth(%+v) attached %d providers, want none", login, len(got))
		}
	}
}
