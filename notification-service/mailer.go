package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// SMTPConfig — конфигурация SMTP-сервера
type SMTPConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
	TLS      bool
}

// Mailer — структура для работы с почтой
type Mailer struct {
	cfg SMTPConfig
}

// NotifyRequest — тело запроса для отправки одного письма
type NotifyRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	IsHTML  bool   `json:"is_html"`
}

// BulkNotifyRequest — тело запроса для массовой рассылки
type BulkNotifyRequest struct {
	Recipients []string `json:"recipients"`
	Subject    string   `json:"subject"`
	Body       string   `json:"body"`
	IsHTML     bool     `json:"is_html"`
}

// BulkResult — результат отправки одному получателю
type BulkResult struct {
	Email   string `json:"email"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func NewMailer(cfg SMTPConfig) *Mailer {
	return &Mailer{cfg: cfg}
}

// SendEmail — отправляет письмо одному получателю
func (m *Mailer) SendEmail(to, subject, body string, isHTML bool) error {
	from := m.cfg.From
	if from == "" {
		from = m.cfg.User
	}

	contentType := "text/plain"
	if isHTML {
		contentType = "text/html"
	}

	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: %s; charset=UTF-8\r\nDate: %s\r\n\r\n%s",
		from, to, subject, contentType,
		time.Now().Format(time.RFC1123Z),
		body,
	)

	addr := net.JoinHostPort(m.cfg.Host, m.cfg.Port)
	auth := smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)

	if m.cfg.TLS {
		// SSL/TLS соединение (порт 465)
		tlsCfg := &tls.Config{
			InsecureSkipVerify: false,
			ServerName:         m.cfg.Host,
		}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("TLS dial error: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, m.cfg.Host)
		if err != nil {
			return fmt.Errorf("SMTP client error: %w", err)
		}
		defer client.Close()

		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth error: %w", err)
		}
		if err = client.Mail(from); err != nil {
			return fmt.Errorf("SMTP MAIL FROM error: %w", err)
		}
		if err = client.Rcpt(to); err != nil {
			return fmt.Errorf("SMTP RCPT TO error: %w", err)
		}
		w, err := client.Data()
		if err != nil {
			return fmt.Errorf("SMTP DATA error: %w", err)
		}
		defer w.Close()
		_, err = w.Write([]byte(msg))
		return err
	}

	// STARTTLS соединение (порт 587)
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

// SendSingleHandler — HTTP-обработчик для отправки одного письма
func (m *Mailer) SendSingleHandler(c *fiber.Ctx) error {
	var req NotifyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Неверный формат запроса"})
	}
	if req.To == "" || req.Subject == "" || req.Body == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Обязательные поля: to, subject, body"})
	}

	if err := m.SendEmail(req.To, req.Subject, req.Body, req.IsHTML); err != nil {
		log.Printf("❌ Ошибка отправки письма на %s: %v", req.To, err)
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"to":      req.To,
			"error":   err.Error(),
		})
	}

	log.Printf("✅ Письмо отправлено: %s → %s", req.Subject, req.To)
	return c.JSON(fiber.Map{"success": true, "to": req.To})
}

// SendBulkHandler — HTTP-обработчик для массовой рассылки
func (m *Mailer) SendBulkHandler(c *fiber.Ctx) error {
	var req BulkNotifyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Неверный формат запроса"})
	}
	if len(req.Recipients) == 0 || req.Subject == "" || req.Body == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Обязательные поля: recipients, subject, body"})
	}

	results := make([]BulkResult, 0, len(req.Recipients))
	failedEmails := []string{}

	for _, email := range req.Recipients {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}

		err := m.SendEmail(email, req.Subject, req.Body, req.IsHTML)
		if err != nil {
			log.Printf("❌ Не удалось отправить письмо на %s: %v", email, err)
			results = append(results, BulkResult{Email: email, Success: false, Error: err.Error()})
			failedEmails = append(failedEmails, email)
		} else {
			log.Printf("✅ Письмо отправлено → %s", email)
			results = append(results, BulkResult{Email: email, Success: true})
		}

		// Пауза между письмами — защита от спам-фильтров
		time.Sleep(200 * time.Millisecond)
	}

	response := fiber.Map{
		"total":   len(req.Recipients),
		"sent":    len(req.Recipients) - len(failedEmails),
		"failed":  len(failedEmails),
		"results": results,
	}

	// Если были ошибки — возвращаем список для уведомления редактора
	if len(failedEmails) > 0 {
		response["failed_emails"] = failedEmails
		return c.Status(207).JSON(response) // 207 Multi-Status
	}

	return c.JSON(response)
}
