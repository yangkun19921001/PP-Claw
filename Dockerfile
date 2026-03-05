# ============================================================
# Stage 1: Builder
# ============================================================
FROM golang:1.23-bookworm AS builder

WORKDIR /src

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Build static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/pp-claw .

# ============================================================
# Stage 2: Runtime
# ============================================================
FROM debian:bookworm-slim

# Install base packages
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js 20 (required by edge-tts skill)
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# Timezone
ENV TZ=Asia/Shanghai
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/pp-claw .

# Copy runtime assets
COPY skills/ ./skills/
COPY templates/ ./templates/

EXPOSE 18790

ENTRYPOINT ["./pp-claw"]
CMD ["gateway"]
