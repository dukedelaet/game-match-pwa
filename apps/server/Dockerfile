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

# Runtime stage.
FROM alpine AS runtime

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
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8000/v1/healthz || exit 1

EXPOSE 8000
USER gamematch:gamematch

# Deliberately "/" and not /data: config.Load() falls back to LoadDotenv(".env")
# relative to the working directory, and /data is a bind mount — a stray .env in
# the host's data directory would otherwise be read as configuration. WORKDIR /
# also writes the module path lookup away from the data directory entirely.
WORKDIR /

# CMD, not ENTRYPOINT, and the difference is the point: `docker run --rm
# gamematch-api:latest id -u` must run id(1) and print 10001. An ENTRYPOINT on
# the binary turns that into `gamematch id -u`, which is not a subcommand the
# binary knows — it starts a server and hangs instead of answering. Compose
# declares `entrypoint: ["/usr/local/bin/gamematch"]` on the api service, which
# keeps `docker compose run --rm api seed` appending seed to the binary.
STOPSIGNAL SIGTERM
CMD ["/usr/local/bin/gamematch"]