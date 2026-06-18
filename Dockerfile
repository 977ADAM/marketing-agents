# 1) сборка фронта
FROM node:22 AS web
WORKDIR /web
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build            # vite пишет в ../internal/web/dist -> здесь /internal/web/dist

# 2) сборка Go с вшитым dist
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

# 3) рантайм
FROM gcr.io/distroless/static-debian12
COPY --from=build /bin/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
