FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server && CGO_ENABLED=0 go build -o /bin/dburl ./cmd/dburl

FROM ghcr.io/amacneil/dbmate:2.36.0 AS migrate
COPY --from=build /bin/dburl /usr/local/bin/dburl

FROM gcr.io/distroless/static-debian12
COPY --from=build /bin/server /server

EXPOSE 8080
ENTRYPOINT ["/server"]
