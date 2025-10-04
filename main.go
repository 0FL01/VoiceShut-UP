package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
    "time"

	"github.com/0fl01/voice-shut-up-bot-go/internal/ai"
	"github.com/0fl01/voice-shut-up-bot-go/internal/bot"
	"github.com/0fl01/voice-shut-up-bot-go/internal/config"
	"github.com/0fl01/voice-shut-up-bot-go/internal/media"
	"github.com/0fl01/voice-shut-up-bot-go/internal/telegram"
	"google.golang.org/genai"
)

func main() {
	log.Println("Запуск бота...")

	cfg := config.LoadFromEnv()
	if cfg.BotToken == "" || cfg.GoogleAPIKey == "" {
		log.Fatalf("Переменные окружения %s и %s должны быть установлены", config.EnvBotToken, config.EnvGoogleAPIKey)
	}

	apiBaseURL := fmt.Sprintf("https://api.telegram.org/bot%s", cfg.BotToken)
    httpClient := &http.Client{Timeout: 65 * time.Second}
	ctx := context.Background()

	gClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: cfg.GoogleAPIKey})
	if err != nil {
		log.Fatalf("Не удалось создать клиент Gemini: %v", err)
	}

	aiSvc := ai.NewService(gClient, ai.Config{
		PrimaryModel:        cfg.PrimaryModel,
		FallbackModel:       cfg.FallbackModel,
		SystemPrompt:        cfg.SystemPrompt,
		UserPromptTemplate:  cfg.UserPromptTemplate,
		ShortPromptTemplate: cfg.ShortPromptTemplate,
		PrimaryModelRetries:  cfg.PrimaryModelRetries,
		FallbackModelRetries: cfg.FallbackModelRetries,
		RetryDelay:           cfg.RetryDelay,
	})

	tele := telegram.NewClient(cfg.BotToken, apiBaseURL, httpClient)
	mediaProc := media.NewProcessor()

	application := bot.NewApp(cfg, tele, aiSvc, mediaProc)
	log.Println("Бот успешно запущен и готов к работе.")
	application.PollUpdates()
}


