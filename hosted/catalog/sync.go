package catalog

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"

	"cubeship/template"
)

// Syncer runs one pass over the catalog.
type Syncer struct {
	GitHub GitHub
	Store  Store
	Topic  string
	Log    *log.Logger
}

// Report is what one pass did.
type Report struct {
	Repositories int `json:"repositories"`
	Accepted     int `json:"accepted"`
	Rejected     int `json:"rejected"`
	Hidden       int `json:"hidden"`
	// Failed counts repositories and releases left for the next pass
	// because something on the way — GitHub or the database —
	// did not answer. Nothing is recorded for them.
	Failed int `json:"failed"`
}

// Run is one pass. It returns an error only when the pass could not
// know which repositories exist; everything below that is per
// repository, logged, and tried again next time.
func (s *Syncer) Run(ctx context.Context) (Report, error) {
	var rep Report
	blocked, err := s.Store.Blocked(ctx)
	if err != nil {
		return rep, fmt.Errorf("read the blocklist: %w", err)
	}
	found, err := s.GitHub.Search(ctx, s.Topic)
	if err != nil {
		return rep, fmt.Errorf("search GitHub: %w", err)
	}

	seen := map[string]bool{}
	for _, r := range found {
		seen[r.NodeID] = true
		s.repository(ctx, r, blocked, &rep)
	}

	// Search is an index, and an index can lag or drop a result. A known
	// repository it did not return is asked about directly before
	// anything is hidden.
	known, err := s.Store.Repositories(ctx)
	if err != nil {
		return rep, fmt.Errorf("read the catalog: %w", err)
	}
	var missing []Known
	for _, k := range known {
		if !seen[k.NodeID] {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return rep, nil
	}
	ids := make([]string, len(missing))
	for i, k := range missing {
		ids[i] = k.NodeID
	}
	still, err := s.GitHub.Lookup(ctx, ids)
	if err != nil {
		return rep, fmt.Errorf("look up %d repositories search did not return: %w", len(ids), err)
	}
	for _, k := range missing {
		r, ok := still[k.NodeID]
		switch {
		case !ok || r.Private:
			if err := s.Store.Hide(ctx, k.ID, HiddenGone); err != nil {
				s.fail(&rep, "hide repository %d: %v", k.ID, err)
				continue
			}
			rep.Hidden++
		case !slices.Contains(r.Topics, s.Topic):
			if err := s.Store.SaveRepository(ctx, r, HiddenUntagged); err != nil {
				s.fail(&rep, "save %s/%s: %v", r.Owner, r.Name, err)
				continue
			}
			rep.Hidden++
		default:
			s.repository(ctx, r, blocked, &rep)
		}
	}
	return rep, nil
}

func (s *Syncer) repository(ctx context.Context, r Repo, blocked map[string]bool, rep *Report) {
	rep.Repositories++
	hidden := ""
	if r.Private {
		hidden = HiddenGone
	} else if blocked[strings.ToLower(r.Owner)] || blocked[strings.ToLower(r.Owner+"/"+r.Name)] {
		hidden = HiddenBlocked
	}
	if err := s.Store.SaveRepository(ctx, r, hidden); err != nil {
		s.fail(rep, "save %s/%s: %v", r.Owner, r.Name, err)
		return
	}
	if hidden != "" {
		rep.Hidden++
		return
	}

	for _, rel := range r.Releases {
		// A prerelease is its author saying "not yet", and a draft is not
		// public. A tag GitHub cannot resolve to a commit has nothing to
		// pin a read to.
		if rel.Draft || rel.Prerelease || rel.Commit == "" {
			continue
		}
		done, err := s.Store.Indexed(ctx, r.ID, rel.Tag, rel.Commit)
		if err != nil {
			s.fail(rep, "%s/%s %s: %v", r.Owner, r.Name, rel.Tag, err)
			continue
		}
		if done {
			continue
		}
		rec, err := s.index(ctx, r, rel)
		if err == nil {
			err = s.Store.SaveRelease(ctx, rec)
		}
		if err != nil {
			s.fail(rep, "%s/%s %s: %v", r.Owner, r.Name, rel.Tag, err)
			continue
		}
		if rec.Accepted {
			rep.Accepted++
		} else {
			rep.Rejected++
		}
		s.Log.Printf("%s/%s %s: %s", r.Owner, r.Name, rel.Tag, verdict(rec))
	}
}

// index reads one release. An error means "try again later"; a release
// that is simply wrong comes back rejected, with the reasons.
func (s *Syncer) index(ctx context.Context, r Repo, rel Release) (Indexed, error) {
	rec := Indexed{
		RepositoryID: r.ID, Tag: rel.Tag, Commit: rel.Commit, Name: rel.Name,
		URL: rel.URL, PublishedAt: rel.PublishedAt, Problems: []template.Diagnostic{},
	}
	if rec.Name == "" {
		rec.Name = rel.Tag
	}
	problem := func(code, message, hint string) {
		rec.Problems = append(rec.Problems, template.Diagnostic{
			Severity: template.Error, Code: code, Message: message, Path: []any{}, Hint: hint,
		})
	}
	fetch := func(path string, limit int64) ([]byte, bool, error) {
		b, err := s.GitHub.File(ctx, r.Owner, r.Name, rel.Commit, path, limit)
		switch {
		case errors.Is(err, ErrNotFound):
			return nil, false, nil
		case errors.Is(err, ErrTooLarge):
			problem("release.too-large", fmt.Sprintf("%s is over %d KB", path, limit>>10), "")
			return nil, true, nil
		case err != nil:
			return nil, false, fmt.Errorf("read %s: %w", path, err)
		}
		return b, true, nil
	}

	source, present, err := fetch(TemplateFile, templateLimit)
	if err != nil {
		return rec, err
	}
	if !present {
		hint := ""
		if _, yml, err := fetch("template.yml", templateLimit); err != nil {
			return rec, err
		} else if yml {
			hint = "rename template.yml to template.yaml"
		}
		problem("release.template-missing", "the release has no template.yaml at the root of its commit", hint)
	} else if source != nil {
		result := template.Validate(source)
		rec.Problems = append(rec.Problems, result.Diagnostics...)
		rec.Manifest = result.Manifest
		rec.Source = string(source)
	}

	readme, present, err := fetch(ReadmeFile, readmeLimit)
	if err != nil {
		return rec, err
	}
	if !present {
		problem("release.readme-missing", "the release has no README.md at the root of its commit", "")
	}
	rec.Readme = string(readme)

	icon, present, err := fetch(IconFile, iconLimit)
	if err != nil {
		return rec, err
	}
	if !present {
		problem("release.icon-missing", "the release has no icon.png at the root of its commit", "")
	} else if icon != nil {
		clean, code, message := checkIcon(icon)
		if code != "" {
			problem(code, message, "")
		}
		rec.Problems = append(rec.Problems, iconAdvice(clean)...)
		icon = clean
	}

	rec.Accepted = !template.Blocks(rec.Problems)
	if !rec.Accepted {
		// A rejection keeps its reasons and nothing that could be shown
		// as though it were a template.
		rec.Manifest, rec.Source, rec.Readme = nil, "", ""
		return rec, nil
	}
	rec.Icon = icon
	return rec, nil
}

func (s *Syncer) fail(rep *Report, format string, args ...any) {
	rep.Failed++
	s.Log.Printf(format, args...)
}

func verdict(rec Indexed) string {
	if rec.Accepted {
		return "accepted"
	}
	codes := make([]string, 0, len(rec.Problems))
	for _, p := range rec.Problems {
		if p.Severity == template.Error {
			codes = append(codes, p.Code)
		}
	}
	return "rejected: " + strings.Join(codes, ", ")
}
