########## Build stage ##########
FROM golang:1.24-alpine AS build

WORKDIR /src

# Install Node.js for frontend build
RUN apk add --no-cache nodejs npm

# Go deps (better layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Frontend deps (better layer caching)
COPY frontend/package*.json frontend/
RUN npm ci --prefix frontend

# Copy the rest of the source
COPY . .

# Prepare a data dir to carry ownership into the runtime volume
RUN mkdir -p /data

# Build frontend (must exist before go:embed)
RUN npm run build --prefix frontend \
    && test -f frontend/dist/index.html

# Build statically-linked server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /modsentinel

########## Final stage ##########
# Use distroless base so HTTPS calls have CA certificates available.
FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /

COPY --from=build /modsentinel /modsentinel
COPY --from=build --chown=nonroot:nonroot /data /data

ENV APP_ENV=production
EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/modsentinel"]
