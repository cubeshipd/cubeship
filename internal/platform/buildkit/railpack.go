package buildkit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/railwayapp/railpack/core"
	"github.com/railwayapp/railpack/core/app"
)

// FrontendImage is the BuildKit frontend that turns a Railpack plan into
// an image.
//
// Its version must match the Railpack this binary generates plans with:
// a plan is a versioned document, and a frontend reading one it does not
// understand is a build that fails for no reason anybody can see. The
// module version in go.mod is the single source of that.
const FrontendImage = "ghcr.io/railwayapp/railpack-frontend:v" + railpackVersion

// railpackVersion must equal the github.com/railwayapp/railpack version
// in go.mod. There is no way to read a dependency's version at compile
// time, so a test asserts the two agree.
const railpackVersion = "0.39.0"

// ErrNoStartCommand reports a repository Railpack understood but could
// not find a way to run.
var ErrNoStartCommand = fmt.Errorf("railpack could not work out how to start this app")

// PlanRepository works out how to build the source in dir, without
// anybody having written a Dockerfile.
//
// env is the app's environment: Railpack reads it for the versions and
// commands a project can pin (NODE_VERSION, a build command), so a
// build's result depends on it and not only on the code.
func PlanRepository(dir string, env map[string]string) (plan []byte, providers []string, err error) {
	source, err := app.NewApp(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read the source: %w", err)
	}
	environment := app.NewEnvironment(&env)

	// GenerateBuildPlan reports a source it cannot plan through
	// Success rather than through err — err is for transient failures.
	result, err := core.GenerateBuildPlan(source, environment, &core.GenerateBuildPlanOptions{
		RailpackVersion:          railpackVersion,
		ErrorMissingStartCommand: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("plan the build: %w", err)
	}
	if !result.Success {
		return nil, nil, planFailure(result)
	}

	encoded, err := json.Marshal(result.Plan)
	if err != nil {
		return nil, nil, fmt.Errorf("encode the build plan: %w", err)
	}
	return encoded, result.DetectedProviders, nil
}

// planFailure turns Railpack's log into one error worth showing. Its
// messages are what tell someone their repository is missing a start
// command or a lockfile, and dropping them for "planning failed" would
// leave nothing to act on.
func planFailure(result *core.BuildResult) error {
	var detail string
	for _, msg := range result.Logs {
		if msg.Msg == "" {
			continue
		}
		if detail != "" {
			detail += "; "
		}
		detail += msg.Msg
	}
	if detail == "" {
		detail = "no provider recognized this repository"
	}
	return fmt.Errorf("railpack could not plan this build: %s", detail)
}

// WritePlan puts a plan where the frontend expects to find it: a
// directory of its own, passed as the "dockerfile" local context under
// the name railpack-plan.json.
func WritePlan(plan []byte) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "cubeship-plan-")
	if err != nil {
		return "", nil, fmt.Errorf("create a directory for the build plan: %w", err)
	}
	cleanup = func() { os.RemoveAll(dir) }

	if err := os.WriteFile(filepath.Join(dir, "railpack-plan.json"), plan, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write the build plan: %w", err)
	}
	return dir, cleanup, nil
}

// BuildPlanned builds a plan that PlanRepository produced.
//
// It goes through BuildKit's gateway frontend rather than the Dockerfile
// one: Railpack ships its own frontend image, and that image is what
// reads the plan.
func (b *Builder) BuildPlanned(ctx context.Context, req PlannedRequest, logs io.Writer) error {
	if req.ContextDir == "" {
		return fmt.Errorf("build: no source to build")
	}
	if req.PlanDir == "" {
		return fmt.Errorf("build: no build plan")
	}
	return b.solve(ctx, solveRequest{
		image:    req.Image,
		frontend: "gateway.v0",
		attrs: map[string]string{
			"source": FrontendImage,
			// Mount caches are shared across builds, so they are keyed
			// per app: two apps sharing one would fight over it.
			"cache-key": req.CacheKey,
		},
		localDirs: map[string]string{
			"context":    req.ContextDir,
			"dockerfile": req.PlanDir,
		},
	}, logs)
}

// PlannedRequest is one Railpack build.
type PlannedRequest struct {
	ContextDir string
	PlanDir    string
	Image      string
	// CacheKey isolates this app's mount caches from every other app's.
	CacheKey string
}
