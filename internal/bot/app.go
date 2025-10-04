package bot

import (
	"context"
	"fmt"
	"html"
	"log"
	"os"
	"strings"
	"time"

	"github.com/0fl01/voice-shut-up-bot-go/internal/ai"
	"github.com/0fl01/voice-shut-up-bot-go/internal/config"
	"github.com/0fl01/voice-shut-up-bot-go/internal/format"
	"github.com/0fl01/voice-shut-up-bot-go/internal/media"
	"github.com/0fl01/voice-shut-up-bot-go/internal/telegram"
)

type App struct {
	cfg        config.Config
	tele       *telegram.Client
	ai         *ai.Service
	media      *media.Processor
	cache      map[int]string
}

func NewApp(cfg config.Config, tele *telegram.Client, aiSvc *ai.Service, mediaProc *media.Processor) *App {
	return &App{cfg: cfg, tele: tele, ai: aiSvc, media: mediaProc, cache: make(map[int]string)}
}

func (a *App) sendFormattedMessage(chatID int64, replyTo int, text, title string, useSpoiler bool) {
	fullText := text
	if title != "" {
		fullText = fmt.Sprintf("<b>%s</b>\n\n%s", title, text)
	}
	if useSpoiler {
		parts := strings.SplitN(fullText, "\n\n", 2)
		if len(parts) > 1 {
			fullText = fmt.Sprintf("%s\n\n<tg-spoiler>%s</tg-spoiler>", parts[0], parts[1])
		} else {
			fullText = fmt.Sprintf("<tg-spoiler>%s</tg-spoiler>", fullText)
		}
	}
	msgs := format.SplitMessage(fullText, a.cfg.MaxMessageLength)
	for _, m := range msgs {
		if err := a.tele.SendMessage(chatID, m, replyTo, "HTML"); err != nil {
			log.Printf("Ошибка отправки части сообщения в чат %d: %v", chatID, err)
			_ = a.tele.SendMessage(chatID, m, replyTo, "")
		}
	}
}

