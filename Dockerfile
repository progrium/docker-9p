FROM alpine:3.6
RUN mkdir -p /run/docker/plugins /mnt/state /mnt/volumes
COPY docker-9p docker-9p
CMD ["docker-9p"]
