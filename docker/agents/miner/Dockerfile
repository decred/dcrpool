FROM golang:1.13-alpine as builder

# Install dependencies and build the binaries.
RUN apk add --no-cache --update alpine-sdk git bash \
        mkdir /go/bin \
        git pull https://github.com/decred/dcrpool/cmd/miner \
        && go install

# Start a new, final image.
FROM alpine as final

RUN apk --no-cache add \
    bash

# Copy the binaries from the builder image.
COPY --from=builder /go/bin/miner /bin/miner

EXPOSE 9735

# Specify the start command and entrypoint as the miner client.
CMD ["./start-miner.sh"]

VOLUME ["/data"]
