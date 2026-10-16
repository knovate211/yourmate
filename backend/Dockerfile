# One image definition for every service: docker build --build-arg SERVICE=identity .
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SERVICE
RUN test -n "$SERVICE" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./services/${SERVICE}

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app && mkdir -p /data/uploads && chown app /data/uploads
USER app
COPY --from=build /out/app /usr/local/bin/app
ENTRYPOINT ["/usr/local/bin/app"]
