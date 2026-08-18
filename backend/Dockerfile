FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux GOFLAGS=-mod=mod go build -a -installsuffix cgo -o doc-server main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates sqlite-libs wget

WORKDIR /app/bin

COPY --from=builder /app/doc-server /app/doc-server

RUN mkdir -p /app/bin/data /app/bin/uploads /app/bin/logs /app/bin/backups

EXPOSE 8090

CMD ["/app/doc-server"]
