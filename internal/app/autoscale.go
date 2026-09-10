package app

import (
	"context"
	"errors"
	"log"
	"math"
	"time"

	"cubeship/internal/metrics"
)

// Autoscale is when this instance decides an app's replica count for
// itself.
//
// **One signal, and it is CPU.** It is the only one where adding a copy
// changes the number: memory does not fall because there are more
// replicas — each still holds what it holds — so a memory rule would
// scale up forever and never come back down. Requests per second would
// be the other honest signal and this instance does not measure them.
//
// The reading is the **average across the app's copies**, which is what
// its chart already is: every replica records against the app, on any
// machine, through the same `metric_samples` table.
type Autoscale struct {
	// Min is the floor, and Max the ceiling. **Zero Max is off**, which
	// is what every app is until somebody says otherwise — and it is
	// the field the whole feature hangs on, so there is no separate
	// "enabled" flag to disagree with it.
	//
	// A ceiling is not optional. Without one a loop of requests is a
	// loop of replicas until the machine has nothing left, which is a
	// worse outage than the one autoscaling was turned on to avoid.
	Min int `json:"min"`
	Max int `json:"max"`
	// CPU is what each copy should be sitting at, as a percentage where
	// **100 is one core** — the same scale every container chart here
	// is drawn on, deliberately, so the number somebody types is the
	// number they were looking at.
	CPU float64 `json:"cpu"`
	// At is when this last changed the count. Reported so a screen can
	// say why nothing is happening, and it is what the cooldown is
	// measured from.
	At *time.Time `json:"at,omitempty"`
}

// On reports whether this app is scaled by the instance.
func (a Autoscale) On() bool { return a.Max > 0 }

// The numbers behind the rule, all of them deliberately not
// configurable: they are the difference between a rule that settles and
// one that oscillates, and every one of them is a thing somebody would
// otherwise have to learn by watching an app flap.
const (
	// AutoscaleInterval is how often the rule is asked. It is the
	// collection interval: asking more often than the data changes is
	// asking the same question twice.
	AutoscaleInterval = metrics.Interval

	// AutoscaleWindow is how much history the average is taken over.
	// One sample is noise, and scaling on a thirty-second spike is how
	// a rule ends up chasing itself.
	AutoscaleWindow = 3 * time.Minute

	// AutoscaleSamples is how many readings the window must hold before
	// the rule will act. An app that has just been deployed, or one on
	// a machine that has just come back, has a window with one point in
	// it — and a decision from one point is a decision from noise.
	AutoscaleSamples = 3

	// AutoscaleTolerance is how far off target the average has to be
	// before anything moves. Without it every pass finds the ratio is
	// not exactly 1 and asks for a count one different from the one it
	// has, forever.
	AutoscaleTolerance = 0.1

	// AutoscaleCooldown is how long after a change the rule waits
	// before making another. A copy takes time to start and longer to
	// take its share of the load, and acting again before that is
	// acting on a reading that does not include the last decision.
	AutoscaleCooldown = 3 * time.Minute

	// AutoscaleDownAfter is the same wait for **shrinking**, and it is
	// longer on purpose. Being wrong in the two directions costs
	// different things: an extra copy costs some memory, and one copy
	// too few costs the app its latency at exactly the moment load is
	// coming back. So it goes up quickly and comes down slowly, which
	// is the one asymmetry worth having in a rule this small.
	AutoscaleDownAfter = 10 * time.Minute
)

// MaxAutoscale is the largest ceiling this instance will accept.
//
// Not a resource limit — it is a typo limit. A ceiling of 1000 on a box
// that runs a handful of containers is somebody who meant 10, and the
// rule would obediently work towards it.
const MaxAutoscale = 100

// ErrInvalidAutoscale is a rule this instance will not run.
var ErrInvalidAutoscale = errors.New("autoscaling needs a maximum of at least 1 and at most 100, a minimum between 1 and that maximum, and a target CPU above 0 where 100 is one core")

