package traefik

import (
	"os"
	"path/filepath"
)

// RetryFileName is the file whose appearing and disappearing is the
// whole of how this daemon asks Traefik to try again for a certificate.
const RetryFileName = "retry.yml"

// retryDocument is what goes in it: one transport nothing refers to.
//
// It has to be a **real difference** to the configuration and no
// difference at all to what the instance serves, and those two pull
// against each other. A serversTransport is the answer: Traefik parses
// it, it changes the document Traefik builds, and it does nothing
// whatever unless a service names it — which none does.
//
// It is its own file rather than a line in the routes, so a mistake in
// it is a mistake in a file that carries no router. The file provider
// assembles the directory into one document, so a file it refuses takes
// everything down with it either way, which is why
// TestTraefikAcceptsTheCertificateRetryFile hands this to the real
// proxy rather than trusting that it parses.
const retryDocument = `# Written and removed by Cubeship to ask Traefik to resolve a
# certificate it has not got. Nothing refers to what is in here; see
# internal/certificates/retry.go for why it exists at all.
http:
  serversTransports:
    cubeship-certificate-retry:
      maxIdleConnsPerHost: 1
`

// RetryCertificates asks Traefik to have another go at every name it is
// missing a certificate for, and reports whether the marker is now
// there.
//
// **Traefik only resolves certificates when its configuration changes.**
// It walks the routers on every configuration it is handed and asks for
// what it has not got; between two of those it does nothing, and there
// is no timer behind it and no API in front of it. So a name whose first
// attempt failed — a record a minute too fresh, an hour when Let's
// Encrypt could not reach the nameservers — waits for the next change to
// the routes file, which on a settled instance may be never.
//
// Changing the configuration is therefore the only lever, and it must
// not be the routes themselves: writing those again means either an
// identical document, which Traefik skips, or a moment with a router
// missing, which is a name off the internet. A file beside them that
// says nothing is both a genuine change and no change to anything
// served.
func RetryCertificates(dataDir string) (bool, error) {
	dir := filepath.Join(dataDir, "traefik-dynamic")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	path := filepath.Join(dir, RetryFileName)

	// Present or absent are both fine to leave behind, so the toggle
	// needs no state anywhere: whichever it is, the other one is a
	// change, and a daemon that restarts mid-cycle carries on from
	// whatever it finds.
	if _, err := os.Stat(path); err == nil {
		return false, os.Remove(path)
	} else if !os.IsNotExist(err) {
		return false, err
	}
	return true, os.WriteFile(path, []byte(retryDocument), 0o600)
}
