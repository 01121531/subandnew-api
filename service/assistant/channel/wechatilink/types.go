package wechatilink

const (
	MessageTypeUser = 1
	MessageTypeBot  = 2

	MessageStateNew        = 0
	MessageStateGenerating = 1
	MessageStateFinish     = 2

	MessageItemTypeText  = 1
	MessageItemTypeImage = 2
	MessageItemTypeVoice = 3
	MessageItemTypeFile  = 4
	MessageItemTypeVideo = 5

	TypingStatusTyping = 1
	TypingStatusCancel = 2
)

type BaseInfo struct {
	ChannelVersion string `json:"channel_version,omitempty"`
	BotAgent       string `json:"bot_agent,omitempty"`
}

type QRCodeRequest struct {
	LocalTokenList []string `json:"local_token_list,omitempty"`
}

type QRCodeResponse struct {
	Ret                int    `json:"ret,omitempty"`
	ErrCode            int    `json:"errcode,omitempty"`
	ErrMsg             string `json:"errmsg,omitempty"`
	QRCode             string `json:"qrcode,omitempty"`
	QRCodeImageContent string `json:"qrcode_img_content,omitempty"`
}

type LoginStatus string

const (
	LoginStatusWait               LoginStatus = "wait"
	LoginStatusScanned            LoginStatus = "scaned"
	LoginStatusConfirmed          LoginStatus = "confirmed"
	LoginStatusExpired            LoginStatus = "expired"
	LoginStatusScannedRedirect    LoginStatus = "scaned_but_redirect"
	LoginStatusAlreadyBound       LoginStatus = "binded_redirect"
	LoginStatusVerifyCodeRequired LoginStatus = "need_verifycode"
	LoginStatusVerifyCodeBlocked  LoginStatus = "verify_code_blocked"
)

type LoginStatusResponse struct {
	Ret          int         `json:"ret,omitempty"`
	ErrCode      int         `json:"errcode,omitempty"`
	ErrMsg       string      `json:"errmsg,omitempty"`
	Status       LoginStatus `json:"status,omitempty"`
	BotToken     string      `json:"bot_token,omitempty"`
	BotID        string      `json:"ilink_bot_id,omitempty"`
	UserID       string      `json:"ilink_user_id,omitempty"`
	BaseURL      string      `json:"baseurl,omitempty"`
	RedirectHost string      `json:"redirect_host,omitempty"`
}

type CDNMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param,omitempty"`
	AESKey            string `json:"aes_key,omitempty"`
	EncryptType       int    `json:"encrypt_type,omitempty"`
	FullURL           string `json:"full_url,omitempty"`
}

type TextItem struct {
	Text string `json:"text,omitempty"`
}

type ImageItem struct {
	Media      *CDNMedia `json:"media,omitempty"`
	ThumbMedia *CDNMedia `json:"thumb_media,omitempty"`
	AESKey     string    `json:"aeskey,omitempty"`
	URL        string    `json:"url,omitempty"`
}

type VoiceItem struct {
	Media      *CDNMedia `json:"media,omitempty"`
	EncodeType int       `json:"encode_type,omitempty"`
	SampleRate int       `json:"sample_rate,omitempty"`
	Playtime   int       `json:"playtime,omitempty"`
	Text       string    `json:"text,omitempty"`
}

type FileItem struct {
	Media    *CDNMedia `json:"media,omitempty"`
	FileName string    `json:"file_name,omitempty"`
	MD5      string    `json:"md5,omitempty"`
	Length   string    `json:"len,omitempty"`
}

type VideoItem struct {
	Media      *CDNMedia `json:"media,omitempty"`
	ThumbMedia *CDNMedia `json:"thumb_media,omitempty"`
	VideoSize  int       `json:"video_size,omitempty"`
	PlayLength int       `json:"play_length,omitempty"`
	VideoMD5   string    `json:"video_md5,omitempty"`
}

type RefMessage struct {
	MessageItem *MessageItem `json:"message_item,omitempty"`
	Title       string       `json:"title,omitempty"`
}

type MessageItem struct {
	Type         int         `json:"type,omitempty"`
	CreateTimeMS int64       `json:"create_time_ms,omitempty"`
	UpdateTimeMS int64       `json:"update_time_ms,omitempty"`
	IsCompleted  bool        `json:"is_completed,omitempty"`
	MessageID    string      `json:"msg_id,omitempty"`
	RefMessage   *RefMessage `json:"ref_msg,omitempty"`
	Text         *TextItem   `json:"text_item,omitempty"`
	Image        *ImageItem  `json:"image_item,omitempty"`
	Voice        *VoiceItem  `json:"voice_item,omitempty"`
	File         *FileItem   `json:"file_item,omitempty"`
	Video        *VideoItem  `json:"video_item,omitempty"`
}

type Message struct {
	Sequence     int64         `json:"seq,omitempty"`
	MessageID    int64         `json:"message_id,omitempty"`
	FromUserID   string        `json:"from_user_id,omitempty"`
	ToUserID     string        `json:"to_user_id,omitempty"`
	ClientID     string        `json:"client_id,omitempty"`
	CreateTimeMS int64         `json:"create_time_ms,omitempty"`
	UpdateTimeMS int64         `json:"update_time_ms,omitempty"`
	DeleteTimeMS int64         `json:"delete_time_ms,omitempty"`
	SessionID    string        `json:"session_id,omitempty"`
	GroupID      string        `json:"group_id,omitempty"`
	MessageType  int           `json:"message_type,omitempty"`
	MessageState int           `json:"message_state,omitempty"`
	Items        []MessageItem `json:"item_list,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
	RunID        string        `json:"run_id,omitempty"`
}

type GetUpdatesRequest struct {
	UpdatesBuffer string   `json:"get_updates_buf"`
	BaseInfo      BaseInfo `json:"base_info"`
}

type GetUpdatesResponse struct {
	Ret                  int       `json:"ret,omitempty"`
	ErrCode              int       `json:"errcode,omitempty"`
	ErrMsg               string    `json:"errmsg,omitempty"`
	Messages             []Message `json:"msgs,omitempty"`
	UpdatesBuffer        string    `json:"get_updates_buf,omitempty"`
	LongPollingTimeoutMS int64     `json:"longpolling_timeout_ms,omitempty"`
}

type SendMessageRequest struct {
	Message  Message  `json:"msg"`
	BaseInfo BaseInfo `json:"base_info"`
}

type SendMessageResponse struct {
	Ret     int    `json:"ret,omitempty"`
	ErrCode int    `json:"errcode,omitempty"`
	ErrMsg  string `json:"errmsg,omitempty"`
}

type GetConfigRequest struct {
	UserID       string   `json:"ilink_user_id"`
	ContextToken string   `json:"context_token,omitempty"`
	BaseInfo     BaseInfo `json:"base_info"`
}

type GetConfigResponse struct {
	Ret          int    `json:"ret,omitempty"`
	ErrCode      int    `json:"errcode,omitempty"`
	ErrMsg       string `json:"errmsg,omitempty"`
	TypingTicket string `json:"typing_ticket,omitempty"`
}

type SendTypingRequest struct {
	UserID       string   `json:"ilink_user_id"`
	TypingTicket string   `json:"typing_ticket"`
	Status       int      `json:"status"`
	BaseInfo     BaseInfo `json:"base_info"`
}

type SendTypingResponse struct {
	Ret     int    `json:"ret,omitempty"`
	ErrCode int    `json:"errcode,omitempty"`
	ErrMsg  string `json:"errmsg,omitempty"`
}
