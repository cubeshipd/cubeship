# Upgrading an existing install

## From a release that stored its data in SQLite

**There is no automatic migration, and the old data is not read.** The
daemon now keeps everything in Postgres; on the first start it brings up
a `cubeship-postgres` container, creates an empty schema in it, and
bootstraps a new super-admin. The old `cubeship.db` is left untouched
under the data dir and simply ignored.

For an instance with real data in it, that means recreating your
organizations, projects, users and apps by hand — there are no automated
steps to give you. Pushed images survive (they live in
`registry-data`, not in the database), but an app has to exist again
before a push to it deploys anything.

Two things change for you on that first start:

- A new super-admin key is written to
  `$CUBESHIP_DATA_DIR/admin-api-key`. Every previously issued API key is
  gone with the old database — everyone logs in again.
- The data dir gains `postgres/` (the database) and `postgres-password`.
  Back up the whole directory, as before; `postgres/` is now the part
  that matters most.

To point the daemon at a Postgres you already run instead of the managed
container, set `CUBESHIP_DATABASE_URL` before starting it.

## Any upgrade

`cubeshipd` leaves an already-running `cubeship-registry`,
`cubeship-traefik` or `cubeship-postgres` container alone, and starts it
if it exists but is stopped. A release that changes how those containers
are configured therefore needs the old container removed once, by hand:

```sh
sudo systemctl stop cubeshipd
docker rm -f cubeship-registry
sudo systemctl start cubeshipd
```

Pushed images survive this — they live in `$CUBESHIP_DATA_DIR/registry-data`
on the host, not inside the container.

## From a release without organizations

The first start migrates the database: every app is adopted into an
organization created for it with the slug `default`. Nothing is
redeployed and no image path changes, so existing pushes keep working;
only apps created from then on get the org-prefixed path
(`registry.<domain>/default/<app>`).

Two things change for you:

- The API no longer accepts `CUBESHIP_TOKEN` as a bearer token. Log in
  again with the super-admin key from `$CUBESHIP_DATA_DIR/admin-api-key`,
  which that same start creates.
- `cubeship app create` now needs `--org`.

## From a release without projects and environments

The first start adopts every app that isn't in a project into one created
for its organization with the slug `default`, in that project's
`production` environment. Nothing is redeployed and no image path
changes. `cubeship app create` now needs `--project`.

## From a release with shared htpasswd registry auth

If `cubeship registry login` used to take `--password <daemon-token>` and
every push went through one shared `cubeship` account, the registry has
to switch from that htpasswd credential to per-user token auth. Recreate
the registry container (the `docker rm -f cubeship-registry` step above),
then have every user run `cubeship registry login` again — no
`--password` flag now, it uses their own saved API key.
