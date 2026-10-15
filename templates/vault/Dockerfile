# The published image runs `server -dev` when given no command: in memory,
# unsealed, with a root token anyone who reaches it can use, and nothing left
# after a restart. Cubeship runs an image's own command, so this image changes
# that to `server` and adds the configuration it reads.
FROM hashicorp/vault:2.1.1
COPY vault.hcl /vault/config/vault.hcl
# So `vault` run inside the container reaches this server over plain HTTP.
ENV VAULT_ADDR=http://127.0.0.1:8200
CMD ["server"]
