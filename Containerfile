FROM golang:1.25-alpine3.23 AS build

WORKDIR /src
COPY go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . ./
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/pdfrest ./

FROM alpine:3.23.2

RUN addgroup -S app && adduser -S app -G app \
    && apk add --no-cache chromium supervisor ca-certificates ttf-freefont

COPY --from=build /out/pdfrest /usr/local/bin/pdfrest
COPY supervisord.conf /etc/supervisord.conf

EXPOSE 8080

USER app

ENTRYPOINT ["/usr/bin/supervisord", "-c", "/etc/supervisord.conf"]
