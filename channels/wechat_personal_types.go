package channels

import (
	"fmt"
	"time"
)

const (
	wechatMessageTypeUser = 1
	wechatMessageTypeBot  = 2

	wechatMessageStateFinish = 2

	wechatMessageItemText  = 1
	wechatMessageItemImage = 2
	wechatMessageItemVoice = 3
	wechatMessageItemFile  = 4
	wechatMessageItemVideo = 5

	wechatMediaTypeImage = 1
	wechatMediaTypeVideo = 2
	wechatMediaTypeFile  = 3
	wechatMediaTypeVoice = 4

	wechatTypingStatusTyping = 1
	wechatTypingStatusCancel = 2
)

type wechatBaseInfo struct {
	ChannelVersion string `json:"channel_version,omitempty"`
}

// wechatBaseResponse captures the business-level error fields that WeChat APIs
// may return inside an HTTP 200 response body. Embed this in any response struct
// that needs error checking.
type wechatBaseResponse struct {
	Ret     int    `json:"ret,omitempty"`
	ErrCode int    `json:"errcode,omitempty"`
	ErrMsg  string `json:"errmsg,omitempty"`
}

func (r *wechatBaseResponse) bizErr() error {
	if r == nil || (r.Ret == 0 && r.ErrCode == 0) {
		return nil
	}
	msg := r.ErrMsg
	if msg == "" {
		msg = "unknown error"
	}
	return fmt.Errorf("wechat biz error ret=%d errcode=%d: %s", r.Ret, r.ErrCode, msg)
}

type wechatCDNMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param,omitempty"`
	FullURL           string `json:"full_url,omitempty"`
	AESKey            string `json:"aes_key,omitempty"`
	EncryptType       int    `json:"encrypt_type,omitempty"`
}

type wechatTextItem struct {
	Text string `json:"text,omitempty"`
}

type wechatImageItem struct {
	Media      *wechatCDNMedia `json:"media,omitempty"`
	ThumbMedia *wechatCDNMedia `json:"thumb_media,omitempty"`
	AESKeyHex  string          `json:"aeskey,omitempty"`
	URL        string          `json:"url,omitempty"`
	MidSize    int             `json:"mid_size,omitempty"`
}

type wechatVoiceItem struct {
	Media         *wechatCDNMedia `json:"media,omitempty"`
	EncodeType    int             `json:"encode_type,omitempty"`
	BitsPerSample int             `json:"bits_per_sample,omitempty"`
	SampleRate    int             `json:"sample_rate,omitempty"`
	Playtime      int             `json:"playtime,omitempty"`
	Text          string          `json:"text,omitempty"`
}

type wechatFileItem struct {
	Media    *wechatCDNMedia `json:"media,omitempty"`
	FileName string          `json:"file_name,omitempty"`
	Len      string          `json:"len,omitempty"`
}

type wechatVideoItem struct {
	Media      *wechatCDNMedia `json:"media,omitempty"`
	ThumbMedia *wechatCDNMedia `json:"thumb_media,omitempty"`
	VideoSize  int             `json:"video_size,omitempty"`
}

type wechatRefMessage struct {
	Title       string             `json:"title,omitempty"`
	MessageItem *wechatMessageItem `json:"message_item,omitempty"`
}

type wechatMessageItem struct {
	Type      int               `json:"type,omitempty"`
	TextItem  *wechatTextItem   `json:"text_item,omitempty"`
	ImageItem *wechatImageItem  `json:"image_item,omitempty"`
	VoiceItem *wechatVoiceItem  `json:"voice_item,omitempty"`
	FileItem  *wechatFileItem   `json:"file_item,omitempty"`
	VideoItem *wechatVideoItem  `json:"video_item,omitempty"`
	RefMsg    *wechatRefMessage `json:"ref_msg,omitempty"`
}

type wechatMessage struct {
	Seq          int                  `json:"seq,omitempty"`
	MessageID    int64                `json:"message_id,omitempty"`
	FromUserID   string               `json:"from_user_id,omitempty"`
	ToUserID     string               `json:"to_user_id,omitempty"`
	ClientID     string               `json:"client_id,omitempty"`
	CreateTimeMS int64                `json:"create_time_ms,omitempty"`
	SessionID    string               `json:"session_id,omitempty"`
	MessageType  int                  `json:"message_type,omitempty"`
	MessageState int                  `json:"message_state,omitempty"`
	ItemList     []*wechatMessageItem `json:"item_list,omitempty"`
	ContextToken string               `json:"context_token,omitempty"`
}

type wechatGetUpdatesRequest struct {
	GetUpdatesBuf string         `json:"get_updates_buf,omitempty"`
	BaseInfo      wechatBaseInfo `json:"base_info,omitempty"`
}

type wechatGetUpdatesResponse struct {
	Ret                int              `json:"ret,omitempty"`
	ErrCode            int              `json:"errcode,omitempty"`
	ErrMsg             string           `json:"errmsg,omitempty"`
	Msgs               []*wechatMessage `json:"msgs,omitempty"`
	GetUpdatesBuf      string           `json:"get_updates_buf,omitempty"`
	LongPollingTimeout int              `json:"longpolling_timeout_ms,omitempty"`
}

type wechatSendMessageRequest struct {
	Msg      *wechatMessage `json:"msg,omitempty"`
	BaseInfo wechatBaseInfo `json:"base_info,omitempty"`
}

