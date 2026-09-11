package backup

import "cubeship/internal/platform/openapi"

var nameParam = []openapi.Parameter{openapi.PathParam("name", "The database.")}
var idParam = []openapi.Parameter{openapi.PathParam("id", "The backup.")}

func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name: "Backups",
			Description: "Copies of the databases this instance runs, and putting one back.\n\n" +
				"**A logical dump, and only that.** It is the one kind that works the same across engines, survives a major version changing under it, and comes out as a single stream that can be sent somewhere else — which is the only property that makes any of this a backup. Copying a data directory is faster to restore, pinned to the exact version it came from, and corrupt if taken while the engine runs. Continuous archiving is a different product.\n\n" +
				"**Redis is not backed up here.** Taking a dump is easy and putting one back is not: an RDB is read once, at startup, so a restore means stopping the server. `--appendonly yes`, which every Redis here runs with, is what carries a queue across a restart.\n\n" +
				"Everything below requires the admin role. A dump is every row in the database, and a restore replaces all of them.",
		}},
		Schemas: map[string]*openapi.Schema{
			"Backup": openapi.Object(map[string]*openapi.Schema{
				"id":              openapi.Integer(""),
				"database":        openapi.String("The database it was taken from, by name. Written down rather than joined, because a backup outlives the database — deleting one is exactly when its backups matter."),
				"database_exists": openapi.Bool("Whether that database is still here. False means this can be downloaded and deleted but not restored: where to put it is a decision, and this release does not make it."),
				"engine":          openapi.String("The engine that produced it."),
				"version":         openapi.String("And its major version. A dump does not load into a different one, and the restore refuses rather than failing partway with the database already half replaced."),
				"store":           openapi.String("The object store it is in, by name. Absent for one on this machine's own disk."),
				"bucket":          openapi.String("The bucket in that store."),
				"key":             openapi.String("The object's key, or the file's name."),
				"off_machine":     openapi.Bool("Whether it is somewhere other than the disk it was taken from. **False is not a backup** in the sense that matters — it survives somebody dropping a table and not the machine — and every screen showing one says so."),
				"size_bytes":      openapi.Integer("What the dump came to. Counted as the bytes went past rather than asked of the far end afterwards."),
				"status":          {Type: "string", Enum: []string{"running", "succeeded", "failed"}, Description: "A row is written before the dump starts, for the reason a deployment row is: the work is detached and where the outcome goes has to exist before there is one."},
				"error":           openapi.String("What the engine said, when it failed. Its own words rather than an exit status, because the status says nothing."),
				"scheduled":       openapi.Bool("Whether the timer asked for this one rather than a person."),
				"started_at":      openapi.String("RFC 3339."),
				"finished_at":     openapi.String("RFC 3339. Absent while it is still running."),
			}, "id", "database", "database_exists", "engine", "version", "key", "off_machine", "size_bytes", "status", "scheduled", "started_at"),

			"BackupSchedule": openapi.Object(map[string]*openapi.Schema{
				"at":          openapi.String(`A time of day, "03:00", on a 24-hour clock.`),
				"timezone":    openapi.String("An IANA name. 03:00 on a server's clock is not the middle of anybody's night."),
				"keep":        openapi.Integer("How many to hold on to, newest first. **Zero keeps every one**, which is a decision rather than a gap — nothing here prunes a bucket on its own. Only successful backups are counted: a week of failures must not push the last good dump out of the window."),
				"store_id":    openapi.Integer("The object store they go to. Absent for this machine's own disk."),
				"bucket":      openapi.String("The bucket in that store."),
				"last_run_at": openapi.String("RFC 3339. Written before the dump starts rather than after, so a dump that takes an hour cannot make the schedule fire again the moment the daemon comes back."),
			}, "at", "timezone", "keep"),
		},
		Paths: map[string]openapi.PathItem{
			"/backups": {
				"get": {
					OperationID: "listBackups",
					Summary:     "List every backup on this instance",
					Description: "Newest first, and **including the ones whose database has been deleted** — which is the only place those still appear.\n\nRequires the admin role.",
					Tags:        []string{"Backups"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The backups.", openapi.Array(openapi.Ref("Backup"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
			},
			"/datastores/{name}/backups": {
				"get": {
					OperationID: "listDatabaseBackups",
					Summary:     "List one database's backups",
					Tags:        []string{"Backups"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The backups, newest first.", openapi.Array(openapi.Ref("Backup"))),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
				"post": {
					OperationID: "takeBackup",
					Summary:     "Take one now",
					Description: "Answers 202 with the row the dump will report into: the work is detached, so a client that times out or hangs up stops waiting rather than stops the backup.\n\nWhere it goes is wherever the schedule says, and this machine's own disk when there is no schedule. Retention is applied when it finishes, so \"keep 7\" counts the one just taken.\n\nRequires the admin role.",
					Tags:        []string{"Backups"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"202": openapi.JSONResponse("The backup, running.", openapi.Ref("Backup")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("That engine is not backed up here."),
					},
				},
			},
			"/datastores/{name}/backups/schedule": {
				"get": {
					OperationID: "getBackupSchedule",
					Summary:     "Read when a database is backed up",
					Description: "404 when there is none, which is what off is: the row existing is the whole of it, so nothing can say a schedule is off while a time sits beside it.",
					Tags:        []string{"Backups"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The schedule.", openapi.Ref("BackupSchedule")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("This database is not backed up on a schedule."),
					},
				},
				"put": {
					OperationID: "setBackupSchedule",
					Summary:     "Back a database up on a schedule",
					Description: "A **time of day** rather than an interval, because what is being chosen is when the database may be busy and slow — and \"every 24 hours from whenever you turned it on\" is not something anybody can plan around.\n\nA missed window runs late rather than being skipped: a daemon that was down at 03:00 and came back at 04:00 takes the backup at 04:00, because a night with no backup is what this exists to prevent and an hour off is not.\n\nRequires the admin role.",
					Tags:        []string{"Backups"},
					Parameters:  nameParam,
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"at":       openapi.String(`"HH:MM" on a 24-hour clock.`),
						"timezone": openapi.String("An IANA name. Defaults to UTC. Resolved here rather than when the timer fires — a name nobody has would otherwise be a schedule that silently never runs."),
						"keep":     openapi.Integer("How many to keep, newest first. Zero keeps every one."),
						"store_id": openapi.Integer("Which object store to put them in. Leave it out for this machine's own disk, which works with nothing configured and is **not a backup**: same disk, same machine, same fire."),
						"bucket":   openapi.String("The bucket in that store. Required when one is named."),
					}, "at")),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The schedule.", openapi.Ref("BackupSchedule")),
						"400": openapi.TextResponse("The time, the timezone, the count or the bucket was not one."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("That engine is not backed up here."),
					},
				},
				"delete": {
					OperationID: "unsetBackupSchedule",
					Summary:     "Stop backing a database up on a schedule",
					Description: "The backups already taken are untouched. Requires the admin role.",
					Tags:        []string{"Backups"},
					Parameters:  nameParam,
					Responses: openapi.Responses{
						"204": openapi.Empty("There is no schedule now."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
					},
				},
			},
			"/backups/{id}/restore": {
				"post": {
					OperationID: "restoreBackup",
					Summary:     "Load a backup back into its database",
					Description: "**This replaces what is in the database and cannot be undone.** Every surface in front of it asks for the database's own name first.\n\nWhat it does not do is stop the database: an app writing during a restore produces a state that is neither the backup nor what was there. Nothing here can prevent that, so it is said rather than pretended otherwise.\n\nRefused for a dump that is still being taken, one that failed, one from a different engine or major version, and one whose database has been deleted.\n\nRequires the admin role.",
					Tags:        []string{"Backups"},
					Parameters:  idParam,
					Responses: openapi.Responses{
						"204": openapi.Empty("The database now holds what the dump held."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("Still running, failed, from another engine or version, or its database is gone."),
					},
				},
			},
			"/backups/{id}/download": {
				"get": {
					OperationID: "downloadBackup",
					Summary:     "Download the dump",
					Description: "Streamed through the daemon rather than redirected: a managed store's endpoint is a container name that resolves on the Docker network and nowhere else, and a backup on the local disk has no URL at all.\n\nAlways `Content-Disposition: attachment`. A dump is somebody's data, and rendering one inline would run whatever is in it on this daemon's own origin, beside the session cookie.\n\nRequires the admin role.",
					Tags:        []string{"Backups"},
					Parameters:  idParam,
					Responses: openapi.Responses{
						"200": {Description: "The dump.", Content: map[string]openapi.MediaType{
							"application/octet-stream": {Schema: &openapi.Schema{Type: "string", Format: "binary"}},
						}},
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("It is still being taken."),
					},
				},
			},
			"/backups/{id}": {
				"delete": {
					OperationID: "deleteBackup",
					Summary:     "Delete a backup and the dump behind it",
					Description: "The object goes first, then the row. A row left behind for a file that is gone is a restore button that fails; a file left behind with no row is bytes in a bucket somebody can see and remove where they are — and of the two, only the second does not lie.\n\nA store this instance can no longer open is not a refusal: nothing here could reach the object, and refusing would leave a row nobody can ever clear.\n\nRequires the admin role.",
					Tags:        []string{"Backups"},
					Parameters:  idParam,
					Responses: openapi.Responses{
						"204": openapi.Empty("It is gone."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.NotFound,
						"409": openapi.TextResponse("It is still being taken."),
					},
				},
			},
		},
	}
}
