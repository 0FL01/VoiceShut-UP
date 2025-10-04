package media

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/0fl01/voice-shut-up-bot-go/internal/telegram"
)

type Processor struct{}

func NewProcessor() *Processor { return &Processor{} }

func (p *Processor) runFFmpeg(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	log.Printf("Выполнение FFmpeg: ffmpeg %s", strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ошибка выполнения ffmpeg: %w, вывод: %s", err, stderr.String())
	}
	return nil
}

func (p *Processor) convertToMp3(inputPath, outputPath string) error {
	return p.runFFmpeg("-y", "-i", inputPath, "-c:a", "libmp3lame", "-q:a", "3", "-ac", "1", "-ar", "22050", outputPath)
}

func (p *Processor) extractAudioFromVideo(inputPath, outputPath string) error {
	return p.runFFmpeg("-y", "-i", inputPath, "-vn", "-acodec", "libmp3lame", "-q:a", "2", outputPath)
}

// SaveAndProcessMedia сохраняет файл из Telegram и конвертирует его в mp3, возвращая путь к временному mp3
func (p *Processor) SaveAndProcessMedia(msg *telegram.Message, api *telegram.Client) (string, error) {
	var fileID, originalFileName string
	var isVideo bool
	switch {
	case msg.Voice != nil:
		fileID, originalFileName = msg.Voice.FileID, "voice.oga"
	case msg.Audio != nil:
		fileID, originalFileName = msg.Audio.FileID, msg.Audio.FileName
	case msg.Video != nil:
		fileID, originalFileName, isVideo = msg.Video.FileID, msg.Video.FileName, true
	case msg.VideoNote != nil:
		fileID, originalFileName, isVideo = msg.VideoNote.FileID, "video_note.mp4", true
	case msg.Document != nil:
		fileID, originalFileName = msg.Document.FileID, msg.Document.FileName
	default:
		return "", fmt.Errorf("сообщение не содержит поддерживаемого медиафайла")
	}

	log.Printf("Получение информации о файле ID: %s", fileID)
	fileInfo, err := api.GetFile(fileID)
	if err != nil {
		return "", err
	}
	log.Printf("Скачивание файла: %s", fileInfo.FilePath)
	fileContent, err := api.DownloadFile(fileInfo.FilePath)
	if err != nil {
		return "", err
	}
	tempInputFile, err := os.CreateTemp("", "input-*"+filepath.Ext(originalFileName))
	if err != nil {
		return "", fmt.Errorf("не удалось создать временный входной файл: %w", err)
	}
	defer os.Remove(tempInputFile.Name())
	defer tempInputFile.Close()
	if _, err := tempInputFile.Write(fileContent); err != nil {
		return "", fmt.Errorf("не удалось записать во временный входной файл: %w", err)
	}
	tempInputFile.Close()
	tempOutputFile, err := os.CreateTemp("", "output-*.mp3")
	if err != nil {
		return "", fmt.Errorf("не удалось создать временный выходной файл: %w", err)
	}
	tempOutputFile.Close()
	log.Printf("Конвертация файла: %s -> %s", tempInputFile.Name(), tempOutputFile.Name())
	if isVideo {
		err = p.extractAudioFromVideo(tempInputFile.Name(), tempOutputFile.Name())
	} else {
		err = p.convertToMp3(tempInputFile.Name(), tempOutputFile.Name())
	}
	if err != nil {
		os.Remove(tempOutputFile.Name())
		return "", fmt.Errorf("ошибка конвертации медиа: %w", err)
	}
	log.Printf("Файл успешно сконвертирован в MP3: %s", tempOutputFile.Name())
	return tempOutputFile.Name(), nil
}


