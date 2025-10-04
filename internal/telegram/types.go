package telegram

type Update struct {
	UpdateID int      `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	MessageID      int        `json:"message_id"`
	From           *User      `json:"from"`
	Chat           *Chat      `json:"chat"`
	Text           string     `json:"text"`
	ReplyToMessage *Message   `json:"reply_to_message"`
	Voice          *Voice     `json:"voice"`
	Audio          *Audio     `json:"audio"`
	Video          *Video     `json:"video"`
	VideoNote      *VideoNote `json:"video_note"`
	Document       *Document  `json:"document"`
	Animation      *struct{}  `json:"animation"`
	Sticker        *struct{}  `json:"sticker"`
}

type User struct {
	ID int64 `json:"id"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type MediaFile struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileSize     int64  `json:"file_size"`
	Duration     int    `json:"duration"`
}

type Voice struct{ MediaFile }

type Audio struct {
	MediaFile
	FileName string `json:"file_name"`
}

type Video struct {
	MediaFile
	FileName string `json:"file_name"`
}

type VideoNote struct{ MediaFile }

type Document struct {
	MediaFile
	FileName string `json:"file_name"`
}

type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

type GetFileResponse struct {
	Ok     bool `json:"ok"`
	Result File `json:"result"`
}

type GetUpdatesResponse struct {
	Ok     bool     `json:"ok"`
	Result []Update `json:"result"`
}


