# Paperly Box — single-binary delivery (Vue SPA embedded via go:embed).
#
#   docker build -t paperly/box:latest .
#   docker run -d -p 8080:8080 -v /data/paperly:/data paperly/box:latest
#
# Liveness for orchestrators / `docker ps` health comes from GET /health
# (auth-free, checks DB connectivity + schema readiness, see cmd/server/main.go).

# ---- frontend: Bun builds the Vue SPA into internal/web/dist (go:embed target)
FROM oven/bun:1 AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

# ---- backend: pure-Go static binary (no CGO, see AGENTS.md F3)
FROM golang:1.26 AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=frontend /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /paperly ./cmd/server

# ---- runtime: minimal image + wget-based HEALTHCHECK against /health
FROM alpine:3.21 AS runtime
RUN apk add --no-cache wget
WORKDIR /app
COPY --from=backend /paperly /app/paperly
ENV PAPERLY_DATA_DIR=/data
EXPOSE 8080
VOLUME /data
# Probe port follows PAPERLY_ADDR (default 8080); /health needs no auth.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD port=${PAPERLY_ADDR##*:}; wget -q -O /dev/null http://127.0.0.1:${port:-8080}/health || exit 1
ENTRYPOINT ["/app/paperly"]
