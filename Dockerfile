# Dockerfile - Recipe for building a container image
# Each line is a "layer" that Docker caches for efficiency

# Stage 1: Build stage - compile Go binaries
# select a base image -> a fresh alpine linux system
# that has Go 1.23 preinstalled, its go compiler of
# version 1.23 on alpine linux
FROM golang:1.23-alpine AS builder

# Install git (needed for go mod download)
# apk is alpine's package manager (like apt on Ubuntu)
RUN apk add --no-cache git

# Set working directory inside the container
# creates /app directory if it doesn't exist
# all subsequent commands (COPY, RUN) execute inside /app
WORKDIR /app

# Copy dependency files first (for Docker layer caching)
# If go.mod/go.sum don't change, Docker reuses this layer
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy all source code
# copy from source to /app
COPY . .

# Build all three binaries
# CGO_ENABLED=0 creates statically linked binaries (no external dependencies)
# -ldflags="-w -s" strips debug info to reduce binary size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/master ./cmd/master
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/chunkserver ./cmd/chunkserver
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/client ./cmd/client

# Stage 2: Runtime stage - minimal image with just the binaries
# alpine is a tiny Linux distribution (~5MB vs hundreds of MB)
FROM alpine:latest

# Install ca-certificates for HTTPS (if needed later)
RUN apk --no-cache add ca-certificates net-tools

# Create a non-root user for security
RUN addgroup -g 1000 dfs && adduser -D -u 1000 -G dfs dfs

# Copy binaries from builder stage
COPY --from=builder /bin/master /usr/local/bin/master
COPY --from=builder /bin/chunkserver /usr/local/bin/chunkserver
COPY --from=builder /bin/client /usr/local/bin/client

# Create directories for data storage and logs
RUN mkdir -p /data /data/log_files /data/files && chown -R dfs:dfs /data

# Switch to non-root user
USER dfs

# Default working directory
WORKDIR /data

# Note: We don't specify CMD or ENTRYPOINT here
# docker-compose.yml will specify which binary to run for each container
