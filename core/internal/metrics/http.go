package metrics

import (
	"errors"
	"net/http"
	"strconv"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/platform/openapi"
)

// WriteSeries answers a metrics request for one subject.
//
// Shared rather than duplicated: the app's endpoint and the datastore's
// differ in how they resolve the subject and in nothing else, and two
// copies of "read the window, look it up, render it" is two places for
// them to drift.
//
// Whoever calls this has already resolved the resource at the role its
// own module requires — that is what authorizes the read.
func WriteSeries(w http.ResponseWriter, r *http.Request, svc *Service, kind string, subjectID int64, collecting bool) {
	series, err := svc.Series(r.Context(), kind, subjectID, r.URL.Query().Get("window"), collecting)
	if err != nil {
		if errors.Is(err, ErrUnknownWindow) {
			http.Error(w, err.Error()+": try "+WindowNames(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, series)
}

// WindowNames renders what may be asked for, for an error message and
// for the document.
func WindowNames() string {
	out := ""
	for i, w := range Windows {
		if i > 0 {
			out += ", "
		}
		out += w.Name
	}
	return out
}

// WindowParam is the query parameter every metrics endpoint takes.
func WindowParam() openapi.Parameter {
	p := openapi.QueryParam("window",
		"How much of the past to cover: "+WindowNames()+". Defaults to "+DefaultWindow.Name+
			". Each is bucketed to around "+strconv.Itoa(TargetPoints)+" points, so a chart is the same density whichever is asked for.")
	return p
}

// Schemas are the components a module's OpenAPI declaration merges in
// when it documents a metrics endpoint — the same shape at every
// address that serves one.
func Schemas() map[string]*openapi.Schema {
	return map[string]*openapi.Schema{
		"MetricSeries": openapi.Object(map[string]*openapi.Schema{
			"window":             openapi.String("The window these cover."),
			"samples":            openapi.Array(openapi.Ref("MetricSample")),
			"memory_limit_bytes": openapi.Integer("The memory ceiling the newest sample saw — the cgroup's limit, or the host's total memory for a container with no limit of its own. This is what a memory chart is drawn against. 0 when nothing has been sampled."),
			"collecting":         openapi.Bool("Whether there is a container behind this right now. False with an empty series means there is nothing to sample, which is a different thing from nothing having been sampled yet."),
		}, "window", "samples", "collecting"),
		"MetricSample": openapi.Object(map[string]*openapi.Schema{
			"at":                 openapi.String("The start of the bucket, RFC 3339."),
			"cpu_percent":        {Type: "number", Description: "Percent of one core, so 250 is two and a half cores. Deliberately not capped at 100: a container using four cores on an eight-core host is a fact worth seeing."},
			"memory_bytes":       openapi.Integer("Usage minus reclaimable page cache — what `docker stats` shows."),
			"memory_limit_bytes": openapi.Integer("The ceiling at the time of this sample."),
		}, "at", "cpu_percent", "memory_bytes", "memory_limit_bytes"),
	}
}

// Description is the shared prose for an operation that serves a
// series, so both endpoints say the same true things about sampling.
const Description = "Samples are taken every 30 seconds from the container's own cgroup counters and kept for a day; there is no downsampling behind that, so a day is what there is.\n\nCPU is a percentage of **one core** — 250 means two and a half cores — because rescaling it to a share of the machine hides how much work something is doing behind how large the host is. The first sample after a daemon restart or a redeploy reports 0: a percentage is a difference, and there is nothing yet to subtract from."
