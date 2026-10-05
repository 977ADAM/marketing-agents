# Образы бэкенда: Go API (/api/*, /healthz) и отдельный сервис миграций.
#
# Контекст сборки — каталог backend/ (docker build ./backend).
# CGO не нужен: драйвер SQLite (modernc.org/sqlite) и golang-migrate — чистый Go.
#
# Цели сборки (docker build --target):
#   server  — API (по умолчанию, последняя стадия);
#   migrate — одноразовый сервис миграций: применяет схему и выходит.

# 1) сборка
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server \
 && CGO_ENABLED=0 go build -o /bin/migrate ./cmd/migrate

# 2) сервис миграций: короткоживущий контейнер без ключей API
FROM gcr.io/distroless/static-debian12 AS migrate
COPY --from=build /bin/migrate /migrate
# Файл БД живёт на volume — тот же, что у API: схему применяет миграция, читает сервер.
ENV SQLITE_PATH=/data/marketing.db
VOLUME ["/data"]
ENTRYPOINT ["/migrate"]
CMD ["up"]

# 3) рантайм API
FROM gcr.io/distroless/static-debian12 AS server
COPY --from=build /bin/server /server
ENV SQLITE_PATH=/data/marketing.db
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/server"]
