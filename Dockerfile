FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /toga ./cmd/toga/

FROM golang:1.26-alpine
RUN apk add --no-cache ca-certificates git
COPY --from=builder /toga /usr/local/bin/toga
EXPOSE 3000
ENTRYPOINT ["toga"]