func (a *App) handleUpdate(update telegram.Update) {
	if update.Message == nil { return }
	msg := update.Message
	log.Printf("Получено сообщение от %d в чате %d", msg.From.ID, msg.Chat.ID)

	if msg.ReplyToMessage != nil && strings.ToLower(strings.TrimSpace(msg.Text)) == "кратко" {
		if originalText, found := a.cache[msg.ReplyToMessage.MessageID]; found {
			_ = a.tele.SendMessage(msg.Chat.ID, "Создаю еще более краткое резюме...", msg.MessageID, "")
			ctx := context.Background()
			shortSummary, err := a.ai.SummarizeText(ctx, originalText, a.cfg.ShortPromptTemplate)
			if err != nil {
				_ = a.tele.SendMessage(msg.Chat.ID, fmt.Sprintf("Ошибка при создании краткого резюме: %v", err), msg.MessageID, "")
			} else {
				a.sendFormattedMessage(msg.Chat.ID, msg.MessageID, format.FormatHTML(shortSummary), "Краткое резюме", false)
			}
		}
		return
	}

	if strings.HasPrefix(msg.Text, "/start") {
		welcome := fmt.Sprintf(
			"Привет! Я бот, который может транскрибировать и суммировать голосовые сообщения, видео и аудиофайлы.\n\n"+
			"Просто отправь мне голосовое сообщение, видео или аудиофайл (mp3, wav, oga), и я преобразую его в текст и создам краткое резюме.\n\n"+
			"P.S Данный бот работает на мощностях Google Gemini AI, использует модели %s и %s для транскрипции и суммаризации\n\n"+
			"Важно: максимальный размер файла для обработки - %d МБ.",
			a.cfg.PrimaryModel, a.cfg.FallbackModel, a.cfg.MaxFileSize/(1024*1024),
		)
		_ = a.tele.SendMessage(msg.Chat.ID, welcome, msg.MessageID, "")
		return
	}

	if msg.Animation != nil || msg.Sticker != nil || msg.Text != "" {
		reply := fmt.Sprintf("Извините, я работаю только с голосовыми сообщениями, видео и аудиофайлами (mp3, wav, oga). Максимальный размер файла - %d МБ.", a.cfg.MaxFileSize/(1024*1024))
		_ = a.tele.SendMessage(msg.Chat.ID, reply, msg.MessageID, "")
		return
	}

	var fileSize int64
	isSupportedDocument := true
	if msg.Voice != nil {
		fileSize = msg.Voice.FileSize
	} else if msg.Audio != nil {
		fileSize = msg.Audio.FileSize
	} else if msg.Video != nil {
		fileSize = msg.Video.FileSize
	} else if msg.VideoNote != nil {
		fileSize = msg.VideoNote.FileSize
	} else if msg.Document != nil {
		fileSize = msg.Document.FileSize
		supported := []string{".mp3", ".wav", ".oga"}
		ok := false
		for _, ext := range supported { if strings.HasSuffix(strings.ToLower(msg.Document.FileName), ext) { ok = true; break } }
		isSupportedDocument = ok
	} else { return }

	if fileSize > a.cfg.MaxFileSize {
		_ = a.tele.SendMessage(msg.Chat.ID, fmt.Sprintf("Извините, максимальный размер файла - %d МБ. Ваш файл слишком большой.", a.cfg.MaxFileSize/(1024*1024)), msg.MessageID, "")
		return
	}
	if !isSupportedDocument {
		_ = a.tele.SendMessage(msg.Chat.ID, "Извините, я могу обрабатывать только аудиофайлы форматов mp3, wav и oga.", msg.MessageID, "")
		return
	}

	_ = a.tele.SendMessage(msg.Chat.ID, "Обрабатываю ваш медиафайл, это может занять некоторое время...", msg.MessageID, "")
	audioPath, err := a.media.SaveAndProcessMedia(msg, a.tele)
	if err != nil {
		log.Printf("Ошибка обработки медиа для сообщения %d: %v", msg.MessageID, err)
		_ = a.tele.SendMessage(msg.Chat.ID, fmt.Sprintf("Произошла ошибка при обработке медиафайла: %v", err), msg.MessageID, "")
		return
	}
	defer os.Remove(audioPath)

	ctx := context.Background()
	transcriptedText, err := a.ai.AudioToText(ctx, audioPath, os.ReadFile)
	if err != nil {
		log.Printf("Ошибка транскрипции для сообщения %d: %v", msg.MessageID, err)
		_ = a.tele.SendMessage(msg.Chat.ID, fmt.Sprintf("Произошла ошибка при транскрипции аудио: %v", err), msg.MessageID, "")
		return
	}
	if transcriptedText == "" {
		_ = a.tele.SendMessage(msg.Chat.ID, "Не удалось распознать речь в аудио.", msg.MessageID, "")
		return
	}

	a.cache[msg.MessageID] = transcriptedText
	a.sendFormattedMessage(msg.Chat.ID, msg.MessageID, html.EscapeString(transcriptedText), "Transcription", false)

	summary, err := a.ai.SummarizeText(ctx, transcriptedText, a.cfg.UserPromptTemplate)
	if err != nil {
		log.Printf("Ошибка суммирования для сообщения %d: %v", msg.MessageID, err)
		_ = a.tele.SendMessage(msg.Chat.ID, fmt.Sprintf("Произошла ошибка при создании резюме: %v", err), msg.MessageID, "")
		return
	}
	a.sendFormattedMessage(msg.Chat.ID, msg.MessageID, format.FormatHTML(summary), "Summary", true)
	log.Printf("Обработка сообщения %d успешно завершена", msg.MessageID)
}

func (a *App) PollUpdates() {
	var offset int
	for {
		updates, err := a.tele.GetUpdates(offset)
		if err != nil {
			log.Printf("Ошибка получения обновлений: %v. Повтор через 3 секунды.", err)
			timeSleep := a.cfg.RetryDelay
			if timeSleep <= 0 { timeSleep = 3 * time.Second }
			<-time.After(timeSleep)
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= offset { offset = update.UpdateID + 1 }
			go a.handleUpdate(update)
		}
	}
}


