# syntax=docker/dockerfile:1

# GameMatch API.
#
# Build from the repository root, not from apps/server, so this image and
# apps/web/Dockerfile share one build context and one .dockerignore:
#
#   docker build -t gamematch-api:latest -f apps/server/Dockerfile .
#
# apps/server has no .dockerignore of its own, so a build rooted there would
# carry apps/server/data — the demo database and its photos — into the context.

# Build stage. golang:1.27.2 is the version mise.toml pins, so the image is
# produced by the same toolchain the repository already promises.
FROM golang:1.27.2-alpine AS build

# CGO_ENABLED=0 also comes from mise.toml. The SQLite driver
# (modernc.org/sqlite) is pure Go, so the result is a static binary: no C
# toolchain at build time, and none at run time either.
ENV CGO_ENABLED=0
WORKDIR /src

# Modules in their own layer, so an edit to a .go file reuses it.
COPY apps/server/go.mod apps/server/go.sum ./
RUN go mod download

COPY apps/server/ ./

# -trimpath keeps the build path out of the binary; -s -w drop the symbol table
# and the DWARF data. Neither changes behaviour. No source, no test fixtures and
# no dist/ are copied into the runtime stage — only this one file.
RUN go build -trimpath -ldflags="-s -w" -o /out/gamematch ./cmd/gamematch

# Runtime stage. Pinned to the minor release for the same reason the build stage
# is pinned: an image meant to make a deployment reproducible cannot have a
# floating base. `apk add` below still resolves the newest patch of ca-certs and
# tzdata inside 3.24, so the pin bounds the base rather than freezing every
# package in it.
FROM alpine:3.24 AS runtime

# ca-certificates is what outbound TLS needs (Twilio, Turnstile, web push).
# tzdata is the zoneinfo database; the alpine base carries none, and the store
# formats timestamps in the configured zone.
RUN apk add --no-cache ca-certificates tzdata

# A fixed UID, not a named user, because ./data is chown'ed once on the host to
# match it. A UID baked into the name would change and silently stop that.
RUN addgroup -g 10001 -S gamematch \
 && adduser  -u 10001 -S -G gamematch -h /data gamematch \
 && install -d -o 10001 -g 10001 /data/photos

COPY --from=build /out/gamematch /usr/local/bin/gamematch

# Container layout, not policy. DATA_DIR=/data is where ./data is bind-mounted;
# ADDR must cover every interface because container loopback is not reachable
# from a sibling container, which is how Caddy in the web service reaches this
# one. Whether the process is serving local traffic or production is decided by
# compose and .env (APP_ENV), never baked in here — `docker run` with no
# environment still starts, and `docker compose run --rm api seed` works.
ENV ADDR=0.0.0.0:8000 \
    DATA_DIR=/data

# Caddy's reverse_proxy has no health endpoint to point at unless the API
# declares one, and `depends_on: service_healthy` needs something to poll. Busybox
# wget ships with alpine, so no client has to be added for this.
# --start-interval probes every 2s while the container is starting and settles to
# --interval afterwards, so a dependent service does not sit idle for the first
# full interval waiting for the first answer.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --start-interval=2s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8000/v1/healthz || exit 1

EXPOSE 8000
USER gamematch:gamematch

# Deliberately "/" and not /data: config.Load() falls back to LoadDotenv(".env")
# relative to the working directory, and /data is a bind mount — a stray .env in
# the host's data directory would otherwise be read as configuration. WORKDIR /
# also writes the module path lookup away from the data directory entirely.
WORKDIR /

# ENTRYPOINT, because the binary is the program: `docker run gamematch-api
# seed` is `gamematch seed`, which is what the binary's subcommands
# (main.go: `seed`, `vapid`) exist for, and it means compose does not need
# `entrypoint:` on the api service to run `docker compose run --rm api seed`.
# Running something other than the binary takes an explicit override —
# `docker run --rm --entrypoint id gamematch-api -u`, which is how the
# non-root UID is verified.
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/gamematch"]