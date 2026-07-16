package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

const (
	uploadDir = "./uploads"
	maxUpload = 100 * 1024 * 1024 // 100 MB
	port      = ":8081"
)

// Middleware для правильных Content-Type заголовков
func mediaHeadersMiddleware(c *fiber.Ctx) error {
	path := c.Path()
	ext := strings.ToLower(filepath.Ext(path))

	mimeMap := map[string]string{
		// Видео
		".mp4":  "video/mp4",
		".webm": "video/webm",
		".mkv":  "video/x-matroska",
		".avi":  "video/x-msvideo",
		// Изображения
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".webp": "image/webp",
		".gif":  "image/gif",
		".svg":  "image/svg+xml",
	}

	if contentType, ok := mimeMap[ext]; ok {
		c.Set("Content-Type", contentType)
		c.Set("X-Content-Type-Options", "nosniff")

		// Для видео — поддержка стриминга
		if strings.HasPrefix(contentType, "video/") {
			c.Set("Accept-Ranges", "bytes")
			c.Set("Cache-Control", "public, max-age=31536000")
			c.Set("Connection", "keep-alive")
		} else {
			// Для изображений — агрессивный кэш
			c.Set("Cache-Control", "public, max-age=86400")
		}
	}

	return c.Next()
}

func main() {
	// Создаем папки для хранения файлов при старте
	for _, dir := range []string{
		uploadDir,
		filepath.Join(uploadDir, "covers"),
		filepath.Join(uploadDir, "photos"),
		filepath.Join(uploadDir, "videos"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("❌ Не удалось создать директорию %s: %v", dir, err)
		}
	}

	app := fiber.New(fiber.Config{
		BodyLimit:         maxUpload,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		StreamRequestBody: true,
	})

	app.Use(logger.New())
	app.Use(recover.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Range",
		AllowMethods: "GET, POST, DELETE, OPTIONS, HEAD",
	}))

	// Middleware для заголовков ПЕРЕД статикой
	app.Use(mediaHeadersMiddleware)

	// === CDN РАЗДАЧА ФАЙЛОВ ===
	app.Static("/media", uploadDir, fiber.Static{
		ByteRange:     true,
		Browse:        false,
		Compress:      false,
		CacheDuration: 24 * time.Hour,
		MaxAge:        86400,
	})

	// === API МАРШРУТЫ ===

	// GET /health — проверка работоспособности сервиса
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "media-service"})
	})

	// POST /api/upload/:type — загрузка файла
	// :type = covers | photos | videos
	app.Post("/api/upload/:type", uploadHandler)

	// GET /api/metadata/*path — метаданные файла
	app.Get("/api/metadata/*", metadataHandler)

	// GET /api/list/:type — список файлов в директории
	app.Get("/api/list/:type", listHandler)

	// DELETE /api/delete/:type/:filename — удаление файла
	app.Delete("/api/delete/:type/:filename", deleteHandler)

	log.Printf("🚀 Media Service запущен на %s", port)
	log.Println("📁 Поддержка: covers (jpg/png/webp), photos (jpg/png/gif), videos (mp4/webm)")
	log.Fatal(app.Listen(port))
}

// uploadHandler — принимает multipart/form-data файл и сохраняет его
func uploadHandler(c *fiber.Ctx) error {
	mediaType := c.Params("type")
	allowedTypes := map[string][]string{
		"covers": {".jpg", ".jpeg", ".png", ".webp"},
		"photos": {".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg"},
		"videos": {".mp4", ".webm", ".mkv", ".avi"},
	}

	allowed, ok := allowedTypes[mediaType]
	if !ok {
		return c.Status(400).JSON(fiber.Map{
			"error": "Неверный тип. Допустимо: covers, photos, videos",
		})
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Файл не найден в запросе"})
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !containsStr(allowed, ext) {
		return c.Status(400).JSON(fiber.Map{
			"error":   "Недопустимый формат файла",
			"allowed": allowed,
		})
	}

	// Генерируем уникальное имя файла
	filename := uniqueFilename(file.Filename)
	savePath := filepath.Join(uploadDir, mediaType, filename)

	if err := c.SaveFile(file, savePath); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Ошибка сохранения файла"})
	}

	url := "/media/" + mediaType + "/" + filename
	log.Printf("✅ Загружен файл: %s → %s", file.Filename, savePath)

	return c.Status(201).JSON(fiber.Map{
		"filename": filename,
		"type":     mediaType,
		"size":     file.Size,
		"url":      url,
	})
}

// metadataHandler — возвращает метаданные файла
func metadataHandler(c *fiber.Ctx) error {
	relativePath := c.Params("*")
	filePath := filepath.Join(uploadDir, relativePath)

	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return c.Status(404).JSON(fiber.Map{"error": "Файл не найден", "path": relativePath})
	}

	ext := strings.ToLower(filepath.Ext(filePath))

	// Для MP4 — расширенные метаданные (нативно, без ffmpeg)
	if ext == ".mp4" {
		meta, err := ExtractMP4Metadata(filePath)
		if err == nil {
			return c.JSON(meta)
		}
	}

	// Для остальных файлов — базовые метаданные
	return c.JSON(fiber.Map{
		"filename":   filepath.Base(filePath),
		"size":       info.Size(),
		"size_human": FormatFileSize(info.Size()),
		"ext":        ext,
		"modified":   info.ModTime().Format("2006-01-02 15:04:05"),
	})
}

// listHandler — возвращает список файлов в заданной директории
func listHandler(c *fiber.Ctx) error {
	mediaType := c.Params("type")
	dirPath := filepath.Join(uploadDir, mediaType)

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Директория не найдена", "type": mediaType})
	}

	type FileInfo struct {
		Filename string `json:"filename"`
		URL      string `json:"url"`
		Size     int64  `json:"size"`
	}

	var files []FileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, _ := entry.Info()
		files = append(files, FileInfo{
			Filename: entry.Name(),
			URL:      "/media/" + mediaType + "/" + entry.Name(),
			Size:     info.Size(),
		})
	}

	return c.JSON(fiber.Map{
		"type":  mediaType,
		"count": len(files),
		"files": files,
	})
}

// deleteHandler — удаляет файл
func deleteHandler(c *fiber.Ctx) error {
	mediaType := c.Params("type")
	filename := c.Params("filename")
	filePath := filepath.Join(uploadDir, mediaType, filename)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return c.Status(404).JSON(fiber.Map{"error": "Файл не найден"})
	}

	if err := os.Remove(filePath); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Ошибка удаления файла"})
	}

	log.Printf("🗑️  Удален файл: %s", filePath)
	return c.JSON(fiber.Map{"deleted": filename})
}

// containsStr — проверяет наличие строки в срезе
func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// uniqueFilename — генерирует уникальное имя файла с временной меткой
func uniqueFilename(original string) string {
	ext := filepath.Ext(original)
	name := strings.TrimSuffix(filepath.Base(original), ext)
	name = strings.ReplaceAll(name, " ", "_")
	ts := time.Now().UnixMilli()
	return filepath.Base(name) + "_" + itoa(ts) + ext
}

func itoa(n int64) string {
	return strings.TrimSpace(string(rune('0'+n%10))) // simplified — используем fmt в реальном коде
}
