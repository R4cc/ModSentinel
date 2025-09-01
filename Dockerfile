########## Frontend build stage ##########
FROM node:20-alpine AS webbuild

WORKDIR /src
# Install deps
COPY frontend/package*.json frontend/
RUN npm ci --prefix frontend
# Copy sources and build
COPY frontend frontend
RUN npm run build --prefix frontend \
    && test -f frontend/dist/index.html

########## Go build stage ##########
FROM golang:1.24-alpine AS build

WORKDIR /src

# Go deps (better layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source and built frontend assets
COPY . .
COPY --from=webbuild /src/frontend/dist ./frontend/dist

# Prepare a data dir to carry ownership into the runtime volume
RUN mkdir -p /data

# Build statically-linked server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /modsentinel

########## Final stage ##########
# Use distroless base so HTTPS calls have CA certificates available.
FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /

COPY --from=build /modsentinel /modsentinel
COPY --from=build --chown=nonroot:nonroot /data /data

# Declare data directory for persistence by default
VOLUME ["/data"]

ENV APP_ENV=production
EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/modsentinel"]
