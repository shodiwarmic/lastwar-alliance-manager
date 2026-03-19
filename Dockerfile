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
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy the compiled binary and necessary directories from the builder
COPY --from=builder /app/alliance-manager .
COPY templates/ ./templates/
COPY static/ ./static/
COPY migrations/ ./migrations/

# (Optional) If you have a default .env.example you want available inside
# COPY .env.example ./

EXPOSE 8080

CMD ["./alliance-manager"]