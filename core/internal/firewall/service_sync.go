package firewall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"cubeship/internal/platform/hostexec"
	"cubeship/internal/user"
)

// ReleaseDocker takes it back out, leaving UFW as it was.
//
// Published ports go back to being open regardless of what any rule
// says, which is Docker's own behaviour and the reason for all of this.
func (s *Service) ReleaseDocker(ctx context.Context, caller *user.User) (*Status, error) {
	if err := user.Allow(caller, user.ResFirewall, user.LevelManage, ""); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.script(ctx, releaseScript); err != nil {
		return nil, err
	}
	return s.Status(ctx, caller)
}

func adoptScript(path string) string {
	return `
set -e
if grep -q '` + dockerBeginMarker + `' /etc/ufw/after.rules; then exit 0; fi
cp /etc/ufw/after.rules /etc/ufw/after.rules.cubeship-backup
cat ` + path + ` >> /etc/ufw/after.rules
ufw status | grep -q '^Status: active' && ufw reload || true
`
}

// SyncPublished rewrites the stanza from what is exposed now, whole.
//
// Only on a host where the stanza is already installed: without it
// nothing is denied, and writing it would be adopting Docker behind the
// operator's back. A daemon that cannot reach the host has nothing to
// do. The set is read inside the lock, so a sync that started earlier
// cannot write an older set over a newer one.
func (s *Service) SyncPublished(ctx context.Context) error {
	if !s.reachable() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncPublished(ctx)
}

// syncPublished rewrites the Docker stanza while the caller holds s.mu.
// Keeping the lock ownership explicit avoids deadlocking operations that
// already hold it while they update a rule and then resync the stanza.
func (s *Service) syncPublished(ctx context.Context) error {
	if s.rules != afterRules {
		contents, err := os.ReadFile(s.rules)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read firewall rules: %w", err)
		}
		if err == nil {
			text := string(contents)
			if !strings.Contains(text, dockerBeginMarker) {
				return nil
			}
			if !strings.Contains(text, dockerEndMarker) {
				return fmt.Errorf("firewall rules have the begin marker but not the end marker")
			}
		}
	}

	ports, err := s.exposedPorts(ctx)
	if err != nil {
		return err
	}
	rules, err := s.appsRules(ctx)
	if err != nil {
		return err
	}
	block, err := renderDockerBlockRules(ports, rules)
	if err != nil {
		return err
	}
	path := s.dataDir + "/ufw-docker.rules"
	// Both paths are single-quoted into the script; neither is typed by
	// anybody through the API, and this keeps it that way.
	if strings.ContainsAny(path+s.rules, "'\n") {
		return fmt.Errorf("the data directory %q cannot be named in a shell script", s.dataDir)
	}
	if err := os.WriteFile(path, []byte(block), 0o600); err != nil {
		return fmt.Errorf("write the docker stanza: %w", err)
	}
	return s.script(ctx, replaceScript(path, s.rules))
}

func (s *Service) appsRules(ctx context.Context) ([]Rule, error) {
	// Do not touch UFW when Docker has not been adopted. Apart from avoiding
	// needless host work, this preserves the important no-op guarantee for a
	// local daemon that has no managed stanza yet.
	line := "if grep -qF '" + dockerBeginMarker + "' '" + s.rules + "'; then ufw status numbered; fi"
	res, err := s.host.Script(ctx, line)
	if err != nil {
		return nil, err
	}
	return parseRules(res.Output), nil
}

func (s *Service) exposedPorts(ctx context.Context) ([]int, error) {
	if s.exposed == nil {
		return nil, nil
	}
	ports, err := s.exposed.ExposedPorts(ctx)
	if err != nil {
		return nil, fmt.Errorf("read which ports are exposed: %w", err)
	}
	return ports, nil
}

// replaceScript swaps the block between the markers in rules for the
// one in the file at block.
//
// The new file is built beside the old one and moved over it: a rename
// within one directory, so every step before it leaves the old file
// whole and the step itself leaves the new one. `cp -p` first, so what
// replaces the file keeps its owner and mode. Both markers are required,
// because sed's range with no end runs to the end of the file and would
// take the operator's own lines with it.
//
// The blank line adopting put before the begin marker stays where it
// is, and the block goes on the end without one, so replacing it any
// number of times gives the same file.
func replaceScript(block, rules string) string {
	return `
set -e
rules='` + rules + `'
block='` + block + `'
grep -qF '` + dockerBeginMarker + `' "$rules" 2>/dev/null || exit 0
if ! grep -qF '` + dockerEndMarker + `' "$rules"; then
	echo "cubeship: $rules has the begin marker and not the end one, so it is left as it is"
	exit 1
fi
tmp=$(mktemp "$rules.cubeship.XXXXXX")
trap 'rm -f "$tmp"' EXIT
cp -p "$rules" "$tmp"
sed '/` + dockerBeginMarker + `/,/` + dockerEndMarker + `/d' "$rules" > "$tmp"
if [ -s "$tmp" ] && [ -n "$(tail -c 1 "$tmp")" ]; then echo >> "$tmp"; fi
cat "$block" >> "$tmp"
mv -f "$tmp" "$rules"
if ufw status | grep -q '^Status: active'; then ufw reload; fi
`
}

// releaseScript removes everything between the markers, which is
// exactly what was added and nothing an operator wrote by hand.
const releaseScript = `
set -e
sed -i '/` + dockerBeginMarker + `/,/` + dockerEndMarker + `/d' /etc/ufw/after.rules
ufw status | grep -q '^Status: active' && ufw reload || true
`

// run executes a command on the host and turns a non-zero exit into an
// error carrying what the command actually said.
//
// UFW's refusals are sentences — "ERROR: Bad port" — and passing them
// through is the difference between a screen that explains itself and
// one that says the operation failed.
// Add writes one rule through a Host, without a Service and without a
// caller.
//
// It exists for `internal/mesh`, which opens the cluster's ports on
// every machine — including the workers, where there is no Service
// because there is no database, no operator and no screen. What it
// keeps is the guarantee that matters: the spec is checked, so nothing
// user-supplied reaches a command line, and `Spec.Args` builds argv
// rather than a string.
//
// Idempotent, because `ufw allow` is: an identical rule is skipped
// rather than duplicated.
func Add(ctx context.Context, host Host, spec Spec) error {
	if err := spec.Check(); err != nil {
		return err
	}
	if host == nil || !host.Available() {
		return hostexec.ErrUnavailable
	}
	res, err := host.Run(ctx, spec.Args()...)
	if err != nil {
		return err
	}
	if !res.OK() {
		return fmt.Errorf("%s", firstLine(res.Output))
	}
	return nil
}

func (s *Service) run(ctx context.Context, argv ...string) error {
	if !s.reachable() {
		return hostexec.ErrUnavailable
	}
	res, err := s.host.Run(ctx, argv...)
	if err != nil {
		return err
	}
	if !res.OK() {
		return fmt.Errorf("%s", firstLine(res.Output))
	}
	return nil
}

func (s *Service) script(ctx context.Context, line string) error {
	if !s.reachable() {
		return hostexec.ErrUnavailable
	}
	res, err := s.host.Script(ctx, line)
	if err != nil {
		return err
	}
	if !res.OK() {
		return fmt.Errorf("%s", firstLine(res.Output))
	}
	return nil
}

func firstLine(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return "the host refused, and said nothing about why"
	}
	if line, _, found := strings.Cut(out, "\n"); found {
		return line
	}
	return out
}
