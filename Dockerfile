# --- Build Stage ---
FROM golang:1.25-bookworm AS builder

WORKDIR /app

# Cache dependencies first for faster rebuilds
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application
COPY . .

# Build the binary. CGO_ENABLED=1 is kept for SQLite support.
RUN CGO_ENABLED=1 GOOS=linux go build -o alliance-manager .

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

COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Container starts as root so entrypoint can fix bind-mount ownership,
# then drops to appuser via gosu before exec'ing the binary.
EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["./alliance-manager"]