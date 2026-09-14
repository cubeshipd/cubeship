package httpx

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONBoundsEveryBody(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		for _, body := range []string{`{"value":"` + strings.Repeat("x", 1<<20) + `"}`, `{}` + strings.Repeat(" ", 1<<20)} {
			r := httptest.NewRequest("POST", "/setup", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			if chunked {
				r.ContentLength = -1
			}
			var v any
			if err := DecodeJSON(r, &v); err == nil {
				t.Fatal("accepted oversized JSON request")
			}
		}
	}
}
