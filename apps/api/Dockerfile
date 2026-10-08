# IdeaVault API — build context is the repository root (needs db/migrations):
#   docker build -f apps/api/Dockerfile -t ideavault-api .
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY apps/api/go.mod apps/api/go.sum ./apps/api/
RUN cd apps/api && go mod download
COPY apps/api ./apps/api
ARG VERSION=dev
RUN cd apps/api && CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags="-s -w -X github.com/Prathameshppawar/ideavault/apps/api/internal/handler/http.Version=${VERSION}" \
      -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /out/migrate /app/migrate
COPY db/migrations /app/db/migrations
ENV MIGRATIONS_DIR=/app/db/migrations \
    PORT=8080 \
    APP_ENV=production \
    LOG_FORMAT=json \
    AUTO_MIGRATE=true
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
