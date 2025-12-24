# --- Stage 1: Build ---
FROM golang:1.24.5-alpine AS builder

# Set the working directory
WORKDIR /app

# Copy dependency files first (optimizes Docker caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code and build
COPY . .
# CGO_ENABLED=0 creates a "static" binary that doesn't need external libraries
RUN CGO_ENABLED=0 GOOS=linux go build -o relay ./src

# --- Stage 2: Run ---
FROM alpine:latest

WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/relay .

# Hugging Face Spaces usually listen on 7860
EXPOSE 7860

# Run the relay
CMD ["./relay"]