# Upgrading an existing install

## Any upgrade

`cubeshipd` leaves an already-running `cubeship-registry` or
`cubeship-traefik` container alone, and starts it if it exists but is
stopped. A release that changes how those containers are configured
therefore needs the old container removed once, by hand:

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
