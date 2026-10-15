package registry

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"cubeship/internal/node"
	"cubeship/internal/platform/regauth"
)

type scopedNodeAuth struct{ err error }

func (scopedNodeAuth) AuthenticateNode(context.Context, string) (int64, string, error) {
	return 7, "worker", nil
}
func (a scopedNodeAuth) NodeCanPull(_ context.Context, id int64, repo string) (bool, error) {
	return id == 7 && repo == "project/production/app", a.err
}

func TestWorkerCannotPullUnassignedRepository(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{nodes: scopedNodeAuth{}, signingKey: key}
	req := httptest.NewRequest("GET", "/v2/token?scope=repository:other/production/private:pull", nil)
	req.SetBasicAuth(node.RegistryUsername, "credential")
	w := httptest.NewRecorder()
	h.issueToken(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var result struct{ Token string }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.Split(result.Token, ".")[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct{ Access []regauth.AccessEntry }
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if len(claims.Access) != 0 {
		t.Fatalf("unassigned repository granted: %+v", claims.Access)
	}
}

func TestNodePushRequestDoesNotGrantPull(t *testing.T) {
	h := &Handler{nodes: scopedNodeAuth{}}
	if got := h.nodeAccess(context.Background(), 7, "repository:project/production/app:push"); len(got) != 0 {
		t.Fatalf("push-only request received access: %+v", got)
	}
}

func TestNodeScopesGrantOnlyAuthorizedRequestedPulls(t *testing.T) {
	for _, tc := range []struct {
		scope string
		id    int64
		err   error
		want  bool
	}{
		{"repository:project/production/app:pull", 7, nil, true},
		{"repository:project/production/app:pull,push,delete", 7, nil, true},
		{"repository:project/production/app:push", 7, nil, false},
		{"repository:project/production/app:pull", 8, nil, false},
		{"repository:project/production/app:pull", 7, errors.New("lookup failed"), false},
		{"repository:other/production/private:pull", 7, nil, false},
		{"repository::pull", 7, nil, false},
		{"registry:catalog:*", 7, nil, false},
		{"repository:project/production/app", 7, nil, false},
	} {
		h := &Handler{nodes: scopedNodeAuth{err: tc.err}}
		access := h.nodeAccess(context.Background(), tc.id, tc.scope)
		if (len(access) > 0) != tc.want {
			t.Errorf("%q node %d: %+v", tc.scope, tc.id, access)
		}
		if len(access) > 0 && (len(access[0].Actions) != 1 || access[0].Actions[0] != "pull") {
			t.Fatalf("excess access: %+v", access)
		}
	}
}
