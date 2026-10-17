# AnythingLLM writes every setting made in its UI — the model provider and its
# key, the password, the key sessions are signed with, the key stored
# credentials are encrypted with — to /app/server/.env, outside its storage
# directory. Upstream mounts that one file from the host. Cubeship mounts
# directories only, so this image changes only that: the file is a link into
# the volume, and the server creates it there the first time it writes.
FROM mintplexlabs/anythingllm:1.18.0
RUN ln -sf /app/server/storage/.env /app/server/.env
