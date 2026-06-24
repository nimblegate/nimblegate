# nimblegate public demo - read-only dashboard over fabricated seed data.
# Build from repo root:  docker build -f deploy/demo/Dockerfile -t nimblegate-demo .
ARG VERSION=demo
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X nimblegate/internal/version.Version=${VERSION}" \
    -o /out/nimblegate ./cmd/nimblegate

FROM alpine:3.20
RUN apk add --no-cache ca-certificates bash coreutils && adduser -D -u 10001 demo
COPY --from=build /out/nimblegate /usr/local/bin/nimblegate
COPY deploy/demo/demo-seed.sh deploy/demo/entrypoint.sh /opt/demo/
RUN chmod +x /opt/demo/demo-seed.sh /opt/demo/entrypoint.sh && mkdir -p /srv/demo && chown demo /srv/demo
USER demo
EXPOSE 7900
ENTRYPOINT ["/opt/demo/entrypoint.sh"]
