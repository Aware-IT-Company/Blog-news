FROM golang:1.21-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o notification-service .

FROM alpine:3.19

WORKDIR /app

COPY --from=builder /app/notification-service .

EXPOSE 8084

CMD ["./notification-service"]
