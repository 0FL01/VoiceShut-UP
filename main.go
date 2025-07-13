// Файл: main.go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"google.golang.org/genai"
)

// --- Конфигурация и константы ---

const (
	// Переменные окружения
	botTokenEnv     = "BOT_TOKEN"
	googleAPIKeyEnv = "GOOGLE_API_KEY"

	// Ограничения Telegram
	maxMessageLength = 4096
	maxFileSize      = 20 * 1024 * 1024 // 20 MB

	// Модели Gemini AI (исправлены на актуальные названия)
	primaryModel   = "gemini-2.5-flash"
	fallbackModel  = "gemini-2.0-flash"

	// Настройки ретраев
	primaryModelRetries  = 3
	fallbackModelRetries = 5
	retryDelay           = 3 * time.Second
)

var (
	// Глобальные клиенты
	httpClient   *http.Client
	geminiClient *genai.Client
	botToken     string

	// Базовый URL для Telegram API
	telegramAPIBaseURL string
)

// --- Структуры для работы с Telegram Bot API ---
// Определяем только те поля, которые нам необходимы

type Update struct {
	UpdateID int      `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	MessageID int        `json:"message_id"`
	From      *User      `json:"from"`
	Chat      *Chat      `json:"chat"`
	Text      string     `json:"text"`
	Voice     *Voice     `json:"voice"`
	Audio     *Audio     `json:"audio"`
	Video     *Video     `json:"video"`
	VideoNote *VideoNote `json:"video_note"`
	Document  *Document  `json:"document"`
	Animation *struct{}  `json:"animation"`
	Sticker   *struct{}  `json:"sticker"`
}

type User struct {
	ID int64 `json:"id"`
}

type Chat struct {
	ID int64 `json:"id"`
}

// Общая структура для медиафайлов
type MediaFile struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileSize     int64  `json:"file_size"`
	Duration     int    `json:"duration"`
}

type Voice struct {
	MediaFile
}

type Audio struct {
	MediaFile
	FileName string `json:"file_name"`
}

type Video struct {
	MediaFile
	FileName string `json:"file_name"`
}

type VideoNote struct {
	MediaFile
}

type Document struct {
	MediaFile
	FileName string `json:"file_name"`
}

type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

// Структура для ответа от getFile
type GetFileResponse struct {
	Ok     bool `json:"ok"`
	Result File `json:"result"`
}

// Структура для ответа от getUpdates
type GetUpdatesResponse struct {
	Ok     bool     `json:"ok"`
	Result []Update `json:"result"`
}

// Структура для отправки сообщения
type sendMessagePayload struct {
	ChatID           int64  `json:"chat_id"`
	Text             string `json:"text"`
	ParseMode        string `json:"parse_mode,omitempty"`
	ReplyToMessageID int    `json:"reply_to_message_id"`
}

// --- Функции для взаимодействия с Telegram API ---

// getUpdates получает новые обновления от Telegram
func getUpdates(offset int) ([]Update, error) {
	resp, err := httpClient.Get(fmt.Sprintf("%s/getUpdates?offset=%d&timeout=60", telegramAPIBaseURL, offset))
	if err != nil {
		return nil, fmt.Errorf("ошибка при запросе getUpdates: %w", err)
	}
	defer resp.Body.Close()

	var updatesResp GetUpdatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&updatesResp); err != nil {
		return nil, fmt.Errorf("ошибка декодирования ответа getUpdates: %w", err)
	}

	if !updatesResp.Ok {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ответ от getUpdates не 'ok': %s", string(body))
	}

	return updatesResp.Result, nil
}

// getFile получает информацию о файле по его ID
func getFile(fileID string) (*File, error) {
	resp, err := httpClient.Get(fmt.Sprintf("%s/getFile?file_id=%s", telegramAPIBaseURL, fileID))
	if err != nil {
		return nil, fmt.Errorf("ошибка при запросе getFile: %w", err)
	}
	defer resp.Body.Close()

	var fileResp GetFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil {
		return nil, fmt.Errorf("ошибка декодирования ответа getFile: %w", err)
	}

	if !fileResp.Ok {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ответ от getFile не 'ok': %s", string(body))
	}

	return &fileResp.Result, nil
}

// downloadFile скачивает файл по его пути
func downloadFile(filePath string) ([]byte, error) {
	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", botToken, filePath)
	resp, err := httpClient.Get(fileURL)
	if err != nil {
		return nil, fmt.Errorf("ошибка при скачивании файла: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("не удалось скачать файл, статус: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

// sendMessage отправляет текстовое сообщение в чат
func sendMessage(chatID int64, text string, replyTo int, parseMode string) error {
	payload := sendMessagePayload{
		ChatID:           chatID,
		Text:             text,
		ParseMode:        parseMode,
		ReplyToMessageID: replyTo,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ошибка маршалинга payload для sendMessage: %w", err)
	}

	resp, err := httpClient.Post(fmt.Sprintf("%s/sendMessage", telegramAPIBaseURL), "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("ошибка при отправке запроса sendMessage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("не удалось отправить сообщение, статус: %s, тело: %s", resp.Status, string(body))
	}

	return nil
}

// --- Функции для обработки медиа и вызова FFmpeg ---

// runFFmpeg выполняет команду ffmpeg с заданными аргументами
func runFFmpeg(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute) // 2 минуты таймаут на конвертацию
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	log.Printf("Выполнение FFmpeg: ffmpeg %s", strings.Join(args, " "))

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("ошибка выполнения ffmpeg: %w, вывод: %s", err, stderr.String())
	}
	return nil
}

// convertToMp3 конвертирует аудиофайл в формат MP3 с помощью FFmpeg
func convertToMp3(inputPath, outputPath string) error {
	args := []string{"-y", "-i", inputPath, "-c:a", "libmp3lame", "-q:a", "3", "-ac", "1", "-ar", "22050", outputPath}
	return runFFmpeg(args...)
}

// extractAudioFromVideo извлекает аудиодорожку из видео и сохраняет в MP3
func extractAudioFromVideo(inputPath, outputPath string) error {
	args := []string{"-y", "-i", inputPath, "-vn", "-acodec", "libmp3lame", "-q:a", "2", outputPath}
	return runFFmpeg(args...)
}

// saveAndProcessMedia скачивает, сохраняет и конвертирует медиафайл в MP3
func saveAndProcessMedia(message *Message) (string, error) {
	var fileID, originalFileName string
	var isVideo bool

	switch {
	case message.Voice != nil:
		fileID = message.Voice.FileID
		originalFileName = "voice.oga"
	case message.Audio != nil:
		fileID = message.Audio.FileID
		originalFileName = message.Audio.FileName
	case message.Video != nil:
		fileID = message.Video.FileID
		originalFileName = message.Video.FileName
		isVideo = true
	case message.VideoNote != nil:
		fileID = message.VideoNote.FileID
		originalFileName = "video_note.mp4"
		isVideo = true
	case message.Document != nil:
		fileID = message.Document.FileID
		originalFileName = message.Document.FileName
	default:
		return "", fmt.Errorf("сообщение не содержит поддерживаемого медиафайла")
	}

	log.Printf("Получение информации о файле ID: %s", fileID)
	fileInfo, err := getFile(fileID)
	if err != nil {
		return "", err
	}

	log.Printf("Скачивание файла: %s", fileInfo.FilePath)
	fileContent, err := downloadFile(fileInfo.FilePath)
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
		err = extractAudioFromVideo(tempInputFile.Name(), tempOutputFile.Name())
	} else {
		err = convertToMp3(tempInputFile.Name(), tempOutputFile.Name())
	}

	if err != nil {
		os.Remove(tempOutputFile.Name())
		return "", fmt.Errorf("ошибка конвертации медиа: %w", err)
	}

	log.Printf("Файл успешно сконвертирован в MP3: %s", tempOutputFile.Name())
	return tempOutputFile.Name(), nil
}

// --- Функции для работы с Gemini AI ---

// isRetryableError проверяет, является ли ошибка временной проблемой API
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	retryableCodes := []string{"503", "429", "500", "overloaded", "unavailable", "timeout", "deadline exceeded"}
	for _, code := range retryableCodes {
		if strings.Contains(errStr, code) {
			return true
		}
	}
	return false
}

// generateContentWithRetryAndFallback - обертка для вызова Gemini с ретраями и сменой модели
func generateContentWithRetryAndFallback(ctx context.Context, contents []*genai.Content) (string, error) {
	var lastErr error

	// Попытки с основной моделью
	for attempt := 1; attempt <= primaryModelRetries; attempt++ {
		log.Printf("Попытка с моделью %s (%d/%d)", primaryModel, attempt, primaryModelRetries)
		resp, err := geminiClient.Models.GenerateContent(ctx, primaryModel, contents, nil)

		if err == nil {
			responseText := resp.Text()
			if responseText != "" {
				return responseText, nil
			}
			lastErr = fmt.Errorf("API вернул пустой текстовый ответ")
		} else {
			lastErr = err
		}

		log.Printf("Ошибка с моделью %s (попытка %d): %v", primaryModel, attempt, lastErr)
		if isRetryableError(lastErr) && attempt < primaryModelRetries {
			time.Sleep(retryDelay)
			continue
		}
		break
	}

	log.Printf("Все попытки с основной моделью %s не удались. Переключение на резервную модель %s.", primaryModel, fallbackModel)

	// Попытки с резервной моделью
	for attempt := 1; attempt <= fallbackModelRetries; attempt++ {
		log.Printf("Попытка с моделью %s (%d/%d)", fallbackModel, attempt, fallbackModelRetries)
		resp, err := geminiClient.Models.GenerateContent(ctx, fallbackModel, contents, nil)

		if err == nil {
			responseText := resp.Text()
			if responseText != "" {
				return responseText, nil
			}
			lastErr = fmt.Errorf("API вернул пустой текстовый ответ")
		} else {
			lastErr = err
		}

		log.Printf("Ошибка с моделью %s (попытка %d): %v", fallbackModel, attempt, lastErr)
		if isRetryableError(lastErr) && attempt < fallbackModelRetries {
			time.Sleep(retryDelay)
			continue
		}
		break
	}

	return "", fmt.Errorf("все попытки генерации контента не удались, последняя ошибка: %w", lastErr)
}

// audioToText транскрибирует аудиофайл в текст
func audioToText(ctx context.Context, filePath string) (string, error) {
	log.Printf("Начало транскрипции файла: %s", filePath)
	audioData, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать аудиофайл: %w", err)
	}

	prompt := genai.NewPartFromText("Пожалуйста, транскрибируйте этот аудио файл в текст на том языке, на котором говорят в записи. Верните только текст транскрипции без дополнительных комментариев.")
	audioPart := genai.NewPartFromBytes(audioData, "audio/mpeg")

	contents := []*genai.Content{
		{Parts: []*genai.Part{prompt, audioPart}},
	}

	return generateContentWithRetryAndFallback(ctx, contents)
}

// summarizeText создает краткое резюме текста
func summarizeText(ctx context.Context, textToSummarize string) (string, error) {
	log.Printf("Начало суммирования текста длиной %d символов", len(textToSummarize))

	systemPrompt := `Вы - высококвалифицированный ассистент по обработке и анализу текста, специализирующийся на создании кратких и информативных резюме голосовых сообщений. Ваши ответы всегда должны быть на русском языке. Избегайте использования эмодзи, смайликов и разговорных выражений, таких как 'говорящий' или 'говоритель'. При форматировании текста используйте следующие обозначения:
* **жирный текст** для выделения ключевых понятий
* *курсив* для обозначения важных, но второстепенных деталей
* ` + "```python" + ` для обозначения начала и конца блоков кода
* * в начале строки для создания маркированных списков.
Ваша задача - создавать краткие, но содержательные резюме, выделяя наиболее важную информацию и ключевые моменты из предоставленного текста. Стремитесь к ясности и лаконичности изложения, сохраняя при этом основной смысл и контекст исходного сообщения.`

	userPrompt := fmt.Sprintf(`Ваша цель - обработать и проанализировать следующий текст, полученный из расшифровки голосового сообщения:
%s
Пожалуйста, создайте краткое резюме, соблюдая следующие правила:
1. Начните резюме с горизонтальной линии (---) для визуального разделения.
2. Ограничьте абстрактное резюме максимум шестью предложениями.
3. Выделите жирным шрифтом ключевые слова и фразы в каждом предложении.
4. Если в тексте присутствуют числовые данные или статистика, включите их в резюме, выделив курсивом.
5. Определите основную тему или темы сообщения и укажите их в начале резюме.
6. Если в тексте есть какие-либо действия или рекомендации, выделите их в отдельный маркированный список.
7. В конце резюме добавьте короткий параграф (2-3 предложения) с аналитическим заключением или выводом на основе содержания сообщения.`, textToSummarize)

	// Создаем историю диалога для корректной передачи системного промпта
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{genai.NewPartFromText(systemPrompt)}},
		{Role: "model", Parts: []*genai.Part{genai.NewPartFromText("Понял, буду следовать указанным правилам форматирования и структуры.")}},
		{Role: "user", Parts: []*genai.Part{genai.NewPartFromText(userPrompt)}},
	}

	return generateContentWithRetryAndFallback(ctx, contents)
}

// --- Вспомогательные функции для форматирования и отправки сообщений ---

// formatHTML форматирует Markdown-подобный текст в HTML для Telegram
func formatHTML(text string) string {
	reCodeBlock := regexp.MustCompile("(?s)```(\\w+)?\n(.*?)\n```")
	text = reCodeBlock.ReplaceAllStringFunc(text, func(match string) string {
		parts := reCodeBlock.FindStringSubmatch(match)
		lang := ""
		code := parts[2]
		if len(parts) > 1 {
			lang = parts[1]
		}
		escapedCode := html.EscapeString(strings.TrimSpace(code))
		return fmt.Sprintf(`<pre><code class="language-%s">%s</code></pre>`, lang, escapedCode)
	})

	reBold := regexp.MustCompile(`\*\*(.*?)\*\*`)
	text = reBold.ReplaceAllString(text, `<b>$1</b>`)

	reItalic := regexp.MustCompile(`\*(.*?)\*`)
	text = reItalic.ReplaceAllString(text, `<i>$1</i>`)

	reCode := regexp.MustCompile("`([^`]+)`")
	text = reCode.ReplaceAllString(text, `<code>$1</code>`)

	reListItem := regexp.MustCompile(`(?m)^\* `)
	text = reListItem.ReplaceAllString(text, "• ")

	return text
}

// splitMessage разбивает длинное сообщение на части
func splitMessage(message string) []string {
	if len(message) <= maxMessageLength {
		return []string{message}
	}

	var parts []string
	for len(message) > 0 {
		if len(message) <= maxMessageLength {
			parts = append(parts, message)
			break
		}

		splitPos := strings.LastIndex(message[:maxMessageLength], "\n")
		if splitPos == -1 {
			splitPos = strings.LastIndex(message[:maxMessageLength], " ")
		}
		if splitPos == -1 {
			splitPos = maxMessageLength
		}

		parts = append(parts, message[:splitPos])
		message = strings.TrimSpace(message[splitPos:])
	}
	return parts
}

// sendFormattedMessage отправляет форматированное сообщение, разбивая его при необходимости
func sendFormattedMessage(chatID int64, replyTo int, text, title string, useSpoiler bool) {
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

	messages := splitMessage(fullText)
	for _, msgPart := range messages {
		err := sendMessage(chatID, msgPart, replyTo, "HTML")
		if err != nil {
			log.Printf("Ошибка отправки части сообщения в чат %d: %v", chatID, err)
			errSimple := sendMessage(chatID, msgPart, replyTo, "")
			if errSimple != nil {
				log.Printf("Повторная ошибка отправки (простой текст) в чат %d: %v", chatID, errSimple)
			}
		}
	}
}

// --- Основная логика бота и обработчики ---

// handleUpdate обрабатывает одно обновление от Telegram
func handleUpdate(update Update) {
	if update.Message == nil {
		return
	}

	msg := update.Message
	log.Printf("Получено сообщение от %d в чате %d", msg.From.ID, msg.Chat.ID)

	if strings.HasPrefix(msg.Text, "/start") {
		welcomeMsg := fmt.Sprintf(
			"Привет! Я бот, который может транскрибировать и суммировать голосовые сообщения, видео и аудиофайлы.\n\n"+
				"Просто отправь мне голосовое сообщение, видео или аудиофайл (mp3, wav, oga), и я преобразую его в текст и создам краткое резюме.\n\n"+
				"P.S Данный бот работает на мощностях Google Gemini AI, использует модели %s и %s для транскрипции и суммаризации\n\n"+
				"Важно: максимальный размер файла для обработки - %d МБ.",
			primaryModel, fallbackModel, maxFileSize/(1024*1024),
		)
		sendMessage(msg.Chat.ID, welcomeMsg, msg.MessageID, "")
		return
	}

	if msg.Animation != nil || msg.Sticker != nil {
		replyText := fmt.Sprintf(
			"Извините, я не могу работать с анимациями, гифками и стикерами. "+
				"Я обрабатываю только голосовые сообщения, видео, видео-кружочки и аудиофайлы (mp3, wav, oga). "+
				"Максимальный размер файла - %d МБ.",
			maxFileSize/(1024*1024),
		)
		sendMessage(msg.Chat.ID, replyText, msg.MessageID, "")
		return
	}

	if msg.Text != "" {
		replyText := fmt.Sprintf(
			"Извините, я работаю только с голосовыми сообщениями, видео и аудиофайлами (mp3, wav, oga). "+
				"Пожалуйста, отправьте голосовое сообщение, видео или аудиофайл. "+
				"Максимальный размер файла - %d МБ.",
			maxFileSize/(1024*1024),
		)
		sendMessage(msg.Chat.ID, replyText, msg.MessageID, "")
		return
	}

	var fileSize int64
	var isSupportedDocument bool = true
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
		supportedExts := []string{".mp3", ".wav", ".oga"}
		isSupported := false
		for _, ext := range supportedExts {
			if strings.HasSuffix(strings.ToLower(msg.Document.FileName), ext) {
				isSupported = true
				break
			}
		}
		isSupportedDocument = isSupported
	} else {
		return
	}

	if fileSize > maxFileSize {
		replyText := fmt.Sprintf("Извините, максимальный размер файла - %d МБ. Ваш файл слишком большой.", maxFileSize/(1024*1024))
		sendMessage(msg.Chat.ID, replyText, msg.MessageID, "")
		return
	}

	if !isSupportedDocument {
		sendMessage(msg.Chat.ID, "Извините, я могу обрабатывать только аудиофайлы форматов mp3, wav и oga.", msg.MessageID, "")
		return
	}

	sendMessage(msg.Chat.ID, "Обрабатываю ваш медиафайл, это может занять некоторое время...", msg.MessageID, "")

	audioPath, err := saveAndProcessMedia(msg)
	if err != nil {
		log.Printf("Ошибка обработки медиа для сообщения %d: %v", msg.MessageID, err)
		errMsg := fmt.Sprintf("Произошла ошибка при обработке медиафайла: %v", err)
		sendMessage(msg.Chat.ID, errMsg, msg.MessageID, "")
		return
	}
	defer os.Remove(audioPath)

	ctx := context.Background()

	log.Printf("Начало транскрипции для сообщения %d", msg.MessageID)
	transcriptedText, err := audioToText(ctx, audioPath)
	if err != nil {
		log.Printf("Ошибка транскрипции для сообщения %d: %v", msg.MessageID, err)
		errMsg := fmt.Sprintf("Произошла ошибка при транскрипции аудио: %v", err)
		sendMessage(msg.Chat.ID, errMsg, msg.MessageID, "")
		return
	}

	if transcriptedText == "" {
		log.Printf("Транскрипция для сообщения %d вернула пустой текст", msg.MessageID)
		sendMessage(msg.Chat.ID, "Не удалось распознать речь в аудио.", msg.MessageID, "")
		return
	}

	log.Printf("Транскрипция для сообщения %d завершена, отправка пользователю", msg.MessageID)
	sendFormattedMessage(msg.Chat.ID, msg.MessageID, html.EscapeString(transcriptedText), "Transcription", false)

	log.Printf("Начало суммирования для сообщения %d", msg.MessageID)
	summary, err := summarizeText(ctx, transcriptedText)
	if err != nil {
		log.Printf("Ошибка суммирования для сообщения %d: %v", msg.MessageID, err)
		errMsg := fmt.Sprintf("Произошла ошибка при создании резюме: %v", err)
		sendMessage(msg.Chat.ID, errMsg, msg.MessageID, "")
		return
	}

	log.Printf("Суммирование для сообщения %d завершено, отправка пользователю", msg.MessageID)
	formattedSummary := formatHTML(summary)
	sendFormattedMessage(msg.Chat.ID, msg.MessageID, formattedSummary, "Summary", true)

	log.Printf("Обработка сообщения %d успешно завершена", msg.MessageID)
}

// pollUpdates - главный цикл получения и обработки обновлений
func pollUpdates() {
	var offset int
	for {
		updates, err := getUpdates(offset)
		if err != nil {
			log.Printf("Ошибка получения обновлений: %v. Повтор через 3 секунды.", err)
			time.Sleep(3 * time.Second)
			continue
		}

		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			go handleUpdate(update)
		}
	}
}

// --- Точка входа ---

func main() {
	log.Println("Запуск бота...")

	botToken = os.Getenv(botTokenEnv)
	googleAPIKey := os.Getenv(googleAPIKeyEnv)

	if botToken == "" || googleAPIKey == "" {
		log.Fatalf("Переменные окружения %s и %s должны быть установлены", botTokenEnv, googleAPIKeyEnv)
	}

	telegramAPIBaseURL = fmt.Sprintf("https://api.telegram.org/bot%s", botToken)

	httpClient = &http.Client{Timeout: 65 * time.Second}

	ctx := context.Background()
	var err error
	geminiClient, err = genai.NewClient(ctx, &genai.ClientConfig{APIKey: googleAPIKey})
	if err != nil {
		log.Fatalf("Не удалось создать клиент Gemini: %v", err)
	}

	log.Println("Бот успешно запущен и готов к работе.")
	pollUpdates()
}