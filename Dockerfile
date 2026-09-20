# --- Build Stage ---
# Pinned to the BUILD platform so the Go compile always runs natively, whatever
# architecture is being targeted: a cross-compile costs nothing here, while an
# emulated compile under QEMU would take multiples of the time. Only the final
# stage's apt-get runs emulated when building for a foreign arch.
FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS builder

WORKDIR /app

# Cache dependencies first for faster rebuilds
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application
COPY . .

# The build's identity, stamped into the binary and surfaced on the Admin page
# and the startup log (internal/app/version.go). Declared here, beside the step
# that consumes them, rather than at the top of the stage: a plain
# `docker build` leaves these defaults, and .github/workflows/docker-publish.yml
# passes the real values.
ARG APP_VERSION=dev
ARG APP_COMMIT=unknown

# Supplied by BuildKit per target platform; empty on a plain `docker build`,
# where Go's own default (the host's arch) is the right answer anyway.
ARG TARGETARCH

# modernc.org/sqlite is pure-Go — no CGO required, so there is no cross
# toolchain to install for the arm64 target.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build \
    -ldflags "-X lastwar-alliance/internal/app.appVersion=${APP_VERSION} -X lastwar-alliance/internal/app.appCommit=${APP_COMMIT}" \
    -o alliance-manager ./cmd/server

# --- Final Stage ---
FROM debian:bookworm-slim

# Install ca-certificates so the app can securely talk to Google Cloud Vision
RUN apt-get update && apt-get install -y ca-certificates gosu && rm -rf /var/lib/apt/lists/*

# Run as a non-root user
RUN useradd -r -u 1001 -s /sbin/nologin appuser

WORKDIR /app

# Copy the compiled binary and necessary directories from the builder
COPY --from=builder /app/alliance-manager .
COPY templates/ ./templates/
COPY static/ ./static/
COPY migrations/ ./migrations/

# Ensure the app user owns the working directory and runtime data paths
RUN mkdir -p /app/data /app/uploads && chown -R appuser:appuser /app

COPY deploy/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Container starts as root so entrypoint can fix bind-mount ownership,
# then drops to appuser via gosu before exec'ing the binary.
EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["./alliance-manager"]