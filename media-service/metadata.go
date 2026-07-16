package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abema/go-mp4"
)

// VideoMetadata — метаданные видеофайла (MP4)
type VideoMetadata struct {
	Filename    string  `json:"filename"`
	Title       string  `json:"title"`
	Author      string  `json:"author"`
	Description string  `json:"description"`
	Duration    float64 `json:"duration_seconds"`
	DurationStr string  `json:"duration"`
	Width       uint32  `json:"width"`
	Height      uint32  `json:"height"`
	FileSize    int64   `json:"file_size"`
	FileSizeStr string  `json:"file_size_human"`
	CreatedAt   string  `json:"created_at"`
	VideoCodec  string  `json:"video_codec"`
	AudioCodec  string  `json:"audio_codec"`
}

// ExtractMP4Metadata — нативно извлекает метаданные из MP4 (без ffmpeg)
func ExtractMP4Metadata(filePath string) (*VideoMetadata, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	metadata := &VideoMetadata{
		Filename:    filepath.Base(filePath),
		FileSize:    fileInfo.Size(),
		FileSizeStr: FormatFileSize(fileInfo.Size()),
	}

	_, err = mp4.ReadBoxStructure(file, func(h *mp4.ReadHandle) (interface{}, error) {
		boxType := h.BoxInfo.Type.String()

		switch boxType {

		// Видеодорожка — разрешение и кодек
		case "avc1":
			box, _, _ := h.ReadPayload()
			if v, ok := box.(*mp4.VisualSampleEntry); ok {
				metadata.Width = uint32(v.Width)
				metadata.Height = uint32(v.Height)
				metadata.VideoCodec = "H.264"
			}

		case "hvc1", "hev1":
			box, _, _ := h.ReadPayload()
			if v, ok := box.(*mp4.VisualSampleEntry); ok {
				metadata.Width = uint32(v.Width)
				metadata.Height = uint32(v.Height)
				metadata.VideoCodec = "H.265"
			}

		// Аудиодорожка
		case "mp4a":
			metadata.AudioCodec = "AAC"

		// Длительность и дата создания
		case "mvhd":
			box, _, _ := h.ReadPayload()
			if mvhd, ok := box.(*mp4.Mvhd); ok {
				if mvhd.Timescale > 0 {
					if mvhd.GetVersion() == 0 {
						metadata.Duration = float64(mvhd.DurationV0) / float64(mvhd.Timescale)
					} else {
						metadata.Duration = float64(mvhd.DurationV1) / float64(mvhd.Timescale)
					}
					metadata.DurationStr = FormatDuration(metadata.Duration)
				}

				var creationTime uint64
				if mvhd.GetVersion() == 0 {
					creationTime = uint64(mvhd.CreationTimeV0)
				} else {
					creationTime = mvhd.CreationTimeV1
				}

				if creationTime > 0 {
					unixTime := int64(creationTime) - 2082844800
					if unixTime > 0 {
						metadata.CreatedAt = time.Unix(unixTime, 0).Format("2006-01-02 15:04:05")
					}
				}
			}

		// Встроенные метаданные (Title, Author, Description)
		case "©nam":
			buf := new(bytes.Buffer)
			if n, _ := h.ReadData(buf); n > 8 {
				metadata.Title = cleanMetadataString(string(buf.Bytes()[8:]))
			}

		case "©ART":
			buf := new(bytes.Buffer)
			if n, _ := h.ReadData(buf); n > 8 {
				metadata.Author = cleanMetadataString(string(buf.Bytes()[8:]))
			}

		case "desc", "©cmt":
			buf := new(bytes.Buffer)
			if n, _ := h.ReadData(buf); n > 8 {
				metadata.Description = cleanMetadataString(string(buf.Bytes()[8:]))
			}
		}

		return h.Expand()
	})

	if err != nil {
		return nil, err
	}

	// Если Title не найден — используем имя файла
	if metadata.Title == "" {
		base := strings.TrimSuffix(metadata.Filename, filepath.Ext(metadata.Filename))
		metadata.Title = strings.ReplaceAll(base, "_", " ")
	}

	return metadata, nil
}

// FormatDuration — секунды в читаемый формат "12:34" или "1:23:45"
func FormatDuration(seconds float64) string {
	d := time.Duration(seconds) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// FormatFileSize — байты в читаемый формат "1.23 MB"
func FormatFileSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// cleanMetadataString — очищает строку от служебных байтов MP4
func cleanMetadataString(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(cleaned)
}