type wechatGetUploadURLRequest struct {
	FileKey         string         `json:"filekey,omitempty"`
	MediaType       int            `json:"media_type,omitempty"`
	ToUserID        string         `json:"to_user_id,omitempty"`
	RawSize         int            `json:"rawsize,omitempty"`
	RawFileMD5      string         `json:"rawfilemd5,omitempty"`
	FileSize        int            `json:"filesize,omitempty"`
	ThumbRawSize    int            `json:"thumb_rawsize,omitempty"`
	ThumbRawFileMD5 string         `json:"thumb_rawfilemd5,omitempty"`
	ThumbFileSize   int            `json:"thumb_filesize,omitempty"`
	NoNeedThumb     bool           `json:"no_need_thumb,omitempty"`
	AESKey          string         `json:"aeskey,omitempty"`
	BaseInfo        wechatBaseInfo `json:"base_info,omitempty"`
}

type wechatGetUploadURLResponse struct {
	wechatBaseResponse
	UploadParam      string `json:"upload_param,omitempty"`
	UploadFullURL    string `json:"upload_full_url,omitempty"`
	ThumbUploadParam string `json:"thumb_upload_param,omitempty"`
}

type wechatGetConfigRequest struct {
	ILinkUserID  string         `json:"ilink_user_id,omitempty"`
	ContextToken string         `json:"context_token,omitempty"`
	BaseInfo     wechatBaseInfo `json:"base_info,omitempty"`
}

type wechatGetConfigResponse struct {
	wechatBaseResponse
	TypingTicket string `json:"typing_ticket,omitempty"`
}

type wechatSendMessageResponse struct {
	wechatBaseResponse
}

type wechatTypingTicketState struct {
	Ticket        string    `json:"ticket,omitempty"`
	NextFetchAt   time.Time `json:"next_fetch_at,omitempty"`
	RetryDelayS   int       `json:"retry_delay_s,omitempty"`
	EverSucceeded bool      `json:"ever_succeeded,omitempty"`
}

type wechatSendTypingRequest struct {
	ILinkUserID  string         `json:"ilink_user_id,omitempty"`
	TypingTicket string         `json:"typing_ticket,omitempty"`
	Status       int            `json:"status,omitempty"`
	BaseInfo     wechatBaseInfo `json:"base_info,omitempty"`
}

type wechatQRCodeResponse struct {
	QRCode       string `json:"qrcode,omitempty"`
	QRCodeImgURL string `json:"qrcode_img_content,omitempty"`
}

type wechatQRCodeStatusResponse struct {
	Status      string `json:"status,omitempty"`
	BotToken    string `json:"bot_token,omitempty"`
	ILinkBotID  string `json:"ilink_bot_id,omitempty"`
	BaseURL     string `json:"baseurl,omitempty"`
	ILinkUserID string `json:"ilink_user_id,omitempty"`
}

type wechatPersistedAccountState struct {
	AccountID     string                              `json:"account_id"`
	Token         string                              `json:"token,omitempty"`
	ILinkUserID   string                              `json:"ilink_user_id,omitempty"`
	ILinkBotID    string                              `json:"ilink_bot_id,omitempty"`
	BaseURL       string                              `json:"base_url,omitempty"`
	CDNBaseURL    string                              `json:"cdn_base_url,omitempty"`
	BotType       string                              `json:"bot_type,omitempty"`
	GetUpdatesBuf string                              `json:"get_updates_buf,omitempty"`
	PausedUntil   time.Time                           `json:"paused_until,omitempty"`
	LastError     string                              `json:"last_error,omitempty"`
	LastSeenAt    time.Time                           `json:"last_seen_at,omitempty"`
	LastMessageAt time.Time                           `json:"last_message_at,omitempty"`
	ContextTokens map[string]string                   `json:"context_tokens,omitempty"`
	TypingTickets map[string]*wechatTypingTicketState `json:"typing_tickets,omitempty"`
	UpdatedAt     time.Time                           `json:"updated_at,omitempty"`
}

type wechatLoginSession struct {
	SessionKey string    `json:"session_key"`
	AccountID  string    `json:"account_id"`
	QRCode     string    `json:"qrcode"`
	QRCodeURL  string    `json:"qrcode_url"`
	StartedAt  time.Time `json:"started_at"`
}

type wechatAccountStatus struct {
	AccountID     string    `json:"account_id"`
	Active        bool      `json:"active"`
	LoggedIn      bool      `json:"logged_in"`
	BaseURL       string    `json:"base_url,omitempty"`
	CDNBaseURL    string    `json:"cdn_base_url,omitempty"`
	ILinkUserID   string    `json:"ilink_user_id,omitempty"`
	PausedUntil   time.Time `json:"paused_until,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	LastSeenAt    time.Time `json:"last_seen_at,omitempty"`
	LastMessageAt time.Time `json:"last_message_at,omitempty"`
}

func wechatMediaTypeName(mediaType int) string {
	switch mediaType {
	case wechatMediaTypeImage:
		return "image"
	case wechatMediaTypeVideo:
		return "video"
	case wechatMediaTypeVoice:
		return "voice"
	case wechatMediaTypeFile:
		return "file"
	default:
		return fmt.Sprintf("unknown(%d)", mediaType)
	}
}

func wechatMessageItemTypeName(itemType int) string {
	switch itemType {
	case wechatMessageItemText:
		return "text"
	case wechatMessageItemImage:
		return "image"
	case wechatMessageItemVoice:
		return "voice"
	case wechatMessageItemFile:
		return "file"
	case wechatMessageItemVideo:
		return "video"
	default:
		return fmt.Sprintf("unknown(%d)", itemType)
	}
}
