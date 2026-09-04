package store

import (
	"net/url"
	"strings"
	"testing"
)

func TestDSNWithSchema(t *testing.T) {
	tests := []struct {
		name   string
		dsn    string
		schema string
		want   string // "" means: assert it parses and carries search_path
	}{
		{
			name:   "empty schema is left alone",
			dsn:    "postgres://u:p@host:5432/db?sslmode=disable",
			schema: "",
			want:   "postgres://u:p@host:5432/db?sslmode=disable",
		},
		{
			name:   "key/value DSN gets a trailing parameter",
			dsn:    "host=/var/run/postgresql dbname=cubeship",
			schema: "test_abc",
			want:   "host=/var/run/postgresql dbname=cubeship search_path=test_abc",
		},
		{
			name:   "URL DSN keeps its existing parameters",
			dsn:    "postgres://u:p@host:5432/db?sslmode=disable",
			schema: "test_abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DSNWithSchema(tt.dsn, tt.schema)
			if err != nil {
				t.Fatalf("DSNWithSchema: %v", err)
			}
			if tt.want != "" {
				if got != tt.want {
					t.Fatalf("got %q, want %q", got, tt.want)
				}
				return
			}
			u, err := url.Parse(got)
			if err != nil {
				t.Fatalf("result is not a valid URL: %v", err)
			}
			if u.Query().Get("search_path") != tt.schema {
				t.Errorf("search_path is %q, want %q", u.Query().Get("search_path"), tt.schema)
			}
			if u.Query().Get("sslmode") != "disable" {
				t.Errorf("sslmode was dropped: %q", got)
			}
		})
	}
}

// A schema name is an identifier, so it cannot be a bound parameter and
// has to be interpolated. Rejecting anything but a plain identifier is
// what keeps that safe.
func TestOpenSchemaRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{
		`public"; DROP TABLE users; --`,
		"has space",
		"Uppercase",
		"1leading-digit",
	} {
		// Rejection happens before any connection is attempted, so this
		// needs no database.
		_, err := OpenSchema("postgres://ignored/db", name)
		if err == nil {
			t.Errorf("OpenSchema accepted the schema name %q", name)
		} else if !strings.Contains(err.Error(), "invalid schema name") {
			t.Errorf("schema %q: got %v, want an invalid-schema-name error", name, err)
		}
	}
}
