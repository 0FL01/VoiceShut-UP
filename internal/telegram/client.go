package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	baseURL  string
	http     *http.Client
	botToken string
}

func NewClient(botToken, baseURL string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, http: httpClient, botToken: botToken}
}

func (c *Client) GetUpdates(offset int) ([]Update, error) {
	resp, err := c.http.Get(fmt.Sprintf("%s/getUpdates?offset=%d&timeout=60", c.baseURL, offset))
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

func (c *Client) GetFile(fileID string) (*File, error) {
	resp, err := c.http.Get(fmt.Sprintf("%s/getFile?file_id=%s", c.baseURL, fileID))
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

func (c *Client) DownloadFile(filePath string) ([]byte, error) {
	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", c.botToken, filePath)
	resp, err := c.http.Get(fileURL)
	if err != nil {
		return nil, fmt.Errorf("ошибка при скачивании файла: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("не удалось скачать файл, статус: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

type sendMessagePayload struct {
	ChatID           int64  `json:"chat_id"`
	Text             string `json:"text"`
	ParseMode        string `json:"parse_mode,omitempty"`
	ReplyToMessageID int    `json:"reply_to_message_id"`
}

func (c *Client) SendMessage(chatID int64, text string, replyTo int, parseMode string) error {
	payload := sendMessagePayload{ChatID: chatID, Text: text, ParseMode: parseMode, ReplyToMessageID: replyTo}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ошибка маршалинга payload для sendMessage: %w", err)
	}
	resp, err := c.http.Post(fmt.Sprintf("%s/sendMessage", c.baseURL), "application/json", bytes.NewBuffer(payloadBytes))
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


