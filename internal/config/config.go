package config

import (
	"os"
	"time"
)

// Ключи переменных окружения
const (
	EnvBotToken     = "BOT_TOKEN"
	EnvGoogleAPIKey = "GOOGLE_API_KEY"
	EnvPrimaryModel = "PRIMARY_MODEL"
	EnvFallbackModel = "FALLBACK_MODEL"
	EnvSystemPrompt = "SYSTEM_PROMPT"
	EnvUserPromptTemplate = "USER_PROMPT_TEMPLATE"
	EnvShortPromptTemplate = "SHORT_PROMPT_TEMPLATE"
)

// Значения по умолчанию
const (
	DefaultPrimaryModel  = "gemini-2.5-flash"
	DefaultFallbackModel = "gemini-2.0-flash"
)

var (
	DefaultSystemPrompt = `Вы - высококвалифицированный ассистент по обработке и анализу текста, специализирующийся на создании кратких и информативных резюме голосовых сообщений. Ваши ответы всегда должны быть на русском языке. Избегайте использования эмодзи, смайликов и разговорных выражений, таких как 'говорящий' или 'говоритель'. При форматировании текста используйте следующие обозначения:
* **жирный текст** для выделения ключевых понятий
* *курсив* для обозначения важных, но второстепенных деталей
* ` + "```python" + ` для обозначения начала и конца блоков кода
* * в начале строки для создания маркированных списков.
Ваша задача - создавать краткие, но содержательные резюме, выделяя наиболее важную информацию и ключевые моменты из предоставленного текста. Стремитесь к ясности и лаконичности изложения, сохраняя при этом основной смысл и контекст исходного сообщения.`

	DefaultUserPromptTemplate = `Ваша цель - обработать и проанализировать следующий текст, полученный из расшифровки голосового сообщения:
%s
Пожалуйста, создайте краткое резюме, соблюдая следующие правила:
1. Начните резюме с горизонтальной линии (---) для визуального разделения.
2. Ограничьте абстрактное резюме максимум шестью предложениями.
3. Выделите жирным шрифтом ключевые слова и фразы в каждом предложении.
4. Если в тексте присутствуют числовые данные или статистика, включите их в резюме, выделив курсивом.
5. Определите основную тему или темы сообщения и укажите их в начале резюме.
6. Если в тексте есть какие-либо действия или рекомендации, выделите их в отдельный маркированный список.
7. В конце резюме добавьте короткий параграф (2-3 предложения) с аналитическим заключением или выводом на основе содержания сообщения.`

	DefaultShortPromptTemplate = `Сделай очень краткое резюме (1-2 предложения) на основе этого текста, выделив только самую главную мысль: %s`
)

type Config struct {
	BotToken            string
	GoogleAPIKey        string
	PrimaryModel        string
	FallbackModel       string
	SystemPrompt        string
	UserPromptTemplate  string
	ShortPromptTemplate string

	MaxMessageLength    int
	MaxFileSize         int64

	PrimaryModelRetries  int
	FallbackModelRetries int
	RetryDelay           time.Duration
}

func getEnvOrDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func LoadFromEnv() Config {
	return Config{
		BotToken:            os.Getenv(EnvBotToken),
		GoogleAPIKey:        os.Getenv(EnvGoogleAPIKey),
		PrimaryModel:        getEnvOrDefault(EnvPrimaryModel, DefaultPrimaryModel),
		FallbackModel:       getEnvOrDefault(EnvFallbackModel, DefaultFallbackModel),
		SystemPrompt:        getEnvOrDefault(EnvSystemPrompt, DefaultSystemPrompt),
		UserPromptTemplate:  getEnvOrDefault(EnvUserPromptTemplate, DefaultUserPromptTemplate),
		ShortPromptTemplate: getEnvOrDefault(EnvShortPromptTemplate, DefaultShortPromptTemplate),
		MaxMessageLength:    4096,
		MaxFileSize:         20 * 1024 * 1024,
		PrimaryModelRetries:  3,
		FallbackModelRetries: 5,
		RetryDelay:           3 * time.Second,
	}
}


