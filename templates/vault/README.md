# Vault on Cubeship

[Vault](https://www.vaultproject.io) is HashiCorp's secrets manager: it stores
secrets encrypted, issues short-lived credentials for databases and clouds, and
records who read what.

This template installs one Vault server on a Cubeship instance, with its UI and
API on a domain and its data in a volume.

> **Vault starts sealed, and seals itself again on every restart and every
> deploy.** Until someone enters the unseal keys, it answers but serves no
> secrets. Keep the keys where you can reach them when Vault is down.

## What it creates

- **vault** — Vault `2.1.0`, built on the instance from the `Dockerfile` in
  this repository. The UI and API answer on the domain you choose; its
  integrated storage (Raft, one node) is kept in a volume at `/vault/file`.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

The published image, `hashicorp/vault`, runs `server -dev` when given no
command: in memory, already unsealed, with a root token, and empty after every
restart. Cubeship runs an image's own command, so the `Dockerfile` here is that
image running `server` with [`vault.hcl`](vault.hcl): the UI on, a TCP listener
on port `8200` without TLS — it ends at Cubeship's proxy — Raft storage in the
volume, and memory locking off.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Vault answers | A domain you control, pointed at your instance. It also becomes Vault's `api_addr`. |

Nothing else: Vault's keys and root token are made when you initialize it, not
at install.

## After installing

1. Open the domain. The UI asks you to initialize Vault: choose how many key
   shares to create and how many unseal it (5 and 3 unless you have a reason).
   From a terminal instead:

   ```bash
   export VAULT_ADDR=https://vault.example.com
   vault operator init
   ```

2. **Save the unseal keys and the initial root token now, outside Vault.** They
   are shown once. Without enough keys the data cannot be unsealed by anyone,
   HashiCorp included. Give the shares to different people, or at least to
   different places.
3. Unseal: enter keys up to the threshold in the UI, or run
   `vault operator unseal` once per key.
4. Sign in with the root token, set up an auth method and policies for real
   use, then revoke the root token (`vault token revoke <token>`). A new one
   can be generated later from the unseal keys.

## After every restart

Each deploy, restart, or move of the app starts Vault sealed. Open the domain
and unseal it with the keys again, or run `vault operator unseal` against it.
Apps that read secrets from Vault get errors until you do.

Unsealing without a person — auto-unseal — needs a cloud KMS, an HSM, or
another Vault's transit engine. This template sets none up; add a `seal` stanza
to `vault.hcl` if you have one.

## Reaching it from other apps

Over the domain, `https://<your domain>`, or inside the instance at
`http://cubeship-vault-production-vault:8200` — the internal name follows the
project, environment and app names you install it with. Only HTTP reaches
Vault: the Raft cluster port, `8201`, is not exposed, so this server cannot
join others.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds — and seals it. Everything is in `/vault/file`, encrypted
by Vault; back it up from the app's settings, or take a consistent snapshot
with `vault operator raft snapshot save vault.snap`. A backup is useless
without the unseal keys.

## Memory locking

Cubeship cannot grant the container `IPC_LOCK`, so `disable_mlock = true`.
HashiCorp recommends that for integrated storage anyway, provided the machine
has no swap or only encrypted swap — otherwise secrets in memory can be written
to disk.

## Updating

Change the tag in the `Dockerfile`, release this repository, and point `ref` at
the new release. Read Vault's upgrade notes first, back the volume up, and have
the unseal keys ready: the deploy seals it.

## License

Vault is under the Business Source License since 1.15, not an open-source
license. [OpenBao](https://openbao.org) is the open-source fork, from before
the change.

## Resources

The app is limited to 1 CPU and 1 GiB of memory. Raise `limits` in
`template.yaml` if you need more.
