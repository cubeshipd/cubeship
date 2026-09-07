package machine

import (
	"cubeship/internal/metrics"
	"cubeship/internal/platform/openapi"
)

func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Instance",
			Description: "What the machine this instance runs on is doing: how much of its CPU is busy, how much of its memory is spoken for, how full the disk everything is kept on is, and how fast bytes are moving over its own network interfaces.\n\nRead only. The numbers are the kernel's; Cubeship samples them on the same timer it samples containers on and keeps a day of them.",
		}},
		Schemas: map[string]*openapi.Schema{
			"InstanceSample": openapi.Object(map[string]*openapi.Schema{
				"at":                 {Type: "string", Format: "date-time"},
				"cpu_percent":        openapi.Number("Percent of the **whole machine**: 100 is every core busy. The opposite convention from an app's or a database's series, where 100 is one core — there the question is how much of the host one container is taking, and here it is how much of the host is left."),
				"memory_bytes":       openapi.Integer("Total minus what the kernel reports available, which is what is actually spoken for. Not total minus free: that counts the page cache as used and makes every machine that has read a file look full."),
				"memory_total_bytes": openapi.Integer("What the machine has."),
				"disk_bytes":         openapi.Integer("Used on the filesystem the data directory is on, counted the way df counts — including the blocks reserved for root, which are not free for an image pull."),
				"disk_total_bytes":   openapi.Integer("The size of that filesystem."),
				"rx_bytes_per_sec":   openapi.Number("Bytes a second in over the machine's own interfaces, averaged across the interval. Absent when this daemon cannot see them — see `unavailable`."),
				"tx_bytes_per_sec":   openapi.Number("Bytes a second out, the same way."),
			}, "at", "cpu_percent", "memory_bytes", "memory_total_bytes", "disk_bytes", "disk_total_bytes"),

			"ContainerUsage": openapi.Object(map[string]*openapi.Schema{
				"kind":               {Type: "string", Enum: []string{"app", "datastore", "objectstore"}, Description: "What the container is running, which is also which listing names it."},
				"name":               openapi.String("What to call it: an app's full `project/environment/name` reference, a database's or a store's name. A bare app name would identify nothing — it is unique inside one environment and nowhere else."),
				"at":                 {Type: "string", Format: "date-time", Description: "When this reading was taken. At most two sampling intervals old, or the container is not reported at all."},
				"cpu_percent":        openapi.Number("Percent of **one core**, the container convention: 250 is two and a half cores. Not the machine's, which is what /instance/metrics reports."),
				"memory_bytes":       openapi.Integer("Usage minus reclaimable page cache, which is what `docker stats` shows."),
				"memory_limit_bytes": openapi.Integer("The cgroup's ceiling, or the machine's memory for a container with no limit of its own."),
			}, "kind", "name", "at", "cpu_percent", "memory_bytes", "memory_limit_bytes"),

			"InstanceSeries": openapi.Object(map[string]*openapi.Schema{
				"window":             openapi.String("The window these samples cover."),
				"samples":            openapi.Array(openapi.Ref("InstanceSample")),
				"cores":              openapi.Integer("How many the machine has, which is what says whether 60% is a busy box or a quiet one."),
				"memory_total_bytes": openapi.Integer("Reported here as well as on each sample, because it is a fact about the machine rather than about the series — a daemon that started a minute ago has no samples and still knows it."),
				"disk_total_bytes":   openapi.Integer("The same, for the disk."),
				"disk_path":          openapi.String("Which filesystem the disk numbers are about. Said rather than assumed: a box with the data directory on its own volume is a normal box, and \"the disk\" would be a lie on it."),
				"interfaces":         openapi.Array(openapi.String("An interface the network figures add up.")),
				"unavailable":        openapi.StringMap("What this daemon cannot measure, keyed by `cpu`, `memory`, `disk` or `network`, with one sentence saying why. Absent when it can measure everything.\n\nThe usual entry is `network`: /proc/net inside a container is that container's own network namespace, so reading it would report the daemon's own veth and call it the instance's traffic. The daemon's container is given the machine's /proc for exactly this, and one started without it says so here rather than answering with a number that is wrong and looks right."),
			}, "window", "samples", "cores", "memory_total_bytes", "disk_total_bytes", "disk_path"),
		},

		Paths: map[string]openapi.PathItem{
			"/instance/containers": {
				"get": {
					OperationID: "listInstanceContainers",
					Summary:     "What every container on this instance is using",
					Description: "One reading per container — the newest, at most two sampling intervals old — heaviest CPU first. It crosses every module that runs one: apps, databases and managed object stores.\n\nA container that has gone is not in the answer, and one that has just started is not in it either until it has been sampled. **The CPU convention here is the container one**: 100 is one core, not one machine.",
					Tags:        []string{"Instance"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("What is running, heaviest first.", openapi.Array(openapi.Ref("ContainerUsage"))),
						"401": openapi.Unauthorized,
					},
				},
			},

			"/instance/metrics": {
				"get": {
					OperationID: "getInstanceMetrics",
					Summary:     "Read the machine's CPU, memory, disk and network",
					Description: "What the box itself has been doing, rather than one container on it.\n\n" + metrics.Description + "\n\nA measurement this daemon cannot take is reported in `unavailable` inside a normal answer, not as an error: the three that read are still worth having.",
					Tags:        []string{"Instance"},
					Parameters:  []openapi.Parameter{metrics.WindowParam()},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The series, and the facts about the machine it is drawn against.", openapi.Ref("InstanceSeries")),
						"400": openapi.TextResponse("No such window."),
						"401": openapi.Unauthorized,
					},
				},
			},
		},
	}
}
