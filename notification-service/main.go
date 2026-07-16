package main

import (
	"log"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

const port = ":8084"

func main() {
	smtpCfg := SMTPConfig{
		Host:     getEnv("SMTP_HOST", "smtp.mail.ru"),
		Port:     getEnv("SMTP_PORT", "465"),
		User:     getEnv("SMTP_USER", ""),
		Password: getEnv("SMTP_PASSWORD", ""),
		From:     getEnv("SMTP_FROM", ""),
		TLS:      getEnv("SMTP_TLS", "true") == "true",
	}

	if smtpCfg.User == "" || smtpCfg.Password == "" {
		log.Fatal("❌ SMTP_USER и SMTP_PASSWORD обязательны (задайте через переменные окружения)")
	}

	mailer := NewMailer(smtpCfg)

	app := fiber.New(fiber.Config{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	})

	app.Use(logger.New())
	app.Use(recover.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept",
		AllowMethods: "GET, POST, OPTIONS",
	}))

	// GET /health — проверка работоспособности
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":  "ok",
			"service": "notification-service",
			"smtp":    smtpCfg.Host + ":" + smtpCfg.Port,
		})
	})

	// POST /api/notify/single — отправить письмо одному адресату
	app.Post("/api/notify/single", mailer.SendSingleHandler)

	// POST /api/notify/bulk — отправить письмо списку адресатов (очередь через RabbitMQ)
	app.Post("/api/notify/bulk", mailer.SendBulkHandler)

	log.Printf("🚀 Notification Service запущен на %s", port)
	log.Printf("📧 SMTP: %s@%s:%s", smtpCfg.User, smtpCfg.Host, smtpCfg.Port)
	log.Fatal(app.Listen(port))
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
