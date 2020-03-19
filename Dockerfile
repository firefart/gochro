FROM golang:latest AS build-env
WORKDIR /src
ENV GO111MODULE=on
COPY go.mod /src/
RUN go mod download
COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -a -o gochro -ldflags="-s -w" -gcflags="all=-trimpath=/src" -asmflags="all=-trimpath=/src"

FROM alpine:latest

RUN apk add --no-cache chromium \
    && rm -rf /var/cache/apk \
    && mkdir -p /var/cache/apk

RUN mkdir -p /app \
    && adduser -D chrome \
    && chown -R chrome:chrome /app

USER chrome
WORKDIR /app

ENV CHROME_BIN=/usr/bin/chromium-browser \
    CHROME_PATH=/usr/lib/chromium/

COPY --from=build-env /src/gochro .

ENTRYPOINT [ "./gochro" ]
