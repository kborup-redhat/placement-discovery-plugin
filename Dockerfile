# Stage 1: Build frontend
FROM registry.redhat.io/ubi9/nodejs-20:latest AS frontend-builder

USER 0
WORKDIR /app/web

# Copy frontend package files
COPY web/package*.json ./

# Install dependencies
RUN npm ci

# Copy frontend source
COPY web/ ./

# Build frontend
RUN npm run build

# Stage 2: Build Go backend
FROM registry.redhat.io/ubi9/go-toolset:latest AS backend-builder

USER 0
WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY pkg/ pkg/

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o plugin cmd/plugin/main.go

# Stage 3: Runtime
FROM registry.redhat.io/ubi9/ubi-minimal:latest

# Install ca-certificates for HTTPS communication
RUN microdnf install -y ca-certificates && \
    microdnf clean all && \
    rm -rf /var/cache/yum

WORKDIR /app

# Copy the binary from backend builder
COPY --from=backend-builder /workspace/plugin .

# Copy static files from frontend builder
COPY --from=frontend-builder /app/web/dist ./web/dist

# Create non-root user
RUN useradd -r -u 1001 -g 0 plugin && \
    chown -R 1001:0 /app && \
    chmod -R g=u /app

USER 1001

EXPOSE 9002

ENTRYPOINT ["/app/plugin"]
CMD ["--static-path=/app/web/dist"]
