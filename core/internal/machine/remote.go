package machine

import (
	"context"
	"fmt"
	"math"
	"time"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// Telemetry is sampled by the machine that owns the counters, never inferred
// from container usage. Sample may be nil while establishing a rate baseline.
type Telemetry struct {
	Sample           *Sample           `json:"sample,omitempty"`
	Cores            int               `json:"cores"`
	MemoryTotalBytes int64             `json:"memory_total_bytes"`
	DiskTotalBytes   int64             `json:"disk_total_bytes"`
	DiskPath         string            `json:"disk_path"`
	Interfaces       []string          `json:"interfaces,omitempty"`
	Unavailable      map[string]string `json:"unavailable,omitempty"`
}

func (c *Collector) Telemetry() Telemetry {
	out := Telemetry{Sample: c.Measure(), Cores: c.reader.Cores(), DiskPath: c.reader.DiskPath()}
	if v, err := c.reader.Memory(); err == nil {
		out.MemoryTotalBytes = v.Total
	}
	if v, err := c.reader.Disk(); err == nil {
		out.DiskTotalBytes = v.Total
	}
	if _, names, err := c.reader.Network(); err == nil {
		out.Interfaces = names
	}
	out.Unavailable = c.reader.Unavailable()
	return out
}

type Hosts interface {
	ResolveMachine(context.Context, *user.User, string) (int64, bool, error)
}

func (s *Service) SetHosts(hosts Hosts) { s.hosts = hosts }

func (s *Service) SeriesOn(ctx context.Context, caller *user.User, window, server string) (Series, error) {
	if server == "" {
		return s.Series(ctx, caller, window)
	}
	if s.hosts == nil {
		return Series{}, fmt.Errorf("machine selection is unavailable")
	}
	id, local, err := s.hosts.ResolveMachine(ctx, caller, server)
	if err != nil {
		return Series{}, err
	}
	if local {
		return s.Series(ctx, caller, window)
	}
	w, err := metrics.ParseWindow(window)
	if err != nil {
		return Series{}, err
	}
	report, err := s.Repo().Report(ctx, id)
	if err != nil {
		return Series{}, err
	}
	samples, err := s.Repo().Series(ctx, w, now(), id)
	if err != nil {
		return Series{}, err
	}
	if samples == nil {
		samples = []Sample{}
	}
	out := Series{Window: w.Name, Samples: samples, Cores: report.Cores, MemoryTotalBytes: report.MemoryTotalBytes,
		DiskTotalBytes: report.DiskTotalBytes, DiskPath: report.DiskPath, Interfaces: report.Interfaces, Unavailable: report.Unavailable}
	if report.DiskPath == "" {
		out.Unavailable = map[string]string{}
		for _, key := range []string{MeasureCPU, MeasureMemory, MeasureDisk, MeasureNetwork} {
			out.Unavailable[key] = "This worker has not reported host monitoring yet. Update it to the current Cubeship release and wait for its next report."
		}
	}
	out.SampledAt, err = s.Repo().SampledAt(ctx, id)
	return out, err
}

// RecordTelemetry is called only after authenticating the reporting node. Its
// identity comes from the credential, never from fields in the report.
func RecordTelemetry(ctx context.Context, db *database.DB, id int64, report Telemetry) error {
	if len(report.DiskPath) > 4096 || len(report.Interfaces) > 128 {
		return fmt.Errorf("invalid host report")
	}
	if v := report.Sample; v != nil {
		validRate := func(p *float64) bool { return p == nil || (!math.IsNaN(*p) && !math.IsInf(*p, 0) && *p >= 0) }
		if v.At.Before(time.Now().Add(-metrics.Retention)) || v.At.After(time.Now().Add(time.Minute)) ||
			math.IsNaN(v.CPUPercent) || math.IsInf(v.CPUPercent, 0) || v.CPUPercent < 0 || v.CPUPercent > 100 ||
			v.MemoryBytes < 0 || v.MemoryTotalBytes < 0 || v.DiskBytes < 0 || v.DiskTotalBytes < 0 ||
			!validRate(v.RxBytesPerSec) || !validRate(v.TxBytesPerSec) {
			return fmt.Errorf("invalid host sample")
		}
	}
	return db.WithTx(ctx, func(tx database.Queryer) error {
		repo := NewRepository(tx)
		if err := repo.SaveReport(ctx, id, report); err != nil {
			return err
		}
		if report.Sample != nil {
			return repo.Insert(ctx, *report.Sample, id)
		}
		return nil
	})
}
