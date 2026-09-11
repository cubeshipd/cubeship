package objectstore

import (
	"errors"
	"strings"
	"testing"
)

// A key typed by somebody becomes an S3 key, and S3 has no paths: `../`
// is a perfectly legal thing to call an object. What makes it wrong
// here is that this instance's own listing is built out of prefixes, so
// a name that walks out of the folder it was uploaded to is a file
// nobody can find again — under a folder nobody put it in.
func TestAKeyCannotLeaveTheFolderItIsWrittenTo(t *testing.T) {
	refused := []string{
		"", " ", ".", "..", "../secrets", "a/../../b", "/absolute", "a//b", "./x",
	}
	for _, name := range refused {
		if _, err := CleanKey("uploads/", name); !errors.Is(err, ErrBadKey) {
			t.Errorf("CleanKey(%q) = %v, want ErrBadKey", name, err)
		}
	}

	accepted := map[string]string{
		"report.pdf":       "uploads/report.pdf",
		"2026/report.pdf":  "uploads/2026/report.pdf",
		"a file.txt":       "uploads/a file.txt",
		"..hidden":         "uploads/..hidden",
		"dots...in.a.name": "uploads/dots...in.a.name",
	}
	for name, want := range accepted {
		got, err := CleanKey("uploads/", name)
		if err != nil {
			t.Errorf("CleanKey(%q): %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("CleanKey(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestAPrefixIsNormalisedToExactlyOneTrailingSlash(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"/":             "",
		"a":             "a/",
		"a/":            "a/",
		"/a/b":          "a/b/",
		"backups/2026/": "backups/2026/",
	}
	for in, want := range cases {
		got, err := CleanPrefix(in)
		if err != nil {
			t.Errorf("CleanPrefix(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("CleanPrefix(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"a/../b", "a//b", "./a"} {
		if _, err := CleanPrefix(in); !errors.Is(err, ErrBadKey) {
			t.Errorf("CleanPrefix(%q) = %v, want ErrBadKey", in, err)
		}
	}
}

// The name goes into a hostname on most providers, so what S3 refuses
// is refused here — where it can be a sentence rather than an XML fault
// somebody has to read a provider's documentation to decode.
func TestABucketNameIsCheckedBeforeItIsSent(t *testing.T) {
	for _, name := range []string{"backups", "my-bucket", "a.b.c", "ab1"} {
		if err := CheckBucketName(name); err != nil {
			t.Errorf("CheckBucketName(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{
		"", "ab", "UPPER", "-leading", "trailing-", "two..dots", "dot-.dash",
		strings.Repeat("a", 64), "under_score",
	} {
		if err := CheckBucketName(name); !errors.Is(err, ErrBadBucket) {
			t.Errorf("CheckBucketName(%q) = %v, want ErrBadBucket", name, err)
		}
	}
}

// Somebody linking a store pastes what the provider's console shows
// them, which is a URL. Refusing that and asking for "just the host"
// would be refusing the thing that is on everybody's clipboard.
func TestAnEndpointMayBePastedAsAURL(t *testing.T) {
	cases := []struct {
		in     string
		host   string
		secure bool
	}{
		{"s3.wasabisys.com", "s3.wasabisys.com", true},
		{"https://s3.wasabisys.com", "s3.wasabisys.com", true},
		{"https://s3.wasabisys.com/", "s3.wasabisys.com", true},
		{"http://minio.internal:9000", "minio.internal:9000", false},
		{"  S3.Wasabisys.com  ", "s3.wasabisys.com", true},
	}
	for _, c := range cases {
		host, secure, err := endpointHost(c.in)
		if err != nil {
			t.Errorf("endpointHost(%q): %v", c.in, err)
			continue
		}
		if host != c.host || secure != c.secure {
			t.Errorf("endpointHost(%q) = %q/%v, want %q/%v", c.in, host, secure, c.host, c.secure)
		}
	}
	for _, in := range []string{"", "   ", "https://", "host/with/path", "a b"} {
		if _, _, err := endpointHost(in); err == nil {
			t.Errorf("endpointHost(%q) was accepted", in)
		}
	}
}

// Every provider here is a template with one variable in it, and the
// variable is what its form asks for. Getting the pair wrong is an
// endpoint that does not resolve or a signature that is refused, and
// neither says which.
func TestEachProviderDerivesItsOwnEndpoint(t *testing.T) {
	cases := []struct {
		spec      LinkSpec
		endpoint  string
		region    string
		pathStyle bool
	}{
		{LinkSpec{Provider: ProviderAWS, Region: "eu-central-1"},
			"s3.eu-central-1.amazonaws.com", "eu-central-1", false},
		{LinkSpec{Provider: ProviderDigitalOcean, Region: "nyc3"},
			"nyc3.digitaloceanspaces.com", "nyc3", false},
		// R2 has one region and its name is the literal "auto". A
		// signature computed for anything else is refused, so this is
		// not a default somebody may override.
		{LinkSpec{Provider: ProviderCloudflare, Account: "abc123"},
			"abc123.r2.cloudflarestorage.com", "auto", true},
		{LinkSpec{Provider: ProviderGeneric, Endpoint: "https://s3.wasabisys.com"},
			"s3.wasabisys.com", DefaultRegion, true},
	}
	for _, c := range cases {
		var store Store
		if err := describeEndpoint(&store, c.spec); err != nil {
			t.Errorf("%s: %v", c.spec.Provider, err)
			continue
		}
		if store.Endpoint != c.endpoint || store.Region != c.region || store.PathStyle != c.pathStyle {
			t.Errorf("%s: endpoint %q region %q path_style %v, want %q %q %v",
				c.spec.Provider, store.Endpoint, store.Region, store.PathStyle,
				c.endpoint, c.region, c.pathStyle)
		}
	}
}

// The one field each provider needs is the one thing that cannot be
// guessed, so it is refused rather than defaulted: an S3 store linked
// without a region would be pointed at the wrong continent, and a
// signature for the wrong region fails with a message about
// credentials.
func TestTheOneFieldAProviderNeedsIsRefusedWhenMissing(t *testing.T) {
	cases := map[Provider]error{
		ProviderAWS:          ErrRegionRequired,
		ProviderDigitalOcean: ErrRegionRequired,
		ProviderCloudflare:   ErrAccountRequired,
		ProviderGeneric:      ErrEndpointRequired,
	}
	for provider, want := range cases {
		var store Store
		err := describeEndpoint(&store, LinkSpec{Provider: provider})
		if !errors.Is(err, want) {
			t.Errorf("%s with nothing filled in: %v, want %v", provider, err, want)
		}
		// And every provider a form may offer says which field it is.
		if provider.Asks() == AsksNothing {
			t.Errorf("%s asks for nothing, so no form can know what to put in front of somebody", provider)
		}
	}
}

// Naming a bucket while linking gives up listing, creating and deleting
// every other bucket in the store, so it is asked for only where the
// provider's own logins are issued that way — R2 tokens and a Space's
// access keys, which their consoles offer per bucket as the ordinary
// choice.
//
// Offered everywhere, it is a field somebody fills in because it is
// there, and the store is narrowed for a limit its login does not have.
// Every provider is listed rather than only the two, so adding one is a
// decision about this rather than whatever the zero value happens to be.
func TestOnlyTheProvidersWhoseLoginsAreScopedAskForABucket(t *testing.T) {
	want := map[Provider]bool{
		ProviderCloudflare:   true,
		ProviderDigitalOcean: true,
		ProviderAWS:          false,
		ProviderGeneric:      false,
		ProviderMinIO:        false,
	}
	for provider, scoped := range want {
		if got := provider.ScopesByBucket(); got != scoped {
			t.Errorf("%s scopes by bucket = %v, want %v", provider, got, scoped)
		}
	}
	for _, p := range Providers() {
		if _, listed := want[p]; !listed {
			t.Errorf("%s can be linked and nothing here decides whether it asks for a bucket", p)
		}
	}
}

// A managed store's endpoint is derived from its own container name, so
// it stays correct through anything that changes about the network and
// there is no column that can drift from it.
func TestAManagedStoreAnswersAtItsOwnContainerName(t *testing.T) {
	store := &Store{Kind: KindManaged, Slug: "uploads"}
	if got, want := store.EndpointHost(), "cubeship-s3-uploads:9000"; got != want {
		t.Errorf("EndpointHost() = %q, want %q", got, want)
	}
	if got, want := store.URL(), "http://cubeship-s3-uploads:9000"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
	// No domain and no published port is no external address. Guessing
	// an interface's address would be wrong on every host behind NAT.
	if got := store.ExternalURL(""); got != "" {
		t.Errorf("ExternalURL with no domain = %q, want empty", got)
	}
	if got := store.ExternalURL("example.com"); got != "" {
		t.Errorf("ExternalURL while unexposed = %q, want empty", got)
	}
	store.ExposedPort = 16000
	if got, want := store.ExternalURL("example.com"), "http://example.com:16000"; got != want {
		t.Errorf("ExternalURL() = %q, want %q", got, want)
	}
	// A linked store is never reached through this host.
	linked := &Store{Kind: KindExternal, Endpoint: "s3.eu-west-1.amazonaws.com", Secure: true, ExposedPort: 16000}
	if got := linked.ExternalURL("example.com"); got != "" {
		t.Errorf("a linked store reported an external address on this host: %q", got)
	}
	if got, want := linked.URL(), "https://s3.eu-west-1.amazonaws.com"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}

// A version is permanent once a store holds data, so offering one is a
// promise to go on running it. An empty list would make Create pick
// nothing and every store fail to start.
func TestTheDefaultVersionIsOneThisReleaseOffers(t *testing.T) {
	if len(Versions()) == 0 {
		t.Fatal("no MinIO version is offered, so no store can be created")
	}
	if !KnowsVersion(DefaultVersion()) {
		t.Errorf("the default version %q is not in the offered list", DefaultVersion())
	}
	if KnowsVersion("latest") {
		t.Error(`"latest" is offered, which would change the server under somebody's data on a redeploy`)
	}
}

// A folder is a common prefix and a file is a key, and both are shown
// by their last segment. Getting this wrong puts the whole path in
// every row of the listing.
func TestAFolderAndAFileAreNamedByTheirLastSegment(t *testing.T) {
	if got, want := FolderName("backups/2026/"), "2026"; got != want {
		t.Errorf("FolderName = %q, want %q", got, want)
	}
	if got, want := FolderName("top/"), "top"; got != want {
		t.Errorf("FolderName = %q, want %q", got, want)
	}
	if got, want := (Object{Key: "backups/2026/db.sql.gz"}).Name(), "db.sql.gz"; got != want {
		t.Errorf("Object.Name = %q, want %q", got, want)
	}
	if got, want := (Object{Key: "root.txt"}).Name(), "root.txt"; got != want {
		t.Errorf("Object.Name = %q, want %q", got, want)
	}
}

// The two published-port ranges must not overlap. Each module checks
// only its own table, so an overlap would appear as a container that
// will not bind — minutes later, for no reason visible on either
// screen.
func TestTheAutomaticPortRangeCannotCollideWithADatabase(t *testing.T) {
	// The datastores' range, written out rather than imported: this
	// module does not depend on that one, and the point of the test is
	// that the constant here was chosen with the other one in mind.
	const datastoreStart, datastoreEnd = 15000, 15999
	if PortRangeStart <= datastoreEnd && datastoreStart <= PortRangeEnd {
		t.Errorf("object stores pick from %d-%d, which overlaps the datastores' %d-%d",
			PortRangeStart, PortRangeEnd, datastoreStart, datastoreEnd)
	}
}

// A region and an R2 account id are each one label of an endpoint this
// instance stores and then connects to. A value that can end the host
// early — `#`, `/`, `:`, `@` — points the store at a server nobody
// chose, and it stays pointed there, because the endpoint is derived
// once and written down.
func TestWhatMayBeInterpolatedIntoAnEndpointsHostname(t *testing.T) {
	for _, value := range []string{
		"eu-central-1", "nyc3", "sfo3", "auto", "abc123",
		"0123456789abcdef0123456789abcdef",
	} {
		if !ValidHostLabel(value) {
			t.Errorf("ValidHostLabel(%q) = false, and that is a region or account somebody has", value)
		}
	}

	for _, value := range []string{
		"", " ", "EU-CENTRAL-1", "eu_central_1",
		"evil.com#", "evil.com", "x/../y", "x:443", "user@evil.com",
		"-leading", "trailing-", "a b", strings.Repeat("a", 64),
	} {
		if ValidHostLabel(value) {
			t.Errorf("ValidHostLabel(%q) = true; it reaches a hostname this instance connects to", value)
		}
	}
}
