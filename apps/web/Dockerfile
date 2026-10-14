# syntax=docker/dockerfile:1

# GameMatch PWA, served by Caddy.
#
#   pnpm --filter web build      # apps/web/dist is gitignored: build it first
#   docker build -t gamematch-web:latest -f apps/web/Dockerfile .
#
# The context is the repository root, not apps/web, because deploy/Caddyfile is
# one level up and a Dockerfile cannot COPY from outside its context.
#
# This image builds nothing. Vite runs on the host and its output is copied in,
# so a frontend change never invalidates the API image, and neither pnpm nor Node
# ever enters a runtime image.

FROM caddy:2.10.2-alpine

# /srv/dist is the default root in deploy/Caddyfile, so the file lands exactly
# where the config looks when GAMEMATCH_ROOT is unset. The trailing slash copies
# the *contents* of dist/ into the destination rather than nesting a dist
# directory inside it.
COPY apps/web/dist/ /srv/dist/
COPY deploy/Caddyfile /etc/caddy/Caddyfile