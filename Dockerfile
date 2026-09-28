FROM golang:1.26-alpine AS builder
LABEL maintainer="Datasplice Core Team"

RUN apk add --no-cache bash ca-certificates

WORKDIR /src
COPY . .
RUN XC_OS=linux XC_ARCH=amd64 /bin/bash ./scripts/build.sh

FROM alpine:3.22
LABEL maintainer="Datasplice Core Team"

RUN apk add --no-cache ca-certificates

COPY --from=builder /src/pkg/linux_amd64/datasplice /usr/local/bin/datasplice

WORKDIR /data
ENTRYPOINT ["/usr/local/bin/datasplice"]