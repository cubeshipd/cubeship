# The published image creates no database — it leaves that to its web
# database manager — and its master password is a file-only setting.
# Cubeship cannot mount a file or run a command, so this image is that one
# with odoo.conf in place and a script that writes the master password and
# creates the database on first start, then hands over to the image's own
# entrypoint and command.
FROM odoo:19.0-20260908
COPY --chown=odoo odoo.conf /etc/odoo/odoo.conf
COPY --chmod=755 cubeship-entrypoint.sh /usr/local/bin/cubeship-entrypoint.sh
ENTRYPOINT ["cubeship-entrypoint.sh"]
CMD ["odoo"]
