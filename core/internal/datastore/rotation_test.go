package datastore

import (
	"strings"
	"testing"
)

func TestPasswordRotationUsesStoredLoginAndQuotesNewPassword(t *testing.T) {
	d := &Datastore{Engine: EnginePostgres, Username: `app"role`, Password: "old"}
	cmd, env, input := passwordRotation(d, "new'password")
	if len(cmd) == 0 || cmd[0] != "psql" {
		t.Fatalf("command = %v", cmd)
	}
	if len(env) != 1 || env[0] != "PGPASSWORD=old" {
		t.Fatalf("environment = %v", env)
	}
	if !strings.Contains(input, `"app""role"`) || !strings.Contains(input, "'new''password'") {
		t.Fatalf("rotation SQL did not safely quote credentials: %s", input)
	}
}

func TestPasswordRotationCoversEverySupportedEngine(t *testing.T) {
	for _, engine := range []Engine{EnginePostgres, EngineMySQL, EngineMariaDB, EngineRedis, EngineMongoDB} {
		t.Run(string(engine), func(t *testing.T) {
			cmd, env, input := passwordRotation(&Datastore{Engine: engine, Username: "app", Password: "old"}, "next")
			if len(cmd) == 0 || input == "" {
				t.Fatalf("rotation missing for %s", engine)
			}
			if len(env) == 0 && !strings.Contains(input, "old") {
				t.Fatalf("stored password is not supplied to %s", engine)
			}
			if engine == EngineRedis && !strings.Contains(input, "resetpass >next") {
				t.Fatalf("Redis rotation did not revoke the old password: %s", input)
			}
		})
	}
}
