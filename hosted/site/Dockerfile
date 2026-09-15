# cubeship.dev, as it ships: the landing page, the docs and /install.sh.
#
# Its own image, built from the repository root so `hosted/site/` is copied
# the way `product/dashboard/` is in its Dockerfile, and run like any other app on a
# Cubeship instance — a container answering on :3000 behind the proxy.
# The docs are compiled in and the search index is built at request time
# from them, and /install.sh is a rewrite to GitHub that the Next server
# performs itself. The public dashboard demo uses browser-local fixtures;
# its loopback server is packaged here, never in the installed product.

FROM node:22-alpine AS demo
WORKDIR /src
RUN corepack enable
COPY product/dashboard/package.json product/dashboard/pnpm-lock.yaml product/dashboard/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY product/dashboard/ ./
RUN pnpm run build:demo
RUN cp -r .next-demo/static .next-demo/standalone/.next-demo/static && \
    cp -r public .next-demo/standalone/public

# --- the build ---------------------------------------------------------
FROM node:22-alpine AS build
WORKDIR /src

RUN corepack enable

# The workspace file travels with the manifest and the lockfile because
# it governs the install (see product/dashboard/Dockerfile for the failure it avoids).
COPY hosted/site/package.json hosted/site/pnpm-lock.yaml hosted/site/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY hosted/site/ ./
RUN pnpm run build

# `output: "standalone"` writes a server plus exactly the files Next
# traced as needed. Static assets and public/ are served from beside the
# server, so they are put back here.
RUN cp -r .next/static .next/standalone/.next/static && \
    if [ -d public ]; then cp -r public .next/standalone/public; fi

# --- what runs ---------------------------------------------------------
FROM node:22-alpine
WORKDIR /app

USER node

COPY --from=build --chown=node:node /src/.next/standalone ./site/
COPY --from=demo --chown=node:node /src/.next-demo/standalone ./demo/
COPY --chown=node:node hosted/site/scripts/serve.mjs ./start.mjs

ENV HOSTNAME=0.0.0.0
ENV PORT=3000
ENV NODE_ENV=production

EXPOSE 3000
CMD ["node", "start.mjs"]
