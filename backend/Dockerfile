# Stage 1: compile the Go plugin into a .so shared library
FROM heroiclabs/nakama-pluginbuilder:3.22.0 AS builder

WORKDIR /backend

# Copy dependency files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY *.go ./
RUN go build \
    --trimpath \
    --buildmode=plugin \
    -o ./backend.so \
    .

# Stage 2: embed the compiled plugin into the Nakama server image
FROM heroiclabs/nakama:3.22.0

COPY --from=builder /backend/backend.so /nakama/data/modules/backend.so
COPY start.sh /nakama/start.sh
RUN chmod +x /nakama/start.sh

ENTRYPOINT ["/nakama/start.sh"]
