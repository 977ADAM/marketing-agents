# Образ бэкенда: Go API (/api/*, /healthz), данные — в MariaDB.
#
# Контекст сборки — каталог backend/ (docker build ./backend).
# CGO не нужен: драйвер MariaDB (go-sql-driver/mysql) — чистый Go, поэтому
# рантайм остаётся статическим distroless-образом. Удаление драйвера SQLite
# (modernc.org/sqlite) уменьшило бинарь примерно на 5 МБ.
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
# Адрес БД приложение собирает из MARIADB_* (см. .env.example); DATABASE_URL
# имеет приоритет. Значений по умолчанию здесь нет намеренно: адрес БД — не то,
# что стоит угадывать молча.
EXPOSE 8080
ENTRYPOINT ["/server"]
