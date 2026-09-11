package node

import "cubeship/internal/platform/openapi"

func (h *Handler) OpenAPI() openapi.Spec {
	nameParam := []openapi.Parameter{
		openapi.PathParam("name", "The server's name, unique across the cluster."),
	}

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Servers",
			Description: "The machines this instance is made of: the control plane — the box Cubeship was installed on, which holds the database, the dashboard, the registry and the builder — and any number of workers.\n\nA worker runs the same daemon in a mode where it decides nothing. **It dials the control plane; nothing dials it.** So a worker publishes nothing to the internet, and adding one is two steps that do not touch each other: this API mints a credential, and somebody runs the installer on the machine with it.\n\nThe private network the machines share is the part that does need them to reach **each other**, over the three cluster ports this instance opens between them — so a machine behind NAT can report in and be told what to run, and cannot be on that network.\n\nThe agent's own endpoint is not documented here. It is machinery between two daemons, like the registry's webhook, and nothing else is meant to call it.",
		}},
		Schemas: map[string]*openapi.Schema{
			"Mesh": openapi.Object(map[string]*openapi.Schema{
				"network":   openapi.String("The overlay's name. Absent on an instance with no cluster."),
				"encrypted": openapi.Bool("Whether what crosses between machines is carried over IPsec. Fixed when the network is created; Docker offers no way to change it after."),
			}, "encrypted"),
			"Server": openapi.Object(map[string]*openapi.Schema{
				"name":          openapi.String("Unique across the cluster and permanent, like every other name here."),
				"description":   openapi.String("What this machine is for."),
				"control_plane": openapi.Bool("Whether this row is the instance itself. There is exactly one, it is seeded, and it cannot be removed."),
				"status":        {Type: "string", Enum: []string{"ready", "pending", "unreachable"}, Description: "Derived from when the agent last called, never stored — a status in a column is one something has to run on a timer to keep true. `pending` has never connected and is waiting for the installer to be run on it; `unreachable` connected before and has stopped, which says nothing about why."},
				"address":       openapi.String("Where the machine is reached from outside, as the machine itself reported it. Absent when it could not work its own out — never a private address, the same rule the instance's own public IP follows."),
				"version":       openapi.String("The daemon the agent is running, which is what says whether a node is behind after an upgrade."),

				"cores":              openapi.Integer("How many the machine has."),
				"memory_total_bytes": openapi.Integer("What the machine has."),
				"disk_total_bytes":   openapi.Integer("The filesystem the instance's data directory is on."),
				"cpu_percent":        openapi.Number("The newest reading, as percent of the **whole machine**. Absent until one has been taken, because zero is a reading."),
				"memory_bytes":       openapi.Integer("The newest reading of what is spoken for."),
				"disk_bytes":         openapi.Integer("The newest reading of what is used."),
				"in_mesh":            openapi.Bool("Whether this machine is on the cluster's private network — Docker's own overlay, which is what lets a container here reach, and resolve by name, a container on another machine. A server can be `ready` and not on it: it is calling in, and its containers are alone."),

				"last_seen_at": {Type: "string", Format: "date-time", Description: "When the agent last called. Absent on a machine that has never connected."},
				"created_at":   {Type: "string", Format: "date-time"},
			}, "name", "control_plane", "status", "cores", "memory_total_bytes", "disk_total_bytes",
				"in_mesh", "created_at"),

			"ServerCreated": openapi.Object(map[string]*openapi.Schema{
				"token": openapi.String("What the machine's agent authenticates with. **Shown once and never again** — only its hash is stored, like an API key's. Losing it means removing the server and adding it back."),
			}, "token"),
		},

		Paths: map[string]openapi.PathItem{
			"/nodes": {
				"get": {
					OperationID: "listServers",
					Summary:     "List the machines in this cluster",
					Description: "The control plane first, then the workers by name, each with what it last reported about itself.",
					Tags:        []string{"Servers"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The cluster.", openapi.Array(openapi.Ref("Server"))),
						"401": openapi.Unauthorized,
					},
				},
				"post": {
					OperationID: "addServer",
					Summary:     "Add a machine to this cluster",
					Description: "Creates the row and mints the credential its agent will authenticate with. **The machine is not contacted**: it does not exist yet as far as this instance is concerned, and it stays `pending` until somebody runs the installer on it with this credential.\n\nWhat this *does* do on the way is bring up the cluster's private network on this machine, if it is not up already — a Docker swarm and an overlay network, which is what the workers will join. That is done here rather than when the machine first calls because this is the moment somebody is watching: an instance with no public address to advertise cannot build a cluster, and being told so now beats a server that says `ready` and can reach nothing.\n\nThe token is in this answer and in no other. Requires the admin role.",
					Tags:        []string{"Servers"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"name":        openapi.String("Lowercase letters, digits and dashes. Permanent."),
						"description": openapi.String("What this machine is for."),
					}, "name")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The server, and the credential its agent uses.", openapi.Ref("ServerCreated")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"409": openapi.TextResponse("That name is taken, or this instance has no public address to build a cluster on."),
					},
				},
			},
			"/nodes/mesh": {
				"get": {
					OperationID: "getMesh",
					Summary:     "The private network between the machines",
					Description: "Docker's own overlay: what every container that has to be reachable from another machine is attached to, and what makes a container name mean the same thing on every box.\n\n`encrypted` says whether what crosses between machines is carried over IPsec, and it is worth reading rather than assuming. **All app traffic crosses this network**: every name arrives at the control plane and is proxied to a container that may be elsewhere, and TLS ends at the proxy — so what goes over the wire is plain HTTP with its Authorization headers and session cookies, plus every connection an app makes to a database on another machine.\n\nDocker fixes the flag when the network is created and offers no way to change it. An instance whose cluster came up before this asked for encryption keeps an unencrypted network until that network is removed, which takes every container off the cluster's network until each is created again.\n\nAbsent `network` is an instance with no cluster: one that never added a second machine.",
					Tags:        []string{"Servers"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The cluster's network.", openapi.Ref("Mesh")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
			},
			"/nodes/{name}": {
				"get": {
					OperationID: "getServer",
					Summary:     "Get one machine",
					Tags:        []string{"Servers"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The server.", openapi.Ref("Server")),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
				"delete": {
					OperationID: "removeServer",
					Summary:     "Remove a machine from this cluster",
					Description: "**A local act.** The row goes and its credential with it, so the agent's next call is refused — but nothing reaches out to that box, and whatever is running there goes on running until somebody stops it. That is the honest shape for a machine that may be off or unreachable: the alternative is a delete that hangs on a host nobody can dial.\n\nThe control plane cannot be removed. Requires the admin role.",
					Tags:        []string{"Servers"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The server that was removed.", openapi.Ref("Server")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("That is this machine, not a server it manages."),
					},
				},
			},
		},
	}
}
