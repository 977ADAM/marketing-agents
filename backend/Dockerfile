# Образ бэкенда: Go API (/api/*, /healthz) с БД в SQLite.
#
# Контекст сборки — каталог backend/ (docker build ./backend).
# CGO не нужен: драйвер SQLite (modernc.org/sqlite) — чистый Go.
# Миграции применяет отдельный сервис migrate в docker-compose (готовый образ
# dbmate, см. docker-compose.yml и README) — схему API не трогает.

# 1) сборка
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

# 2) рантайм
FROM gcr.io/distroless/static-debian12
COPY --from=build /bin/server /server
# Файл БД живёт на volume: без него данные исчезнут вместе с контейнером.
ENV SQLITE_PATH=/data/marketing.db
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/server"]
