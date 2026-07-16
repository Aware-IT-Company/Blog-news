FROM golang:1.21-alpine AS builder

WORKDIR /app

# Устанавливаем зависимости сборки
RUN apk add --no-cache git

# Копируем модульные файлы и загружаем зависимости
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходники и собираем бинарник
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o media-service .

# --- Финальный минимальный образ ---
FROM alpine:3.19

WORKDIR /app

# Создаем директории для хранения файлов
RUN mkdir -p uploads/covers uploads/photos uploads/videos

# Копируем бинарник из builder
COPY --from=builder /app/media-service .

EXPOSE 8081

CMD ["./media-service"]
