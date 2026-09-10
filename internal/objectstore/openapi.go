package objectstore

import (
	"maps"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/openapi"
)

// withMetrics folds in the components every metrics endpoint shares, so
// the series has one shape wherever it is served from.
func withMetrics(own map[string]*openapi.Schema) map[string]*openapi.Schema {
	maps.Copy(own, metrics.Schemas())
	return own
}

// The exposure warning, said in one place because it is said in three.
const exposeWarning = "\n\n**There is no TLS in front of this.** Traefik terminates HTTPS for the things it routes by hostname; a managed store is published as a plain port, so an exposed one answers HTTP on the open internet. The signature protects the keys and nothing protects what is being transferred. What makes it safe is a firewall rule, which is yours to write — this instance has a screen for it."

func (h *Handler) OpenAPI() openapi.Spec {
	nameParam := []openapi.Parameter{
		openapi.PathParam("name", "The store's name, unique across the instance."),
	}
	bucketParams := []openapi.Parameter{
		openapi.PathParam("name", "The store's name."),
		openapi.PathParam("bucket", "The bucket."),
	}
	browseParams := append(append([]openapi.Parameter{}, bucketParams...),
		openapi.QueryParam("prefix", "The folder to list, e.g. `backups/2026/`. Omit for the root of the bucket. A folder is a common prefix — S3 has no directories — so this is a string match, not a path lookup."),
		openapi.QueryParam("cursor", "Continue a listing that came back with one."),
		openapi.QueryParam("limit", "How many entries to return. Defaults to 200 and is capped at 1000."))
	attachParams := []openapi.Parameter{
		openapi.PathParam("name", "The store's name."),
		openapi.PathParam("project", "The attached app's project slug."),
		openapi.PathParam("env", "The attached app's environment slug."),
		openapi.PathParam("app", "The attached app's own name."),
		openapi.QueryParam("bucket", "Which bucket to unwire it from. Required: one app may be attached to two buckets in the same store, so the app's reference alone does not identify an attachment."),
	}
	uploadParams := append(append([]openapi.Parameter{}, bucketParams...),
		openapi.QueryParam("prefix", "The folder to write into. Omit for the root of the bucket."),
		openapi.QueryParam("filename", "What to call the object inside that folder. It may not start with a slash or contain a `..` segment."))

	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Object storage",
			Description: "Buckets and the files in them, from two places at once: a MinIO this instance runs, and an S3 endpoint somewhere else it holds the keys to. Both are the same resource here — an endpoint, a login and buckets inside it — and every call below works the same way on either.\n\nThe convention for object storage is backups, and a backup of this machine kept on this machine is not one, which is why linking somewhere else exists. Running one here is for the other half: an app's uploads, a dump on its way out, developing against S3 without paying for S3.",
		}},
		Schemas: withMetrics(map[string]*openapi.Schema{
			"ObjectStoreLimits": openapi.Object(map[string]*openapi.Schema{
				"cpu":          {Type: "number", Description: "Cores a managed store's container may use, fractional allowed. A ceiling rather than a share. Zero is no limit, which is the default, and always zero on a linked store."},
				"memory_bytes": openapi.Integer("A hard memory ceiling in bytes for a managed store's container. The kernel enforces it by killing whatever crosses it. Zero is no limit, which is the default, and always zero on a linked store."),
			}, "cpu", "memory_bytes"),
			"ObjectStore": openapi.Object(map[string]*openapi.Schema{
				"name":              openapi.String("Unique across the instance. For a managed store it is the container's own name, which is the host apps connect to, so it is permanent either way."),
				"description":       openapi.String("What this storage is for. With nothing above a store to say where it belongs, this is the only place that can."),
				"kind":              {Type: "string", Enum: []string{"managed", "external"}, Description: `"managed" is a MinIO this instance runs; "external" is an endpoint somewhere else that it holds keys for. Deleting the first removes the data; deleting the second touches nothing anywhere else.`},
				"provider":          {Type: "string", Enum: []string{"minio", "aws", "cloudflare", "digitalocean", "generic"}, Description: "Which S3 this is. It decides how the endpoint is spelled and whether the bucket goes in the hostname or the path."},
				"provider_label":    openapi.String("The provider's name as a person writes it, so every surface spells it the same way."),
				"endpoint":          openapi.String("Where this answers, as a client dials it. For a managed store it is its own container's name on the shared Docker network — which is exactly why it is reachable from an app on this instance and from nowhere else."),
				"region":            openapi.String("What a request's signature is computed for. Every S3 signature carries one whether the provider has regions or not."),
				"path_style":        openapi.Bool("Whether the bucket goes in the path rather than in the hostname. Half the S3 clients need telling and the other half guess wrong, so it is reported."),
				"bucket":            openapi.String("The one bucket this store is pinned to, for a login that reaches exactly one and cannot list them. Absent for a store that lists its own."),
				"credential_id":     openapi.Integer("The stored account an external store authenticates as. Absent on a managed one, whose keys are its own."),
				"version":           openapi.String("The MinIO release a managed store runs. Permanent: a data directory belongs to the server that wrote it."),
				"exposed_port":      openapi.Integer("The host port a managed store also answers on from outside this instance. Absent when it does not, which is the default." + exposeWarning),
				"limits":            openapi.Ref("ObjectStoreLimits"),
				"external_endpoint": openapi.String("Where something off this host reaches it. Present only while it is exposed and the instance has a domain to be reached at."),
				"attachments":       openapi.Array(openapi.Ref("ObjectStoreAttachment")),
				"has_container":     openapi.Bool("Whether a container currently backs this, which is what decides whether there is a log to read or anything to stop. The status alone cannot answer it: one whose provisioning failed may have neither."),
				"status":            {Type: "string", Enum: []string{"provisioning", "running", "stopped", "down", "failed", "linked"}, Description: `The first five are a managed store's. "linked" is an external one's, and it is not a health check: this instance holds a login, and whether the endpoint answers is found out by opening it.`},
				"error":             openapi.String("Why provisioning failed, when it did — usually the tail of what the server printed before it exited."),
				"created_at":        {Type: "string", Format: "date-time"},
				"updated_at":        {Type: "string", Format: "date-time"},
			}, "name", "kind", "provider", "provider_label", "endpoint", "region",
				"path_style", "attachments", "has_container", "status", "created_at", "updated_at"),

			"ObjectStoreAttachment": openapi.Object(map[string]*openapi.Schema{
				"app":       openapi.String("The app's full reference, `project/environment/name`. Full, because a store is not inside an environment and one may serve apps in several."),
				"bucket":    openapi.String("Which bucket in this store the app is pointed at. It is on the attachment rather than on the store because a store holds many and an app wants one."),
				"prefix":    openapi.String("What the injected variables are named under. Absent for the usual case, which gives `S3_ENDPOINT` and its parts."),
				"variables": openapi.Array(openapi.String("A variable name this app's container receives. The values are not reported: one of them is the secret key.")),
			}, "app", "bucket", "variables"),

			"ObjectStoreCredentials": openapi.Object(map[string]*openapi.Schema{
				"access_key":        openapi.String("The access key id. Generated for a managed store; the stored account's username for a linked one."),
				"secret_key":        openapi.String("The secret. Stored as given, because a hash cannot sign a request, and only ever returned here."),
				"region":            openapi.String(""),
				"endpoint":          openapi.String("Where an app on this instance connects."),
				"external_endpoint": openapi.String("The same from off this host. Present only while a managed store is exposed and the instance has a domain."),
				"path_style":        openapi.Bool("Whether the client must be told to put the bucket in the path."),
			}, "access_key", "secret_key", "region", "endpoint", "path_style"),

			"ObjectStoreProviders": openapi.Object(map[string]*openapi.Schema{
				"providers": openapi.Array(openapi.Ref("ObjectStoreProvider")),
				"versions":  openapi.Array(openapi.String("A MinIO release this Cubeship offers, newest first.")),
			}, "providers", "versions"),

			"ObjectStoreProvider": openapi.Object(map[string]*openapi.Schema{
				"provider": openapi.String(""),
				"label":    openapi.String("The provider's name as a person writes it."),
				"asks":     {Type: "string", Enum: []string{"region", "account", "endpoint"}, Description: "The one field this provider needs beyond the login, because its endpoint is a template with one variable in it. `endpoint` means the whole address is typed."},
			}, "provider", "label", "asks"),

			"Bucket": openapi.Object(map[string]*openapi.Schema{
				"name":       openapi.String(""),
				"created_at": {Type: "string", Format: "date-time", Description: "Absent for a store pinned to one bucket, which is reported without asking the endpoint anything."},
			}, "name"),

			"ObjectListing": openapi.Object(map[string]*openapi.Schema{
				"prefix":  openapi.String("The folder that was listed."),
				"folders": openapi.Array(openapi.Ref("ObjectFolder")),
				"objects": openapi.Array(openapi.Ref("StoredObject")),
				"cursor":  openapi.String("Pass back as `cursor` for the next page. Absent when there is no more."),
			}, "prefix", "folders", "objects"),

			"ObjectFolder": openapi.Object(map[string]*openapi.Schema{
				"prefix": openapi.String("The whole prefix, from the root of the bucket — what to pass back as `prefix` to go into it."),
				"name":   openapi.String("Its last segment, which is what it is called inside the folder it appears in."),
			}, "prefix", "name"),

			"StoredObject": openapi.Object(map[string]*openapi.Schema{
				"key":         openapi.String("The whole key, from the root of the bucket. This is what every other call takes."),
				"name":        openapi.String("Its last segment."),
				"size":        openapi.Integer("Bytes."),
				"modified_at": {Type: "string", Format: "date-time"},
				"etag":        openapi.String("What the store calls this version of the object. For a single-part upload it is the MD5 of the content; for a multipart one it is not, which is why it is reported rather than described as a checksum."),
			}, "key", "name", "size", "modified_at"),
		}),

		Paths: map[string]openapi.PathItem{
			"/objectstores": {
				"get": {
					OperationID: "listObjectStores",
					Summary:     "List object storage",
					Description: "Every store this instance can reach, the ones it runs and the ones it holds keys for. No keys are included.",
					Tags:        []string{"Object storage"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The instance's object stores.", openapi.Array(openapi.Ref("ObjectStore"))),
						"401": openapi.Unauthorized,
					},
				},
				"post": {
					OperationID: "createObjectStore",
					Summary:     "Run a MinIO here, or link one elsewhere",
					Description: "One endpoint for both, because a store is one resource: what comes back is the same row and every call after this treats the two alike. `kind` decides which half of the body is read.\n\n**managed** provisions a MinIO on this instance. The row is written and the container started detached, so this returns while the image is still being pulled: the store comes back in `provisioning`, and how it went lands on the same row. The keys are generated when the request does not carry them, and are returned only from the credentials endpoint.\n\n**external** records an endpoint somewhere else. Nothing is checked against it here — the credential may be scoped to one bucket, and the endpoint may be behind a network this daemon reaches later — so whether the login works is answered the first time somebody opens it, where the provider's own words can be shown.\n\nRequires the admin role.",
					Tags:        []string{"Object storage"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"kind":        {Type: "string", Enum: []string{"managed", "external"}, Description: "Which of the two this is."},
						"name":        openapi.String("Lowercase letters, digits and dashes, unique across the instance. For a managed store it becomes the container's name, so it is permanent. `providers` is refused — it is where this API lists what it can link."),
						"description": openapi.String("What this storage is for. Optional."),

						"version":    openapi.String("managed: a MinIO release this Cubeship offers. Defaults to the newest, and is permanent."),
						"access_key": openapi.String("managed: the root access key. Generated when omitted, which is the normal answer. At least 3 characters, or the server refuses to start."),
						"secret_key": openapi.String("managed: the root secret. Generated when omitted. At least 8 characters."),
						"expose":     openapi.Integer("managed: publish on a host port at creation — 0 picks one from 16000-16999, or name one. Omit for internal-only, which is the normal answer." + exposeWarning),

						"provider": {Type: "string", Enum: []string{"aws", "cloudflare", "digitalocean", "generic"}, Description: "external: which S3 this is. GET /objectstores/providers says what each one asks for. `minio` is not a choice — a MinIO somewhere else is `generic`, because from here that is all it is."},
						"region":   openapi.String("external: required for `aws` and `digitalocean`, whose endpoints are derived from it. Optional for `generic`."),
						"account":  openapi.String("external: required for `cloudflare` — R2's endpoint is named after the account id."),
						"endpoint": openapi.String("external: required for `generic`. A URL is accepted and taken apart, because that is what a provider's console shows: `https://s3.eu-central-1.wasabisys.com`."),
						"bucket":   openapi.String("external: pin this store to one bucket, for a login that reaches exactly one and cannot list them — an R2 token scoped to a bucket, an IAM policy without ListAllMyBuckets. Omit for the normal case."),

						"credential_id":  openapi.Integer("external: the stored account this store authenticates as, whose username is the access key id and whose secret is the secret key."),
						"new_access_key": openapi.String("external: an access key id typed here instead of choosing a stored account. A credential is a convenience, not a prerequisite — the account is created from these in the same transaction and turns up under credentials afterwards, ready to be picked for the next store. Refused together with `credential_id`."),
						"new_secret_key": openapi.String("external: the secret half of the above."),
						"label":          openapi.String("external: what to call the account created from a typed login. Derived from the endpoint when omitted."),
					}, "kind", "name")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The store. A managed one is still provisioning.", openapi.Ref("ObjectStore")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"409": openapi.TextResponse("A store with that name already exists, or the host port asked for is taken."),
					},
				},
			},

			"/objectstores/providers": {
				"get": {
					OperationID: "listObjectStoreProviders",
					Summary:     "List what can be linked and what can be run",
					Description: "The providers this release knows how to link, what each asks for beyond the login, and the MinIO releases it can run. Read it rather than hard-coding a list: a version is permanent once a store holds data, so the ones offered are pinned by the release.",
					Tags:        []string{"Object storage"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The providers and versions.", openapi.Ref("ObjectStoreProviders")),
						"401": openapi.Unauthorized,
					},
				},
			},

			"/objectstores/{name}": {
				"get": {
					OperationID: "getObjectStore",
					Summary:     "Get one object store",
					Tags:        []string{"Object storage"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The store.", openapi.Ref("ObjectStore")),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
					},
				},
				"patch": {
					OperationID: "updateObjectStore",
					Summary:     "Change a store's description, its limits, or which account it uses",
					Description: "Not the name, which is the container's for a managed store and the identity for both. Not the endpoint or the provider either: an app configured against this store would silently start reaching somewhere else. What can change is the account an external store authenticates as — a second key, a different tenancy — which is what a credential is for. A managed store has no account to re-point, and asking is refused rather than ignored.\n\nRequires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters:  nameParam,
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"description":   openapi.String("Omit to leave it alone."),
						"credential_id": openapi.Integer("The stored account this store authenticates as. External stores only."),
						"limits":        {Ref: "#/components/schemas/ObjectStoreLimits", Description: "How much of the machine a managed store's container may take.\n\n**Managed stores only.** A linked store runs on somebody else's server, so there is no container here to cap — asking is refused rather than stored and ignored, which would put a ceiling on a screen for a server this instance has no say over.\n\nIt takes effect at once and does not replace the container, the way publishing a port does. Removing one is the exception: the Engine reads a zero in an update as \"leave that one alone\", so it waits for the next provision.\n\nSend the whole object — a field left out of it is a zero, which is how a limit is removed."},
					})),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The store as it now stands.", openapi.Ref("ObjectStore")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("A managed store's connection is this instance's own, so there is nothing to re-point; or a limit was asked for on a linked store, which runs on somebody else's server."),
					},
				},
				"delete": {
					OperationID: "deleteObjectStore",
					Summary:     "Delete an object store",
					Description: "**The two kinds differ here, and the difference is the whole point.** Deleting a managed store stops its container and removes its objects from this host: that cannot be undone and there is no backup. Deleting a linked one forgets an endpoint and a login — the bucket and everything in it stay exactly where they are.\n\nRequires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"204": openapi.Empty("Deleted."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},

			"/objectstores/{name}/credentials": {
				"get": {
					OperationID: "getObjectStoreCredentials",
					Summary:     "Read a store's keys",
					Description: "Its own request rather than a field on the store: everything else about one is worth listing on a screen, and this is worth asking for. Requires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The keys, and where to use them.", openapi.Ref("ObjectStoreCredentials")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},

			"/objectstores/{name}/metrics": {
				"get": {
					OperationID: "getObjectStoreMetrics",
					Summary:     "Read a managed store's CPU and memory",
					Description: "What the container running this store has been using. Only for a store this instance runs — nothing here samples a server somewhere else.\n\n" + metrics.Description,
					Tags:        []string{"Object storage"},
					Parameters:  append(append([]openapi.Parameter{}, nameParam...), metrics.WindowParam()),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The series.", openapi.Ref("MetricSeries")),
						"400": openapi.TextResponse("No such window."),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("This store runs somewhere else."),
					},
				},
			},

			"/objectstores/{name}/logs": {
				"get": {
					OperationID: "objectStoreLogs",
					Summary:     "Read a managed store's log",
					Description: "What the server has written, most recent lines first to arrive. Only for a store this instance runs — there is no log here for one that runs somewhere else.",
					Tags:        []string{"Object storage"},
					Parameters: append(append([]openapi.Parameter{}, nameParam...),
						openapi.QueryParam("tail", "How many lines. Defaults to 500.")),
					Responses: openapi.Responses{
						"200": openapi.TextResponse("The log."),
						"401": openapi.Unauthorized,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("This store runs somewhere else, or has no container."),
					},
				},
			},

			"/objectstores/{name}/start": {
				"post": {
					OperationID: "startObjectStore",
					Summary:     "Start a managed store",
					Description: "Provisions again rather than starting the container that is there — one path instead of two, and the objects are a host bind mount a recreate does not touch. It is also how a store whose provisioning failed is retried. Requires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The store, provisioning.", openapi.Ref("ObjectStore")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("This store runs somewhere else."),
					},
				},
			},

			"/objectstores/{name}/stop": {
				"post": {
					OperationID: "stopObjectStore",
					Summary:     "Stop a managed store",
					Description: "The container is stopped rather than removed, so its log survives — what somebody wants immediately after turning storage off is usually the reason they turned it off. Every app writing to it starts failing. Requires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The stopped store.", openapi.Ref("ObjectStore")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("This store runs somewhere else, or has no container."),
					},
				},
			},

			"/objectstores/{name}/expose": {
				"post": {
					OperationID: "exposeObjectStore",
					Summary:     "Publish a managed store on a host port",
					Description: "So something that is not an app on this instance can reach it: `aws s3 cp` from a laptop, a backup script on another machine. The container is replaced to pick the port up, because published ports are fixed when a container is created; the objects survive, being a bind mount." + exposeWarning + "\n\nRequires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters:  nameParam,
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"port": openapi.Integer("0 picks one from 16000-16999. Naming one is for an operator whose firewall rule is already written."),
					})),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The store, provisioning onto its new port.", openapi.Ref("ObjectStore")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("That port is taken, or this store runs somewhere else."),
					},
				},
				"delete": {
					OperationID: "unexposeObjectStore",
					Summary:     "Take a managed store off its host port",
					Description: "It goes back to being reachable only by its neighbours on the shared network. Requires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The store.", openapi.Ref("ObjectStore")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("This store runs somewhere else."),
					},
				},
			},

			"/objectstores/{name}/attachments": {
				"post": {
					OperationID: "attachObjectStore",
					Summary:     "Wire an app to a bucket",
					Description: "The app's container is given `S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY` and `S3_PATH_STYLE` **from its next deploy onwards** — a container keeps the environment it was created with, the same rule that makes adding a domain take effect on redeploy.\n\nThere is no connection string, because no S3 client agrees on one. There are no `AWS_*` names either: they would make an app using the AWS SDK work with no configuration and would also mean two stores on one app fighting over six names the SDK reads and the prefix does not reach. Map them in the app's own environment when you want that.\n\nThe bucket is not checked against the store. Whether it exists is a live call this instance may not be allowed to make — a credential scoped to one bucket may not stat another — and refusing on evidence it does not have is how a working attachment gets blocked.\n\nRequires the admin role: this hands an app the store's keys.",
					Tags:        []string{"Object storage"},
					Parameters:  nameParam,
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"app":    openapi.String("The app's full reference, `project/environment/name` — or `project/name` for production."),
						"bucket": openapi.String("Which bucket in this store to point it at."),
						"prefix": openapi.String("What the variables are named under, e.g. `BACKUPS_`. Omit for the usual case. Needed when one app is attached to two buckets, since two attachments would otherwise name the same variables — a datastore's attachment cannot collide with one of these, because the two write different names."),
					}, "app", "bucket")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The store, with the attachment on it.", openapi.Ref("ObjectStore")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such store, or no such app."),
						"409": openapi.TextResponse("That app is already attached to that bucket, or it already receives those variable names from another attachment."),
					},
				},
			},

			"/objectstores/{name}/attachments/{project}/{env}/{app}": {
				"delete": {
					OperationID: "detachObjectStore",
					Summary:     "Unwire an app from a bucket",
					Description: "The app's container keeps the variables it was created with until it is deployed again, so this is not how you cut an app off from a bucket in a hurry — rotating the credential is.\n\nRequires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters:  attachParams,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The store, without the attachment.", openapi.Ref("ObjectStore")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such store, no such app, or that app is not attached to that bucket."),
					},
				},
			},

			"/objectstores/{name}/buckets": {
				"get": {
					OperationID: "listBuckets",
					Summary:     "List a store's buckets",
					Description: "A store pinned to one bucket answers with that one and asks the endpoint nothing — its login reaches a single bucket and listing them is exactly what it may not do.\n\n**Everything about a store's contents requires the admin role.** Cubeship lets a member read a great deal about what the instance is wired to, and never lets one read data — there is no way to see a row of a database from here either, and a bucket is data.",
					Tags:        []string{"Object storage"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The buckets.", openapi.Array(openapi.Ref("Bucket"))),
						"401": openapi.Unauthorized,
						"403": openapi.TextResponse("Either the caller lacks the admin role, or the store refused the login this instance holds. The body says which."),
						"404": openapi.NotFound,
						"502": openapi.TextResponse("The endpoint did not answer."),
					},
				},
				"post": {
					OperationID: "createBucket",
					Summary:     "Create a bucket",
					Tags:        []string{"Object storage"},
					Parameters:  nameParam,
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"name": openapi.String("3 to 63 characters of lowercase letters, digits, dots and dashes, starting and ending with a letter or digit. Checked here so the refusal is a sentence rather than an XML fault."),
					}, "name")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The bucket.", openapi.Ref("Bucket")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("A bucket with that name already exists."),
						"502": openapi.TextResponse("The endpoint did not answer."),
					},
				},
			},

			"/objectstores/{name}/buckets/{bucket}": {
				"delete": {
					OperationID: "deleteBucket",
					Summary:     "Delete an empty bucket",
					Description: "Empty, because S3 refuses otherwise and that refusal is worth keeping: a bucket delete that quietly took a thousand objects with it is the one mistake here nobody recovers from. Emptying it is a separate act, one folder at a time. Requires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters:  bucketParams,
					Responses: openapi.Responses{
						"204": openapi.Empty("Deleted."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("The bucket still has objects in it."),
						"502": openapi.TextResponse("The endpoint did not answer."),
					},
				},
			},

			"/objectstores/{name}/buckets/{bucket}/objects": {
				"get": {
					OperationID: "listObjects",
					Summary:     "List one level of a bucket",
					Description: "The folders directly under a prefix and the objects directly in it.\n\n**A folder is a common prefix**, which is the only kind S3 has: it exists while something is under it and stops existing when the last thing goes. An empty one is a zero-byte object whose key ends in a slash, which is the convention every console uses — this listing hides the marker for the folder being listed, so it does not appear as a nameless file inside itself.",
					Tags:        []string{"Object storage"},
					Parameters:  browseParams,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("One level of the bucket.", openapi.Ref("ObjectListing")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"502": openapi.TextResponse("The endpoint did not answer."),
					},
				},
				"put": {
					OperationID: "uploadObject",
					Summary:     "Upload a file",
					Description: "The file is the request body, and its `Content-Type` header becomes the object's.\n\nNot a multipart form, and that is a security decision: `multipart/form-data` is one of the three content types a browser sends cross-site without a preflight, so an endpoint that took one would be reachable from any page a signed-in operator happens to open.\n\nWhen `Content-Length` is absent the body is uploaded in parts rather than buffered to find out how big it is, so a stream of unknown length is fine. Requires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters:  uploadParams,
					RequestBody: &openapi.RequestBody{
						Required:    true,
						Description: "The file's bytes.",
						Content: map[string]openapi.MediaType{
							"application/octet-stream": {Schema: &openapi.Schema{Type: "string", Format: "binary"}},
						},
					},
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The object as written.", openapi.Ref("StoredObject")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"502": openapi.TextResponse("The endpoint did not answer."),
					},
				},
				"delete": {
					OperationID: "deleteObject",
					Summary:     "Delete one file",
					Tags:        []string{"Object storage"},
					Parameters: append(append([]openapi.Parameter{}, bucketParams...),
						openapi.QueryParam("key", "The whole key, from the root of the bucket.")),
					Responses: openapi.Responses{
						"204": openapi.Empty("Deleted."),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"502": openapi.TextResponse("The endpoint did not answer."),
					},
				},
			},

			"/objectstores/{name}/buckets/{bucket}/download": {
				"get": {
					OperationID: "downloadObject",
					Summary:     "Download a file",
					Description: "Streamed through the daemon rather than redirected to a presigned URL, and that is not a shortcut: a managed store's endpoint is a container name that resolves on the Docker network and nowhere else, so a presigned link would be one only the daemon could follow.\n\nThe response is always `Content-Disposition: attachment`. A bucket holds whatever the apps on this instance put there, and rendering an uploaded HTML file inline would run it on this daemon's own origin, beside the session cookie.\n\nRequires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters: append(append([]openapi.Parameter{}, bucketParams...),
						openapi.QueryParam("key", "The whole key, from the root of the bucket.")),
					Responses: openapi.Responses{
						"200": {Description: "The file.", Content: map[string]openapi.MediaType{
							"application/octet-stream": {Schema: &openapi.Schema{Type: "string", Format: "binary"}},
						}},
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"502": openapi.TextResponse("The endpoint did not answer."),
					},
				},
			},

			"/objectstores/{name}/buckets/{bucket}/folders": {
				"post": {
					OperationID: "createFolder",
					Summary:     "Create an empty folder",
					Description: "Writes the zero-byte object whose key ends in a slash. There is no other way: S3 has no directories, so an empty folder is a convention, and this is the one every console uses. Requires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters:  bucketParams,
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"prefix": openapi.String("The folder to create it in. Omit for the root of the bucket."),
						"name":   openapi.String("What to call it. It may not start with a slash or contain a `..` segment."),
					}, "name")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The folder.", openapi.Ref("ObjectFolder")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"502": openapi.TextResponse("The endpoint did not answer."),
					},
				},
				"delete": {
					OperationID: "deleteFolder",
					Summary:     "Delete a folder and everything under it",
					Description: "Everything under it, at every depth, because that is what a folder is — there is nothing else to delete. The count comes back so the answer can say what actually happened: \"deleted\" over a prefix that held four hundred files is worth being told about.\n\nAn empty prefix is refused rather than obeyed: emptying a whole bucket is what a missing query parameter would look like. Requires the admin role.",
					Tags:        []string{"Object storage"},
					Parameters: append(append([]openapi.Parameter{}, bucketParams...),
						openapi.QueryParam("prefix", "The folder, e.g. `backups/2026/`. Required.")),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("How many objects went.", openapi.Object(map[string]*openapi.Schema{
							"removed": openapi.Integer("How many objects were deleted."),
						}, "removed")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"502": openapi.TextResponse("The endpoint did not answer."),
					},
				},
			},
		},
	}
}
