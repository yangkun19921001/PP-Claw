# ============================================================
# Stage 1: Builder
# ============================================================
FROM docker.1ms.run/golang:1.25-bookworm AS builder

WORKDIR /src

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Build static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/pp-claw .

# ============================================================
# Stage 2: Node Runtime
# ============================================================
FROM docker.1ms.run/node:20-bookworm-slim AS node_runtime

# ============================================================
# Stage 3: Runtime
# ============================================================
FROM docker.1ms.run/debian:bookworm-slim

# Use domestic Debian mirrors to reduce network failures during image builds.
RUN rm -f /etc/apt/sources.list.d/* /etc/apt/sources.list \
    && printf '%s\n' \
    'deb http://mirrors.tuna.tsinghua.edu.cn/debian bookworm main contrib non-free non-free-firmware' \
    'deb http://mirrors.tuna.tsinghua.edu.cn/debian bookworm-updates main contrib non-free non-free-firmware' \
    'deb http://mirrors.tuna.tsinghua.edu.cn/debian-security bookworm-security main contrib non-free non-free-firmware' \
    > /etc/apt/sources.list \
    && apt-get update -o Acquire::Retries=3 \
    && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    curl \
    # --- OCR 依赖 ---
    tesseract-ocr \
    tesseract-ocr-chi-sim \
    tesseract-ocr-chi-tra \
    tesseract-ocr-eng \
    # --- Python (技能依赖) ---
    python3 \
    python3-pip \
    python3-venv \
    # --- 多媒体处理 ---
    ffmpeg \
    file \
    && rm -rf /var/lib/apt/lists/*

# Copy Node.js 20 runtime from the official Node image to avoid extra apt repos.
COPY --from=node_runtime /usr/local/bin/node /usr/local/bin/node
COPY --from=node_runtime /usr/local/bin/npm /usr/local/bin/npm
COPY --from=node_runtime /usr/local/bin/npx /usr/local/bin/npx
COPY --from=node_runtime /usr/local/bin/corepack /usr/local/bin/corepack
COPY --from=node_runtime /usr/local/lib/node_modules /usr/local/lib/node_modules

# Install Python packages (OCR + 其他技能依赖)
RUN pip3 install --no-cache-dir --break-system-packages \
    pytesseract \
    Pillow \
    python-pptx \
    openpyxl
    
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
