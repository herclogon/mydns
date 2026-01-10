# Build stage
FROM golang:alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY main.go ./

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o mydns .

# Final stage - distroless
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy the binary from builder
COPY --from=builder /build/mydns /mydns

# Use non-root user
USER nonroot:nonroot

# Expose DNS ports
EXPOSE 53/udp
EXPOSE 53/tcp

ENTRYPOINT ["/mydns"]
