FROM golang:1.25-alpine3.24 AS build

WORKDIR /src
COPY go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . ./
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/pdfrest ./src

FROM alpine:3.24.1

RUN addgroup -S app && adduser -S app -G app \
    && apk add --no-cache chromium tini ca-certificates ttf-freefont

COPY --from=build /out/pdfrest /usr/local/bin/pdfrest
COPY VERSION /VERSION

EXPOSE 8080

USER app

ENV CHROME_BIN=/usr/bin/chromium

ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/pdfrest"]
