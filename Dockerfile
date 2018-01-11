FROM golang:1.9-alpine as builder
COPY . /go/src/github.com/progrium/docker-9p
WORKDIR /go/src/github.com/progrium/docker-9p
RUN set -ex \
    && apk add --no-cache --virtual .build-deps \
    gcc libc-dev \
    && go install --ldflags '-extldflags "-static"' \
    && apk del .build-deps
#CMD ["/go/bin/docker-9p"]


FROM alpine:3.6
RUN mkdir -p /run/docker/plugins /mnt/state /mnt/volumes
COPY --from=builder /go/bin/docker-9p .
CMD ["docker-9p"]
