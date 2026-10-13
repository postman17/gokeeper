FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/postman17/gokeeper/internal/buildinfo.version=${VERSION} -X github.com/kmorozov/gophkeeper/internal/buildinfo.commit=${COMMIT} -X github.com/kmorozov/gophkeeper/internal/buildinfo.date=${DATE}" \
    -o /out/gophkeeper-server ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/postman17/gokeeper/internal/buildinfo.version=${VERSION} -X github.com/kmorozov/gophkeeper/internal/buildinfo.commit=${COMMIT} -X github.com/kmorozov/gophkeeper/internal/buildinfo.date=${DATE}" \
    -o /out/gophkeeper-client ./cmd/client

FROM alpine:3.20
COPY --from=builder /out/gophkeeper-server /usr/local/bin/gophkeeper-server
COPY --from=builder /out/gophkeeper-client /usr/local/bin/gophkeeper-client

EXPOSE 3200
ENTRYPOINT ["gophkeeper-server"]
