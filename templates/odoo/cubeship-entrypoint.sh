#!/bin/bash
# Runs before the image's /entrypoint.sh, and ends by handing over to it.
set -euo pipefail

: "${ODOO_RC:=/etc/odoo/odoo.conf}"

# Odoo reads the master password only from its configuration file. Written
# at every start, so the value in the app's settings is always the one used.
# Rewritten in place, not with sed -i: that creates a temporary file beside
# it, and /etc/odoo belongs to root while this runs as odoo.
conf=$(grep -vE '^[[:space:]]*admin_passwd[[:space:]]*=' "$ODOO_RC" || true)
printf '%s\nadmin_passwd = %s\n' "$conf" "$ODOO_MASTER_PASSWORD" > "$ODOO_RC"

if [ "${1:-}" = odoo ] && [ "${2:-}" != scaffold ]; then
	wait-for-psql.py --timeout=60

	# A failed query stops here (set -e) rather than reading as "no
	# database": the only databases ever replaced are missing or empty.
	exists=$(psql -d postgres -tA -v db="$PGDATABASE" <<'SQL'
SELECT count(*) FROM pg_database WHERE datname = :'db';
SQL
)
	tables=0
	if [ "$exists" = 1 ]; then
		tables=$(psql -tAc "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'")
	fi

	if [ "$exists" = 0 ] || [ "$tables" = 0 ]; then
		# Cubeship created the database empty, with the server's collation.
		# Odoo recreates it the way it creates its own: from template0,
		# collated C.
		args=(--force --username "$ODOO_ADMIN_LOGIN" --password "$ODOO_ADMIN_PASSWORD"
			--language "${ODOO_LANGUAGE:-en_US}")
		if [ -n "${ODOO_COUNTRY:-}" ]; then
			args+=(--country "$ODOO_COUNTRY")
		fi
		echo "Creating Odoo database $PGDATABASE; this takes a few minutes." >&2
		if ! odoo db init "$PGDATABASE" "${args[@]}"; then
			# Half a database would be started as a whole one next time.
			odoo db drop "$PGDATABASE" || true
			exit 1
		fi
	fi
fi

exec /entrypoint.sh "$@"
