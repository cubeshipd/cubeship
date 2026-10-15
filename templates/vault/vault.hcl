ui = true

# The container has no IPC_LOCK, and integrated storage needs an explicit value.
disable_mlock = true

# Integrated storage requires one. A single node talks only to itself.
cluster_addr = "http://127.0.0.1:8201"

# api_addr comes from VAULT_API_ADDR, which the template sets to the domain.

storage "raft" {
  # The volume. The image creates this directory owned by the vault user.
  path    = "/vault/file"
  node_id = "vault"
}

# TLS ends at the proxy in front of it.
listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_disable = true
}
