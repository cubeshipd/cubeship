package datastore

import (
	"cubeship/internal/platform/openapi"
)

func (h *Handler) OpenAPI() openapi.Spec {
	refParams := []openapi.Parameter{
		openapi.PathParam("project", "The project's slug."),
		openapi.PathParam("env", "The environment's slug."),
		openapi.PathParam("name", "The datastore's name, unique within that environment."),
	}
	attachParams := append(append([]openapi.Parameter{}, refParams...),
		openapi.PathParam("app", "The app's name within the same environment."))

	const exposeWarning = "\n\n**There is no TLS in front of this.** Traefik terminates HTTPS for HTTP; a database speaks its own protocol on its own port, so an exposed datastore is a password on the open internet. What makes that safe is the password — generated, 24 characters — and a firewall rule, which is yours to write. Leave it off for anything an app on this instance can reach through its container name."

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Datastores",
			Description: "A managed database, running in an environment beside the apps that use it. Cubeship provisions the container, keeps its data in a directory on the host, and hands the connection string to whichever apps are attached to it.",
		}},
		Schemas: map[string]*openapi.Schema{
			"Datastore": openapi.Object(map[string]*openapi.Schema{
				"reference":     openapi.String("The datastore's identifier, project/environment/name."),
				"name":          openapi.String("Unique within its environment, not across the instance. Permanent."),
				"description":   openapi.String("What this database is for. Empty unless someone set it."),
				"project":       openapi.String(""),
				"environment":   openapi.String(""),
				"engine":        {Type: "string", Enum: []string{"postgres", "mysql", "mariadb"}, Description: "Which server this runs. Permanent."},
				"version":       openapi.String("The engine's version. Permanent: a data directory written by one major version is not readable by another, so changing this would be a container that will not start with the only copy of the data inside it."),
				"status":        {Type: "string", Enum: []string{"provisioning", "running", "down", "failed"}, Description: `"provisioning" while the container is being pulled and started, which happens detached from the request that asked for it.`},
				"error":         openapi.String("Why provisioning failed, when it did — usually the tail of what the engine printed before it exited."),
				"username":      openapi.String("The login the engine was initialized with. Its password is not reported here; see the credentials endpoint."),
				"database":      openapi.String("The database created inside the server. Absent for an engine with no such concept."),
				"host":          openapi.String("Where an app on this instance reaches it: the container's own name on the shared Docker network. Attached apps already receive this as a variable."),
				"port":          openapi.Integer("What the engine listens on inside the network — 5432 for Postgres, 3306 for MySQL and MariaDB."),
				"exposed_port":  openapi.Integer("The host port it also answers on from outside this instance. Absent when it does not, which is the default."),
				"external_host": openapi.String("The instance's own domain, which is where an exposed datastore is reached. Absent while there is no domain, or while it is not exposed."),
				"attachments":   openapi.Array(openapi.Ref("DatastoreAttachment")),
				"created_at":    {Type: "string", Format: "date-time"},
			}, "reference", "name", "description", "project", "environment", "engine", "version",
				"status", "username", "host", "port", "attachments", "created_at"),

			"DatastoreAttachment": openapi.Object(map[string]*openapi.Schema{
				"app":       openapi.String("The app's name within the environment."),
				"prefix":    openapi.String(`What the injected variables are named under. Absent for the usual case, which gives DATABASE_URL.`),
				"variables": openapi.Array(openapi.String("A variable name this app's container receives.")),
			}, "app", "variables"),

			"DatastoreCredentials": openapi.Object(map[string]*openapi.Schema{
				"username":      openapi.String(""),
				"password":      openapi.String("Stored as given, because a hash cannot connect to anything. It is only ever returned here."),
				"database":      openapi.String(""),
				"internal_uri":  openapi.String("What an app on this instance connects with. The same string an attached app receives as DATABASE_URL."),
				"internal_host": openapi.String("The container's name on the shared Docker network."),
				"internal_port": openapi.Integer(""),
				"external_uri":  openapi.String("The same from off this host. Present only while the datastore is exposed and the instance has a domain."),
				"external_host": openapi.String(""),
				"external_port": openapi.Integer(""),
			}, "username", "password", "internal_uri", "internal_host", "internal_port"),

			"DatastoreEngine": openapi.Object(map[string]*openapi.Schema{
				"engine":          openapi.String(""),
				"versions":        openapi.Array(openapi.String("A version tag this release offers, newest first.")),
				"default_version": openapi.String("What a datastore created without naming one runs."),
				"port":            openapi.Integer("What this engine listens on."),
				"has_database":    openapi.Bool("Whether naming a database inside the server means anything for this engine."),
			}, "engine", "versions", "default_version", "port", "has_database"),
		},
		Paths: map[string]openapi.PathItem{
			"/datastores": {
				"post": {
					OperationID: "createDatastore",
					Summary:     "Provision a database",
					Description: "Creates the row and starts the container detached, so this returns while the image is still being pulled: the datastore comes back in `provisioning`, and how it went lands on the same row.\n\nThe password is generated when the request does not carry one, so a database with no password is not something anyone can create by leaving a field empty. It is returned here once, and afterwards only from the credentials endpoint.\n\nRequires the admin role: this starts a container and claims disk on the host that nothing reclaims on its own.",
					Tags:        []string{"Datastores"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"name":        openapi.String("Lowercase letters, digits and dashes. It becomes a component of the container name attached apps resolve, so it is permanent."),
						"description": openapi.String("What this database is for. Optional."),
						"project":     openapi.String("Project slug."),
						"environment": openapi.String(`Environment slug. Defaults to "production".`),
						"engine":      {Type: "string", Enum: []string{"postgres", "mysql", "mariadb"}, Description: "Which server to run. GET /datastores/engines lists what this release offers."},
						"version":     openapi.String("A version this release offers for that engine. Defaults to the newest. Permanent."),
						"username":    openapi.String(`The login to create. Defaults to "cubeship". MySQL and MariaDB refuse "root" — it already exists and Cubeship does not hold its password.`),
						"password":    openapi.String("Generated when omitted. Any characters: it is escaped into the connection URL rather than concatenated into it."),
						"database":    openapi.String("The database to create inside the server. Defaults to the name with dashes turned into underscores. Ignored by an engine that has no named databases."),
						"expose":      openapi.Integer("Publish on a host port at creation: 0 picks one from 15000-15999, or name one. Omit for internal-only, which is the normal answer." + exposeWarning),
					}, "name", "project", "engine")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The provisioning datastore, with the password it was created with.", openapi.Ref("Datastore")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("The project or environment does not exist."),
						"409": openapi.TextResponse("A datastore with that name already exists in the environment, or the host port asked for is already taken."),
					},
				},
				"get": {
					OperationID: "listDatastores",
					Summary:     "List databases",
					Description: "Every managed database on this instance, with the apps attached to each. Passwords are not included.",
					Tags:        []string{"Datastores"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The instance's datastores.", openapi.Array(openapi.Ref("Datastore"))),
						"401": openapi.Unauthorized,
					},
				},
			},
			"/datastores/engines": {
				"get": {
					OperationID: "listDatastoreEngines",
					Summary:     "List the engines this release runs",
					Description: "What may be passed as `engine` and `version` when creating a datastore. Read it rather than hard-coding a list: the versions offered are pinned by the release, because a version is permanent once a datastore runs it.",
					Tags:        []string{"Datastores"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The engines and their versions.", openapi.Array(openapi.Ref("DatastoreEngine"))),
						"401": openapi.Unauthorized,
					},
				},
			},
			datastorePath: {
				"get": {
					OperationID: "getDatastore",
					Summary:     "Get a database",
					Description: "One datastore: its engine, status, the address apps reach it at, and what each attached app receives. Not its password.",
					Tags:        []string{"Datastores"},
					Parameters:  refParams,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The datastore.", openapi.Ref("Datastore")),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
				"patch": {
					OperationID: "updateDatastore",
					Summary:     "Change a database's description",
					Description: "The description is the only editable field, and the rest is not an oversight.\n\nThe name is a component of the container name every attached app resolves. The engine and the version are what wrote the data directory — a major version changed under an existing one is a container that will not start, with the only copy of the data inside the directory it will not read. The password is used once, when the engine initializes itself: changing this column would change every connection string Cubeship hands out while the database went on accepting only the old one.\n\nRunning a different version means creating a second datastore and moving the data with the engine's own tools.",
					Tags:        []string{"Datastores"},
					Parameters:  refParams,
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"description": openapi.String("May be empty."),
					})),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The updated datastore.", openapi.Ref("Datastore")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"delete": {
					OperationID: "deleteDatastore",
					Summary:     "Delete a database",
					Description: "Stops and removes the container, **and removes its data directory from the host**. There is no backup and this cannot be undone.\n\nThe data goes because a managed database whose files outlived it would leave a directory nothing on the instance names any more and nothing ever reclaims — and it is the largest thing on the disk. Deleting a project or an environment does the same to every datastore inside it.",
					Tags:        []string{"Datastores"},
					Parameters:  refParams,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The deleted datastore, as it last was.", openapi.Ref("Datastore")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
			datastorePath + "/credentials": {
				"get": {
					OperationID: "getDatastoreCredentials",
					Summary:     "Read a database's login",
					Description: "The username, the password and the connection strings to use them with — from inside the instance, and from outside it when the datastore is exposed.\n\nIts own request rather than a field on the datastore: everything else is worth listing on a screen, and a credential is worth asking for on purpose. Requires the admin role.",
					Tags:        []string{"Datastores"},
					Parameters:  refParams,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The login and where to use it.", openapi.Ref("DatastoreCredentials")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
			datastorePath + "/expose": {
				"post": {
					OperationID: "exposeDatastore",
					Summary:     "Publish a database on a host port",
					Description: "So that something which is not an app on this instance can connect: a migration run from a laptop, psql, a BI tool." + exposeWarning + "\n\nThe container is replaced to pick the port up, because a container's published ports are fixed when it is created — the data is a host bind mount and survives that untouched. The datastore goes back to `provisioning` while it happens.",
					Tags:        []string{"Datastores"},
					Parameters:  refParams,
					RequestBody: &openapi.RequestBody{
						Description: "Which port to publish on. An empty body picks one.",
						Content: openapi.JSON(openapi.Object(map[string]*openapi.Schema{
							"port": openapi.Integer("A port between 1024 and 65535, or 0 (the default) to take the next free one from 15000-15999. A port held by something Cubeship did not start fails when the container binds it — no list here could have known about it."),
						})),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The datastore, now provisioning on its new port.", openapi.Ref("Datastore")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("Another datastore already answers on that port, or the automatic range is exhausted."),
					},
				},
				"delete": {
					OperationID: "unexposeDatastore",
					Summary:     "Take a database off its host port",
					Description: "It stays reachable by the apps on this instance, which never used the published port. The container is replaced, so the datastore goes back to `provisioning` briefly.",
					Tags:        []string{"Datastores"},
					Parameters:  refParams,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The datastore, no longer published.", openapi.Ref("Datastore")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
			datastorePath + "/attachments": {
				"post": {
					OperationID: "attachDatastore",
					Summary:     "Wire an app to a database",
					Description: "The app's container is given `DATABASE_URL` and its parts — host, port, name, user, password — **from its next deploy onwards**. A container keeps the environment it was created with, the same rule that makes adding a domain take effect on redeploy.\n\nThe variables sit between the environment's and the app's own in the layering, so an app can override one without detaching anything.\n\nThe app must be in the same environment as the datastore. Requires the admin role: attaching hands the database's password to an app.",
					Tags:        []string{"Datastores"},
					Parameters:  refParams,
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"app":    openapi.String("The app's name within the same environment — not its full reference."),
						"prefix": openapi.String(`What the variables are named under, e.g. "ANALYTICS_" for ANALYTICS_DATABASE_URL. Empty is the usual answer. An app that needs two databases names one of them: two under the same prefix would be one variable with two values.`),
					}, "app")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The datastore, with the new attachment.", openapi.Ref("Datastore")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such datastore, or no such app in its environment."),
						"409": openapi.TextResponse("That app is already attached, or already has a datastore under that prefix."),
					},
				},
			},
			datastorePath + "/attachments/{app}": {
				"delete": {
					OperationID: "detachDatastore",
					Summary:     "Unwire an app from a database",
					Description: "The app's container keeps the variables it was created with until it is deployed again, so this is not how you cut an app off from a database in a hurry.",
					Tags:        []string{"Datastores"},
					Parameters:  attachParams,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The datastore, without that attachment.", openapi.Ref("Datastore")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such datastore or app, or that app was not attached."),
					},
				},
			},
		},
	}
}
