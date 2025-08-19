# Build stage
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN apk add --no-cache nodejs npm
RUN (cd frontend && npm ci && npm run build)
RUN CGO_ENABLED=0 go build -o modsentinel

# Final stage
FROM gcr.io/distroless/static
WORKDIR /
COPY --from=build /src/modsentinel /modsentinel
ENTRYPOINT ["/modsentinel"]
