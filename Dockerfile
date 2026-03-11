# Stage 1: Build frontend
FROM registry.redhat.io/ubi9/nodejs-20@sha256:631ee34c98ffb95456cb0f35dd3b8beb5c1f16a2875f9c02d1ff28bf98a9f292 AS frontend-builder

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
FROM registry.redhat.io/ubi9/go-toolset@sha256:632bb2d1e3c0c3450d8ce9627e81aed7b340ca84d9700db61d510752cf0f506b AS backend-builder

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
FROM registry.redhat.io/ubi9/ubi-minimal@sha256:69f5c9886ecb19b23e88275a5cd904c47dd982dfa370fbbd0c356d7b1047ef68

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
