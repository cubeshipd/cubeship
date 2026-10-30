package templateinstall

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"cubeship/internal/app"
	"cubeship/internal/user"
)

// runUpdate applies an update and records the template it brought the
// installation to, or puts everything back when a step fails.
func (s *Service) runUpdate(caller *user.User, in *Install, run *Run, up *updatePlan) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeoutFor(up.manifest))
	defer cancel()
	if err := s.applyUpdate(ctx, caller, run, up); err != nil {
		s.rollbackUpdate(caller, run, err)
		return
	}
	in.Release, in.Commit, in.Manifest = up.release.Tag, up.release.Commit, up.manifest
	in.Answers = up.answers()
	// A domain an earlier update added and this one took away is gone.
	in.Resources = slices.DeleteFunc(in.Resources, func(r Resource) bool {
		return r.Kind == KindDomain && slices.ContainsFunc(run.Snapshot, func(snap AppSnapshot) bool {
			return slices.ContainsFunc(snap.RemovedDomains, func(d SnapshotDomain) bool { return r.Name == snap.Ref+" "+d.Host })
		})
	})
	in.Resources = append(in.Resources, run.Created...)
	s.saveInstall(in)
	s.finish(run, nil)
}

func (s *Service) applyUpdate(ctx context.Context, caller *user.User, run *Run, up *updatePlan) error {
	created := func(kind, key, name string) {
		run.Created = append(run.Created, Resource{Kind: kind, Key: key, Name: name})
		s.saveRun(run)
	}
	m := up.manifest

	changed := make([]string, 0, len(up.changed))
	for key := range up.changed {
		changed = append(changed, key)
	}
	sort.Strings(changed)

	s.step(run, "Recording the apps as they are")
	for _, key := range changed {
		ch, ref := up.changed[key], up.ref(key)
		a, err := s.apps.Resolve(ctx, caller, ref, RoleToInstall)
		if err != nil {
			return fmt.Errorf("read app %s: %w", ref, err)
		}
		own, _, err := s.apps.Env(ctx, caller, ref)
		if err != nil {
			return fmt.Errorf("read the variables of app %s: %w", ref, err)
		}
		snap := AppSnapshot{
			Ref: ref.String(), SourceChanged: ch.source, SettingsChanged: ch.settings, NetworkingChanged: ch.networking,
			Source: a.Source, Image: a.SourceImage, Tag: a.SourceTag, Repo: a.SourceRepo,
			GitRef: a.SourceRef, Dockerfile: a.SourceDockerfile, Health: a.HealthPath,
			InternalPorts: append([]int(nil), a.InternalPorts...), HealthChecks: append([]app.HealthCheck(nil), a.HealthChecks...),
			CPU: a.Limits.CPU, Memory: a.Limits.Memory,
			AutoscaleMin: a.Autoscale.Min, AutoscaleMax: a.Autoscale.Max, AutoscaleCPU: a.Autoscale.CPU,
			Scale: a.Scale, Spread: a.Spread, Env: map[string]*string{},
		}
		for _, name := range ch.env {
			if value, ok := own[name]; ok {
				snap.Env[name] = &value
			} else {
				snap.Env[name] = nil
			}
		}
		for _, d := range ch.dropped {
			host := app.NormalizeHost(hostOf(d, up.install.Answers))
			for _, have := range a.Domains {
				if have.Host == host {
					snap.RemovedDomains = append(snap.RemovedDomains, SnapshotDomain{Host: have.Host, Port: have.Port})
				}
			}
		}
		run.Snapshot = append(run.Snapshot, snap)
	}
	s.saveRun(run)

	for _, db := range m.Databases {
		if up.newDatabases[db.Key] {
			if err := s.createDatabase(ctx, caller, run, up.plan, db, created); err != nil {
				return err
			}
		}
	}
	for _, st := range m.Stores {
		if up.newStores[st.Key] {
			if err := s.createStore(ctx, caller, run, up.plan, st, created); err != nil {
				return err
			}
		}
	}
	for _, a := range m.Apps {
		if up.newApps[a.Key] {
			if err := s.createApp(ctx, caller, run, up.plan, a, created); err != nil {
				return err
			}
		}
	}
	for _, key := range changed {
		ch, ref, a := up.changed[key], up.ref(key), up.app(key)
		s.step(run, "Changing app %s", ref)
		if ch.source {
			source, origin := sourceOf(a)
			if _, err := s.apps.Update(ctx, caller, ref, &source, &origin, nil, nil, nil, nil); err != nil {
				return fmt.Errorf("change the source of app %s: %w", ref, err)
			}
		}
		if ch.settings {
			if err := s.configure(ctx, caller, ref, a); err != nil {
				return fmt.Errorf("configure app %s: %w", ref, err)
			}
		}
	}

	for _, db := range m.Databases {
		if up.newDatabases[db.Key] {
			if err := s.waitDatabase(ctx, caller, run, up.databases[db.Key]); err != nil {
				return err
			}
		}
	}
	for _, st := range m.Stores {
		buckets := up.newBuckets[st.Key]
		if up.newStores[st.Key] {
			if err := s.waitStore(ctx, caller, run, up.stores[st.Key]); err != nil {
				return err
			}
			buckets = st.Buckets
		}
		for _, bucket := range buckets {
			if err := s.stores.CreateBucket(ctx, caller, up.stores[st.Key], bucket); err != nil {
				return fmt.Errorf("create bucket %s in %s: %w", bucket, up.stores[st.Key], err)
			}
		}
	}

	// Before any domain is added: a template that moves a domain from one
	// app to another needs it free.
	for _, snap := range run.Snapshot {
		if err := s.dropDomains(ctx, caller, run, snap); err != nil {
			return err
		}
	}

	var redeploy []app.Reference
	for _, a := range m.Apps {
		var err error
		switch ch := up.changed[a.Key]; {
		case up.newApps[a.Key]:
			err = s.wire(ctx, caller, run, up.plan, a, a.Domains, a.Attach, envNames(a), created)
		case ch != nil:
			err = s.wire(ctx, caller, run, up.plan, a, ch.domains, ch.attach, ch.env, created)
			if err == nil {
				err = s.addVolumes(ctx, caller, up.ref(a.Key), a.Key, ch.volumes, created)
			}
			if err == nil {
				err = s.addTCPPorts(ctx, caller, up.plan, up.ref(a.Key), a.Key, ch.tcp, created)
			}
			if err == nil && ch.settings {
				err = s.configure(ctx, caller, up.ref(a.Key), a)
			}
		default:
			continue
		}
		if err != nil {
			return err
		}
		redeploy = append(redeploy, up.ref(a.Key))
	}
	for _, ref := range redeploy {
		if err := s.deploy(ctx, caller, run, ref); err != nil {
			return err
		}
	}
	return nil
}

// dropDomains takes off an app the domains its snapshot recorded as going.
func (s *Service) dropDomains(ctx context.Context, caller *user.User, run *Run, snap AppSnapshot) error {
	if len(snap.RemovedDomains) == 0 {
		return nil
	}
	ref, err := app.ParseReference(snap.Ref)
	if err != nil {
		return err
	}
	s.step(run, "Removing domains from app %s", ref)
	for _, d := range snap.RemovedDomains {
		if err := s.remove(ctx, caller, Resource{Kind: KindDomain, Name: snap.Ref + " " + d.Host}, false); err != nil {
			return fmt.Errorf("remove %s from app %s: %w", d.Host, ref, err)
		}
	}
	return nil
}
