package project_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/project"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

// A one-pixel PNG, which is the smallest thing that is genuinely one.
var onePixelPNG = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
	0x0d, 0x0a, 0x2d, 0xb4,
	0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

// put sends raw bytes with a chosen Content-Type, which is the whole
// point of several of these: the header is the caller's claim.
func put(t *testing.T, f *servertest.Fixture, slug, contentType string, body []byte, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut,
		httpx.APIPrefix+"/projects/"+slug+"/image", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return f.Serve(t, req)
}

// **The bytes decide the type, and the header decides nothing.**
//
// What is stored is what a project is served back as, from this
// instance's own origin and beside the session cookie — so a caller who
// could name the type would be choosing what this daemon serves. A
// header is a claim; `http.DetectContentType` reads what actually
// arrived.
func TestWhatArrivesDecidesTheTypeRatherThanWhatItClaims(t *testing.T) {
	f := servertest.New(t)

	// An SVG with a script in it, announced as a PNG. SVG is not one of
	// the three either way, and that is the point: nothing about the
	// header gets it in.
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	servertest.RequireStatus(t, put(t, f, "web", "image/png", svg, f.AdminKey),
		http.StatusUnsupportedMediaType)

	// And a real PNG announced as something else is still a PNG.
	servertest.RequireStatus(t, put(t, f, "web", "text/plain", onePixelPNG, f.AdminKey),
		http.StatusNoContent)

	rec := f.Do(t, http.MethodGet, "/projects/web/image", nil, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("served as %q, want image/png", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), onePixelPNG) {
		t.Error("the bytes that came back are not the ones that went in")
	}
}

// Nothing here resizes, so the ceiling is what makes that safe rather
// than a hope. Refused while whoever chose the file still has it.
func TestAnImageOverTheCeilingIsRefusedRatherThanShrunk(t *testing.T) {
	f := servertest.New(t)

	big := append(append([]byte{}, onePixelPNG...),
		bytes.Repeat([]byte{0}, project.MaxImageBytes)...)
	servertest.RequireStatus(t, put(t, f, "web", "image/png", big, f.AdminKey),
		http.StatusRequestEntityTooLarge)

	// And nothing was stored on the way to refusing.
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/projects/web/image", nil, f.AdminKey),
		http.StatusNotFound)
}

// A picture is part of what the instance looks like to everybody, so
// putting one on is an admin's — and looking at one is not, because it
// is drawn on the grid every member opens.
func TestAMemberSeesThePictureAndDoesNotChooseIt(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "employee", user.RoleMember)

	servertest.RequireStatus(t, put(t, f, "web", "image/png", onePixelPNG, memberKey),
		http.StatusForbidden)
	servertest.RequireStatus(t, put(t, f, "web", "image/png", onePixelPNG, f.AdminKey),
		http.StatusNoContent)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/projects/web/image", nil, memberKey),
		http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/projects/web/image", nil, memberKey),
		http.StatusForbidden)
}

// **The listing says whether to ask.** Without it a grid makes a
// request per project, and on an instance where nobody has chosen a
// picture every one of them answers 404 — a screen whose normal state
// is a row of failures in the network tab.
func TestTheListingSaysWhichProjectsWearOne(t *testing.T) {
	f := servertest.New(t)

	has := func() map[string]bool {
		t.Helper()
		var projects []struct {
			Slug     string `json:"slug"`
			HasImage bool   `json:"has_image"`
		}
		rec := f.Do(t, http.MethodGet, "/projects", nil, f.AdminKey)
		servertest.RequireStatus(t, rec, http.StatusOK)
		if err := json.Unmarshal(rec.Body.Bytes(), &projects); err != nil {
			t.Fatalf("decode projects: %v", err)
		}
		out := map[string]bool{}
		for _, p := range projects {
			out[p.Slug] = p.HasImage
		}
		return out
	}

	if has()["web"] {
		t.Error("a project nobody has given a picture claims to wear one")
	}
	servertest.RequireStatus(t, put(t, f, "web", "image/png", onePixelPNG, f.AdminKey),
		http.StatusNoContent)
	if !has()["web"] {
		t.Error("a project that wears one does not say so")
	}

	// Taking it off puts it back, and asking twice is not a mistake:
	// asking for the state a thing is already in is not an error.
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/projects/web/image", nil, f.AdminKey),
		http.StatusNoContent)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/projects/web/image", nil, f.AdminKey),
		http.StatusNoContent)
	if has()["web"] {
		t.Error("a project still claims a picture after it was taken off")
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/projects/web/image", nil, f.AdminKey),
		http.StatusNotFound)
}
