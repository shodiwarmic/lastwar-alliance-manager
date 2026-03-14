# Stage 1: Build the Go binary with CGO and Tesseract using Go 1.25
FROM golang:1.25-bookworm AS builder

WORKDIR /app

# Install the C++ Tesseract/Leptonica dependencies required by gosseract
RUN apt-get update && apt-get install -y \
    tesseract-ocr \
    libtesseract-dev \
    libleptonica-dev \
    gcc \
    g++ \
    && rm -rf /var/lib/apt/lists/*

# Copy the entire project first
COPY . .

# Safely tidy and download dependencies natively in 1.25
RUN go mod tidy
RUN go mod download

# Build the application (CGO_ENABLED=1 is required for gosseract)
RUN CGO_ENABLED=1 GOOS=linux go build -o alliance-manager .

# Stage 2: Clean runtime environment
FROM debian:bookworm-slim

WORKDIR /app

# Install only the runtime Tesseract packages (no compilers needed)
RUN apt-get update && apt-get install -y \
    tesseract-ocr \
    tesseract-ocr-eng \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy the compiled binary and web assets from the builder stage
COPY --from=builder /app/alliance-manager .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static
COPY --from=builder /app/migrations ./migrations

# Expose the web port
EXPOSE 8080

# Start the application
CMD ["./alliance-manager"]