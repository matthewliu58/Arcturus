# Use official Go image as base
FROM golang:1.21.3-alpine AS builder

# Set working directory
WORKDIR /app

# Install dependencies
RUN apk add --no-cache git gcc musl-dev

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy project files
COPY . .

# Build control-plane
RUN cd control-plane && go build -o ../bin/control-plane .

# Build data-plane
RUN cd data-plane && go build -o ../bin/data-plane .

# Build data-proxy
RUN cd data-proxy && go build -o ../bin/data-proxy .

# Use alpine as base for runtime
FROM alpine:latest

# Set working directory
WORKDIR /app

# Install dependencies
RUN apk add --no-cache ca-certificates tzdata

# Copy built binaries from builder stage
COPY --from=builder /app/bin/* /app/

# Copy configuration files
COPY control-plane/config.yaml /app/control-plane/config.yaml
COPY data-plane/config.yaml /app/data-plane/config.yaml
COPY data-proxy/config.yaml /app/data-proxy/config.yaml

# Create log directories
RUN mkdir -p /app/control-plane/log /app/data-plane/log /app/data-proxy/log

# Expose ports
EXPOSE 7081 7082 7083 8000-9000 4433

# Copy entrypoint script
COPY entrypoint.sh /app/
RUN chmod +x /app/entrypoint.sh

# Set entrypoint
ENTRYPOINT ["/app/entrypoint.sh"]
