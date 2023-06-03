FROM golang:1.20.2-alpine3.17 AS builder
RUN apk update && apk add --no-cache git
WORKDIR $GOPATH/src/tsnet-serve/
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /go/bin/tsnet-serve main.go
RUN apk del git

FROM scratch
COPY --from=builder /go/bin/tsnet-serve /go/bin/tsnet-serve
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["/go/bin/tsnet-serve"]
