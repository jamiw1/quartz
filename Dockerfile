# ── Stage 1: build ────────────────────────────────────────────────────────────
# Use the official Go image to compile a fully-static binary.
# CGO_ENABLED=0 is important: ncruces/go-sqlite3 is WASM-based (pure Go),
# so we don't need cgo and can produce a binary with no external lib deps.
FROM golang:1.24-alpine AS builder

WORKDIR /src

# Cache dependency downloads as a separate layer so they don't re-download
# on every code change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /quartz .

# ── Stage 2: runtime ──────────────────────────────────────────────────────────
# scratch would work but alpine makes it easier to shell in for debugging.
FROM alpine:3.21

# Run as a non-root user.
RUN addgroup -S quartz && adduser -S -G quartz quartz

WORKDIR /app

# Copy the binary and the frontend assets.
COPY --from=builder /quartz ./quartz
COPY --chown=quartz:quartz public/ ./public/

# /data is the mount point for the persistent volume.
# The DB and uploads folder will be created here at runtime.
RUN mkdir -p /data && chown quartz:quartz /data

USER quartz

# Expose the default port (override with the PORT env var).
EXPOSE 3000

ENV PORT=3000
ENV DATA_DIR=/data

ENTRYPOINT ["./quartz"]
