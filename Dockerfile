FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/all-notify ./cmd/all-notify

FROM alpine:3.20

RUN mkdir -p /data
WORKDIR /app
COPY --from=build /out/all-notify /app/all-notify

ENV ALL_NOTIFY_ADDR=:8080
ENV ALL_NOTIFY_DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/app/all-notify"]
