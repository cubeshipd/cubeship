package project_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"cubeship/internal/server/servertest"

	"cubeship/internal/envvar"
	"cubeship/internal/project"
	"cubeship/internal/user"
)

func inheritedEnvWrites(s *project.Service, caller *user.User) map[string]func() error {
	ctx := context.Background()
	vars := envvar.Map{"RAILPACK_BUILD_CMD": "echo untrusted", "ORDINARY": "value"}
	return map[string]func() error{
		"replace project": func() error { _, err := s.SetEnv(ctx, caller, "web", vars); return err },
		"merge project":   func() error { _, err := s.MergeEnv(ctx, caller, "web", vars, nil); return err },
		"unset project": func() error {
			_, err := s.MergeEnv(ctx, caller, "web", nil, []string{"RAILPACK_INSTALL_CMD"})
			return err
		},
		"replace environment": func() error { _, err := s.SetEnvironmentEnv(ctx, caller, "web", "production", vars); return err },
		"merge environment":   func() error { _, err := s.MergeEnvironmentEnv(ctx, caller, "web", "production", vars, nil); return err },
		"unset environment": func() error {
			_, err := s.MergeEnvironmentEnv(ctx, caller, "web", "production", nil, []string{"RAILPACK_INSTALL_CMD"})
			return err
		},
	}
}

func TestInheritedEnvRejectsNonAdminsBeforeDatabaseAccess(t *testing.T) {
	policy := func(level user.Level, items []string) user.Policy {
		return user.NewPolicy([]user.Grant{{Resource: user.ResProjects, Level: level, Items: items}})
	}
	for _, tt := range []struct {
		name   string
		caller *user.User
		want   error
	}{
		{"anonymous", nil, user.ErrUnauthenticated},
		{"default member", &user.User{Role: user.RoleMember}, user.ErrForbidden},
		{"member with project manage", &user.User{Role: user.RoleMember, Policy: policy(user.LevelManage, []string{"web"})}, user.ErrForbidden},
		{"admin key with view", &user.User{Role: user.RoleAdmin, Policy: policy(user.LevelView, []string{"web"})}, user.ErrForbidden},
		{"admin key outside project", &user.User{Role: user.RoleAdmin, Policy: policy(user.LevelManage, []string{"other"})}, project.ErrNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// A refusal must not depend on the current apps, their sources, or a DB lookup.
			for name, write := range inheritedEnvWrites(project.NewService(nil, ""), tt.caller) {
				t.Run(name, func(t *testing.T) {
					defer func() {
						if p := recover(); p != nil {
							t.Errorf("unauthorized inherited env write reached database: %v", p)
						}
					}()
					if err := write(); !errors.Is(err, tt.want) {
						t.Fatalf("got %v, want %v", err, tt.want)
					}
				})
			}
		})
	}
}

// CI runs this with Postgres; the local short suite still exercises every
// refusal above without starting infrastructure.
func TestInheritedEnvAdminKeysPersistAndMembersCannotChangeBuildInput(t *testing.T) {
	f := servertest.New(t)
	ctx := context.Background()
	svc := project.NewService(f.DB, f.DataDir)
	policy := user.NewPolicy([]user.Grant{{Resource: user.ResProjects, Level: user.LevelManage, Secrets: true, Items: []string{"web"}}})
	adminKey := &user.User{Role: user.RoleAdmin, Policy: policy}
	member := &user.User{Role: user.RoleMember, Policy: policy}
	for name, write := range inheritedEnvWrites(svc, adminKey) {
		t.Run(name, func(t *testing.T) {
			if err := write(); err != nil {
				t.Fatalf("scoped admin key: %v", err)
			}
		})
	}
	trusted := envvar.Map{"RAILPACK_BUILD_CMD": "echo trusted", "RAILPACK_INSTALL_CMD": "echo install"}
	if _, err := svc.SetEnv(ctx, adminKey, "web", trusted); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetEnvironmentEnv(ctx, adminKey, "web", "production", trusted); err != nil {
		t.Fatal(err)
	}
	beforeProject, err := svc.Env(ctx, adminKey, "web")
	if err != nil {
		t.Fatal(err)
	}
	beforeEnv, beforeInherited, err := svc.EnvironmentEnv(ctx, adminKey, "web", "production")
	if err != nil {
		t.Fatal(err)
	}
	if beforeProject["RAILPACK_BUILD_CMD"] != "echo trusted" || beforeEnv["RAILPACK_BUILD_CMD"] != "echo trusted" {
		t.Fatal("admin writes did not persist to both inherited layers")
	}
	for name, write := range inheritedEnvWrites(svc, member) {
		t.Run("member "+name, func(t *testing.T) {
			if err := write(); !errors.Is(err, user.ErrForbidden) {
				t.Fatalf("got %v, want forbidden", err)
			}
		})
	}
	afterProject, err := svc.Env(ctx, adminKey, "web")
	if err != nil {
		t.Fatal(err)
	}
	afterEnv, afterInherited, err := svc.EnvironmentEnv(ctx, adminKey, "web", "production")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeProject, afterProject) || !reflect.DeepEqual(beforeEnv, afterEnv) || !reflect.DeepEqual(beforeInherited, afterInherited) {
		t.Fatal("member changed inherited build input")
	}
}
