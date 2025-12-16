FROM golang:1.25-alpine3.20 AS builder

WORKDIR /app

# Copy go.mod and go.sum for dependency resolution
COPY go.mod go.sum ./

# Copy the main.go file
COPY cmd/hack/rpc_cache/main.go ./

# Build the application
RUN go mod download && \
    go build -o rpc_cache main.go

# Final stage - minimal runtime image
FROM alpine:3.20

# Create non-root user for security
RUN addgroup -g 1000 rpc_cache && \
    adduser -D -u 1000 -G rpc_cache rpc_cache

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/rpc_cache .

# Change ownership to non-root user
RUN chown -R rpc_cache:rpc_cache /app

# Switch to non-root user
USER rpc_cache

# Expose the port the app runs on
EXPOSE 6969

# Run the application
CMD ["./rpc_cache"]

