package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"
)

type Config struct {
	PrimaryModel        string
	FallbackModel       string
	SystemPrompt        string
	UserPromptTemplate  string
	ShortPromptTemplate string

	PrimaryModelRetries  int
	FallbackModelRetries int
	RetryDelay           time.Duration
}

type Service struct {
	client *genai.Client
	conf   Config
}

func NewService(client *genai.Client, conf Config) *Service { return &Service{client: client, conf: conf} }

func isRetryable(err error) bool {
	if err == nil { return false }
	es := strings.ToLower(err.Error())
	retryable := []string{"503", "429", "500", "overloaded", "unavailable", "timeout", "deadline exceeded"}
	for _, s := range retryable { if strings.Contains(es, s) { return true } }
	return false
}

func (s *Service) generateWithRetry(ctx context.Context, contents []*genai.Content) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= s.conf.PrimaryModelRetries; attempt++ {
		resp, err := s.client.Models.GenerateContent(ctx, s.conf.PrimaryModel, contents, nil)
		if err == nil {
			if txt := resp.Text(); txt != "" { return txt, nil }
			lastErr = fmt.Errorf("API вернул пустой текстовый ответ")
		} else { lastErr = err }
		if isRetryable(lastErr) && attempt < s.conf.PrimaryModelRetries { time.Sleep(s.conf.RetryDelay); continue }
		break
	}
	for attempt := 1; attempt <= s.conf.FallbackModelRetries; attempt++ {
		resp, err := s.client.Models.GenerateContent(ctx, s.conf.FallbackModel, contents, nil)
		if err == nil {
			if txt := resp.Text(); txt != "" { return txt, nil }
			lastErr = fmt.Errorf("API вернул пустой текстовый ответ")
		} else { lastErr = err }
		if isRetryable(lastErr) && attempt < s.conf.FallbackModelRetries { time.Sleep(s.conf.RetryDelay); continue }
		break
	}
	return "", fmt.Errorf("все попытки генерации контента не удались, последняя ошибка: %w", lastErr)
}

func (s *Service) AudioToText(ctx context.Context, filePath string, readFile func(string) ([]byte, error)) (string, error) {
	audioData, err := readFile(filePath)
	if err != nil { return "", fmt.Errorf("не удалось прочитать аудиофайл: %w", err) }
	prompt := genai.NewPartFromText("Пожалуйста, транскрибируйте этот аудио файл в текст на том языке, на котором говорят в записи. Верните только текст транскрипции без дополнительных комментариев.")
	audioPart := genai.NewPartFromBytes(audioData, "audio/mpeg")
	contents := []*genai.Content{{Parts: []*genai.Part{prompt, audioPart}}}
	return s.generateWithRetry(ctx, contents)
}

func (s *Service) SummarizeText(ctx context.Context, textToSummarize, promptTemplate string) (string, error) {
	userPrompt := fmt.Sprintf(promptTemplate, textToSummarize)
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{genai.NewPartFromText(s.conf.SystemPrompt)}},
		{Role: "model", Parts: []*genai.Part{genai.NewPartFromText("Понял, буду следовать указанным правилам форматирования и структуры.")}},
		{Role: "user", Parts: []*genai.Part{genai.NewPartFromText(userPrompt)}},
	}
	return s.generateWithRetry(ctx, contents)
}


