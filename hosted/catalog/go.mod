module catalog

go 1.27.0

require (
	cubeship v0.0.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/pressly/goose/v3 v3.26.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

// The validator is the product's own package, read from the checkout
// rather than a published version: the catalog and the daemon have to
// agree on what a valid template is, commit for commit.
replace cubeship => ../../product