// Valid reports whether this is a rule that can settle.
func (a Autoscale) Valid() bool {
	if !a.On() {
		// Off, and the other two are not read. Refusing a stale
		// minimum left behind by turning it off would be refusing
		// somebody's own previous answer back at them.
		return a.Min >= 0 && a.CPU >= 0 && a.Max == 0
	}
	if a.Max > MaxAutoscale || a.Min < 1 || a.Min > a.Max {
		return false
	}
	return a.CPU > 0
}

// Want is how many copies the rule asks for, given what the copies are
// doing now.
//
// The arithmetic is the one every autoscaler uses, and it is worth
// having in one small function that can be read: if each of `running`
// copies is averaging `cpu` and each should be averaging `target`, then
// the number that would put them there is `running * cpu / target`,
// rounded up — because half a copy does not exist and rounding down
// leaves every copy above target.
//
// Clamped to the app's own floor and ceiling, and answered as `running`
// when the ratio is within tolerance: a rule that asks for a different
// number every pass is a rule that never settles.
func (a Autoscale) Want(running int, cpu float64) int {
	if !a.On() || running < 1 || a.CPU <= 0 {
		return running
	}
	ratio := cpu / a.CPU
	if math.Abs(ratio-1) <= AutoscaleTolerance {
		return running
	}
	want := int(math.Ceil(float64(running) * ratio))
	return min(max(want, a.Min), a.Max)
}

// Autoscaler asks the rule on a timer and acts on the answer.
//
// It runs in cmd/cubeshipd rather than in server.New, for the reason the
// metrics collector does: a server is a request handler, and a test that
// builds one must not thereby start changing how many containers exist.
type Autoscaler struct {
	Apps    *Service
	Metrics *metrics.Service
}

// Run asks on every interval until ctx is done.
func (a *Autoscaler) Run(ctx context.Context) {
	ticker := time.NewTicker(AutoscaleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.Once(ctx)
		}
	}
}

// Once is one pass over every app that is scaled by the instance.
//
// One app failing is not a reason to stop asking about the others: what
// goes wrong here is a machine that will not start a container, and the
// next pass is a perfectly good place to find that out again.
func (a *Autoscaler) Once(ctx context.Context) {
	apps, err := a.Apps.Repo().Autoscaling(ctx)
	if err != nil {
		log.Printf("autoscale: reading which apps are scaled here: %v", err)
		return
	}
	for _, app := range apps {
		if err := a.one(ctx, app); err != nil {
			log.Printf("autoscale: %s: %v", ReferenceOf(app), err)
		}
	}
}

func (a *Autoscaler) one(ctx context.Context, app *Scoped) error {
	rule := app.Autoscale
	if !rule.On() {
		return nil
	}
	running := 0
	for _, r := range app.Replicas {
		if r.Running() {
			running++
		}
	}
	if running == 0 {
		// Nothing is serving, so there is no reading to divide and
		// nothing this can usefully do. An app that is down is a
		// deploy's problem, not a scale's — and multiplying a count by
		// a CPU figure nobody produced is how a rule invents a number.
		return nil
	}

	cpu, samples, err := a.Metrics.AverageCPU(ctx, metrics.KindApp, app.ID, AutoscaleWindow)
	if err != nil {
		return err
	}
	if samples < AutoscaleSamples {
		return nil
	}

	want := rule.Want(running, cpu)
	if want == len(app.Replicas) {
		return nil
	}
	if !ready(rule.At, want < len(app.Replicas)) {
		return nil
	}

	log.Printf("autoscale: %s is averaging %.0f%% of a core across %d cop%s, target %.0f%% — going to %d",
		ReferenceOf(app), cpu, running, plural(running), rule.CPU, want)
	return a.Apps.scaleTo(ctx, app, want)
}

// ready is the cooldown: whether enough time has passed since the last
// change this rule made.
//
// Longer before shrinking, because being wrong in the two directions
// costs different things — see AutoscaleDownAfter.
func ready(at *time.Time, shrinking bool) bool {
	if at == nil {
		return true
	}
	wait := AutoscaleCooldown
	if shrinking {
		wait = AutoscaleDownAfter
	}
	return time.Since(*at) >= wait
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
