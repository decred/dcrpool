FROM golang:1.13-alpine as builder

# Install dependencies and build the binaries.
RUN apk add --no-cache --update alpine-sdk git bash \
    && mkdir -p /go/src/dcrpool

COPY .. /go/src/dcrpool

RUN cd / && mkdir /data \
        && cd /go/src/dcrpool \
        && go install \
        && cd docker && ./write-config.sh

# Start a new, final image.
FROM alpine as final

RUN apk --no-cache add \
    bash

# Copy the binaries from the builder image.
COPY --from=builder /go/bin/dcrpool /bin/dcrpool
COPY --from=builder /data/dcrpool.conf /data/dcrpool.conf
COPY --from=builder /go/src/dcrpool/docker/start-dcrpool.sh /etc/start-dcrpool.sh

# Expose Miner ports
EXPOSE 9735 10009

# Specify the start command and entrypoint as the miner client.
ENTRYPOINT ["./etc/start-dcrpool.sh"]
VOLUME ["/data"]
