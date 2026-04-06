package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangkun19921001/PP-Claw/agent/tools"
	"github.com/yangkun19921001/PP-Claw/bus"
	"go.uber.org/zap"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(status int, body string, headers map[string]string) *http.Response {
	respHeaders := make(http.Header)
	for key, value := range headers {
		respHeaders.Set(key, value)
	}
	return &http.Response{
		StatusCode: status,
		Header:     respHeaders,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func bytesResponse(status int, body []byte, headers map[string]string) *http.Response {
	respHeaders := make(http.Header)
	for key, value := range headers {
		respHeaders.Set(key, value)
	}
	return &http.Response{
		StatusCode: status,
		Header:     respHeaders,
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
}

func newTestWechatChannel(t *testing.T) (*WechatPersonalChannel, *wechatAccountRuntime, *bus.MessageBus) {
	t.Helper()
	msgBus := bus.NewMessageBus()
	ch := &WechatPersonalChannel{
		BaseChannel: BaseChannel{
			ChannelName: "wechat_personal",
			Bus:         msgBus,
			Logger:      zap.NewNop(),
		},
		httpClient:          &http.Client{},
		accounts:            make(map[string]*wechatAccountRuntime),
		logins:              make(map[string]*wechatLoginSession),
		stateDir:            t.TempDir(),
		baseURL:             "https://api.test",
		cdnBaseURL:          "https://cdn.test",
		botType:             "3",
		pollTimeoutMS:       35000,
		requestTimeoutMS:    15000,
		configTimeoutMS:     1000,
		rootCtx:             context.Background(),
		sessionPauseMinutes: 60,
	}
	account := ch.ensureAccount("wx1")
	account.baseURL = "https://api.test"
	account.cdnBaseURL = "https://cdn.test"
	account.token = "token-1"
	return ch, account, msgBus
}

func TestWechatStatePersistsContextTokensAndTypingTickets(t *testing.T) {
	ch, account, _ := newTestWechatChannel(t)
	account.contextTokens["wx-user"] = "ctx-1"
	account.typingTickets["wx-user"] = &wechatTypingTicketState{
		Ticket:        "ticket-1",
		NextFetchAt:   time.Unix(123, 0).UTC(),
		RetryDelayS:   8,
		EverSucceeded: true,
	}
	account.getUpdatesBuf = "cursor-1"

	if err := ch.saveAccountState(account); err != nil {
		t.Fatalf("saveAccountState() error = %v", err)
	}

	ch2, _, _ := newTestWechatChannel(t)
	ch2.stateDir = ch.stateDir
	if err := ch2.loadAccountsFromState(); err != nil {
		t.Fatalf("loadAccountsFromState() error = %v", err)
	}
	restored := ch2.lookupAccount("wx1")
	if restored == nil {
		t.Fatalf("restored account not found")
	}
	if got := restored.contextTokens["wx-user"]; got != "ctx-1" {
		t.Fatalf("context token = %q, want ctx-1", got)
	}
	if got := restored.typingTickets["wx-user"]; got == nil || got.Ticket != "ticket-1" || got.RetryDelayS != 8 {
		t.Fatalf("typing ticket state = %+v, want ticket-1/backoff 8", got)
	}
}

func TestWechatProcessInboundDeduplicatesMessages(t *testing.T) {
	ch, account, msgBus := newTestWechatChannel(t)
	msg := &wechatMessage{
		MessageID:   1001,
		FromUserID:  "wx-user",
		MessageType: wechatMessageTypeUser,
		ItemList: []*wechatMessageItem{
			buildTextItem("hello"),
		},
	}

	if err := ch.processInboundMessage(account, msg); err != nil {
		t.Fatalf("processInboundMessage() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	first, err := msgBus.ConsumeInbound(ctx)
	if err != nil {
		t.Fatalf("ConsumeInbound() error = %v", err)
	}
	if first.Content != "hello" {
		t.Fatalf("first content = %q, want hello", first.Content)
	}

	if err := ch.processInboundMessage(account, msg); err != nil {
		t.Fatalf("processInboundMessage() second error = %v", err)
	}
	if got := msgBus.InboundSize(); got != 0 {
		t.Fatalf("inbound size after duplicate = %d, want 0", got)
	}
}

func TestWechatProcessInboundFallsBackToReferencedMedia(t *testing.T) {
	ch, account, msgBus := newTestWechatChannel(t)
	ch.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "cdn.test" {
			t.Fatalf("unexpected host %s", req.URL.Host)
		}
		return bytesResponse(http.StatusOK, []byte("image-bytes"), nil), nil
	})

	msg := &wechatMessage{
		MessageID:   1002,
		FromUserID:  "wx-user",
		MessageType: wechatMessageTypeUser,
		ItemList: []*wechatMessageItem{
			{
				Type:     wechatMessageItemText,
				TextItem: &wechatTextItem{Text: "reply to image"},
				RefMsg: &wechatRefMessage{
					MessageItem: &wechatMessageItem{
						Type: wechatMessageItemImage,
						ImageItem: &wechatImageItem{
							Media: &wechatCDNMedia{EncryptQueryParam: "ref-enc"},
						},
					},
				},
			},
		},
	}

	if err := ch.processInboundMessage(account, msg); err != nil {
		t.Fatalf("processInboundMessage() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	inbound, err := msgBus.ConsumeInbound(ctx)
	if err != nil {
		t.Fatalf("ConsumeInbound() error = %v", err)
	}
	if len(inbound.Media) != 1 {
		t.Fatalf("media len = %d, want 1", len(inbound.Media))
	}
	if !strings.Contains(inbound.Content, "reply to image") || !strings.Contains(inbound.Content, "[image]") {
		t.Fatalf("content = %q, want quoted text and image marker", inbound.Content)
	}
}

func TestWechatProcessInboundDoesNotFallbackWhenTopLevelMediaExists(t *testing.T) {
	ch, account, msgBus := newTestWechatChannel(t)
	ch.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.RawQuery {
		case "encrypted_query_param=top-enc":
			return jsonResponse(http.StatusInternalServerError, "boom", nil), nil
		case "encrypted_query_param=ref-enc":
			return bytesResponse(http.StatusOK, []byte("ref-image"), nil), nil
		default:
			t.Fatalf("unexpected query %s", req.URL.RawQuery)
			return nil, nil
		}
	})

	msg := &wechatMessage{
		MessageID:   1003,
		FromUserID:  "wx-user",
		MessageType: wechatMessageTypeUser,
		ItemList: []*wechatMessageItem{
			{
				Type: wechatMessageItemImage,
				ImageItem: &wechatImageItem{
					Media: &wechatCDNMedia{EncryptQueryParam: "top-enc"},
				},
			},
			{
				Type:     wechatMessageItemText,
				TextItem: &wechatTextItem{Text: "quoted has media"},
				RefMsg: &wechatRefMessage{
					MessageItem: &wechatMessageItem{
						Type: wechatMessageItemImage,
						ImageItem: &wechatImageItem{
							Media: &wechatCDNMedia{EncryptQueryParam: "ref-enc"},
						},
					},
				},
			},
		},
	}

	if err := ch.processInboundMessage(account, msg); err != nil {
		t.Fatalf("processInboundMessage() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	inbound, err := msgBus.ConsumeInbound(ctx)
	if err != nil {
		t.Fatalf("ConsumeInbound() error = %v", err)
	}
	if len(inbound.Media) != 0 {
		t.Fatalf("media len = %d, want 0", len(inbound.Media))
	}
	if strings.Contains(inbound.Content, "ref-image") {
		t.Fatalf("content should not include referenced fallback media path: %q", inbound.Content)
	}
	if !strings.Contains(inbound.Content, "[image]") {
		t.Fatalf("content should keep top-level placeholder: %q", inbound.Content)
	}
}

func TestWechatDownloadInboundMediaUsesFullURLFallback(t *testing.T) {
	ch, _, _ := newTestWechatChannel(t)
	var mu sync.Mutex
	var requests []string
	ch.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, req.URL.String())
		mu.Unlock()
		if strings.Contains(req.URL.String(), "download/full") {
			return jsonResponse(http.StatusInternalServerError, "retry", nil), nil
		}
		return bytesResponse(http.StatusOK, []byte("raw-image"), nil), nil
	})

	path, err := ch.downloadInboundMedia("https://cdn.test", &wechatCDNMedia{
		FullURL:           "https://cdn.test/download/full",
		EncryptQueryParam: "fallback-enc",
	}, "", "image.jpg", "image")
	if err != nil {
		t.Fatalf("downloadInboundMedia() error = %v", err)
	}
	if path == "" {
		t.Fatalf("downloadInboundMedia() returned empty path")
	}
	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}
}

func TestWechatUploadOutboundVoiceUsesUploadFullURL(t *testing.T) {
	ch, account, _ := newTestWechatChannel(t)
	voicePath := filepath.Join(t.TempDir(), "voice.mp3")
	if err := os.WriteFile(voicePath, []byte("voice-bytes"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	oldTranscode := wechatTranscodeAudioToAMR
	wechatTranscodeAudioToAMR = func(string) (string, error) {
		convertedPath := filepath.Join(t.TempDir(), "voice.amr")
		if err := os.WriteFile(convertedPath, []byte("voice-amr"), 0644); err != nil {
			t.Fatalf("WriteFile(converted) error = %v", err)
		}
		return convertedPath, nil
	}
	defer func() { wechatTranscodeAudioToAMR = oldTranscode }()

	var uploadURL string
	ch.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "api.test" && req.URL.Path == "/ilink/bot/getuploadurl":
			return jsonResponse(http.StatusOK, `{"ret":0,"upload_full_url":"https://upload-full.example.test/voice?foo=bar"}`, nil), nil
		case req.URL.Host == "upload-full.example.test":
			uploadURL = req.URL.String()
			return jsonResponse(http.StatusOK, "", map[string]string{"x-encrypted-param": "voice-dl-param"}), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})

	uploaded, mediaType, err := ch.uploadOutboundMedia(context.Background(), account, voicePath, "wx-user")
	if err != nil {
		t.Fatalf("uploadOutboundMedia() error = %v", err)
	}
	if mediaType != wechatMediaTypeVoice {
		t.Fatalf("mediaType = %d, want %d", mediaType, wechatMediaTypeVoice)
	}
	if uploadURL != "https://upload-full.example.test/voice?foo=bar" {
		t.Fatalf("upload URL = %q, want upload_full_url", uploadURL)
	}
	item := buildMediaItem(mediaType, uploaded)
	if item.Type != wechatMessageItemVoice || item.VoiceItem == nil || item.FileItem != nil {
		t.Fatalf("voice item not built correctly: %+v", item)
	}
	if item.VoiceItem.EncodeType != 5 {
		t.Fatalf("voice encode_type = %d, want 5", item.VoiceItem.EncodeType)
	}
	if item.VoiceItem.SampleRate != 8000 {
		t.Fatalf("voice sample_rate = %d, want 8000", item.VoiceItem.SampleRate)
	}
}

func TestWechatUploadOutboundAudioFallsBackToFileWhenVoiceTranscodeFails(t *testing.T) {
	ch, account, _ := newTestWechatChannel(t)
	audioPath := filepath.Join(t.TempDir(), "voice.mp3")
	if err := os.WriteFile(audioPath, []byte("voice-bytes"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	oldTranscode := wechatTranscodeAudioToAMR
	wechatTranscodeAudioToAMR = func(string) (string, error) {
		return "", os.ErrNotExist
	}
	defer func() { wechatTranscodeAudioToAMR = oldTranscode }()

	ch.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "api.test" && req.URL.Path == "/ilink/bot/getuploadurl":
			return jsonResponse(http.StatusOK, `{"ret":0,"upload_full_url":"https://upload-full.example.test/file?foo=bar"}`, nil), nil
		case req.URL.Host == "upload-full.example.test":
			return jsonResponse(http.StatusOK, "", map[string]string{"x-encrypted-param": "file-dl-param"}), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})

	uploaded, mediaType, err := ch.uploadOutboundMedia(context.Background(), account, audioPath, "wx-user")
	if err != nil {
		t.Fatalf("uploadOutboundMedia() error = %v", err)
	}
	if mediaType != wechatMediaTypeFile {
		t.Fatalf("mediaType = %d, want %d", mediaType, wechatMediaTypeFile)
	}
	item := buildMediaItem(mediaType, uploaded)
	if item.Type != wechatMessageItemFile || item.FileItem == nil {
		t.Fatalf("file item not built correctly: %+v", item)
	}
	if item.FileItem.FileName != "voice.mp3" {
		t.Fatalf("file name = %q, want voice.mp3", item.FileItem.FileName)
	}
}

func TestWechatSendMessageReturnsBusinessError(t *testing.T) {
	ch, account, _ := newTestWechatChannel(t)
	ch.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"errcode":123,"errmsg":"boom"}`, nil), nil
	})

	err := ch.sendOutboundItem(context.Background(), account, "wx-user", "ctx-1", buildTextItem("hello"))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("sendOutboundItem() error = %v, want business error containing boom", err)
	}
}

func TestWechatSendOutboundItemIncludesFinishMessageState(t *testing.T) {
	ch, account, _ := newTestWechatChannel(t)
	type sendBody struct {
		Msg wechatMessage `json:"msg"`
	}
	var captured sendBody
	ch.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/ilink/bot/sendmessage" {
			t.Fatalf("unexpected request path %s", req.URL.Path)
		}
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if err := json.Unmarshal(raw, &captured); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		return jsonResponse(http.StatusOK, `{"ret":0}`, nil), nil
	})

	if err := ch.sendOutboundItem(context.Background(), account, "wx-user", "ctx-1", buildTextItem("hello")); err != nil {
		t.Fatalf("sendOutboundItem() error = %v", err)
	}
	if captured.Msg.MessageState != wechatMessageStateFinish {
		t.Fatalf("message_state = %d, want %d", captured.Msg.MessageState, wechatMessageStateFinish)
	}
	if captured.Msg.MessageType != wechatMessageTypeBot {
		t.Fatalf("message_type = %d, want %d", captured.Msg.MessageType, wechatMessageTypeBot)
	}
	if captured.Msg.ToUserID != "wx-user" {
		t.Fatalf("to_user_id = %q, want wx-user", captured.Msg.ToUserID)
	}
}

func TestWechatSendSplitsLongTextAndKeepsTypingAlive(t *testing.T) {
	ch, account, _ := newTestWechatChannel(t)
	account.contextTokens["wx-user"] = "ctx-typing"
	oldInterval := wechatTypingKeepaliveInterval
	wechatTypingKeepaliveInterval = 5 * time.Millisecond
	defer func() { wechatTypingKeepaliveInterval = oldInterval }()

	type typingBody struct {
		ILinkUserID  string `json:"ilink_user_id"`
		TypingTicket string `json:"typing_ticket"`
		Status       int    `json:"status"`
	}

	var mu sync.Mutex
	sendMessageCalls := 0
	typingStatuses := []int{}
	typingUsers := []string{}

	ch.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "api.test" && req.URL.Path == "/ilink/bot/getconfig":
			return jsonResponse(http.StatusOK, `{"ret":0,"typing_ticket":"ticket-1"}`, nil), nil
		case req.URL.Host == "api.test" && req.URL.Path == "/ilink/bot/sendtyping":
			var body typingBody
			raw, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			typingStatuses = append(typingStatuses, body.Status)
			typingUsers = append(typingUsers, body.ILinkUserID)
			mu.Unlock()
			return jsonResponse(http.StatusOK, `{"ret":0}`, nil), nil
		case req.URL.Host == "api.test" && req.URL.Path == "/ilink/bot/sendmessage":
			time.Sleep(12 * time.Millisecond)
			mu.Lock()
			sendMessageCalls++
			mu.Unlock()
			return jsonResponse(http.StatusOK, `{"ret":0}`, nil), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})

	longText := strings.Repeat("a", 9001)
	msg := bus.NewOutboundMessage("wechat_personal", "wx-user", longText)
	msg.AccountID = account.id
	if err := ch.Send(msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if sendMessageCalls != 3 {
		t.Fatalf("sendMessageCalls = %d, want 3", sendMessageCalls)
	}
	if len(typingStatuses) < 2 {
		t.Fatalf("typing status calls = %v, want at least start and cancel", typingStatuses)
	}
	if typingUsers[0] != "wx-user" {
		t.Fatalf("typing user = %q, want wx-user", typingUsers[0])
	}
	if typingStatuses[0] != wechatTypingStatusTyping {
		t.Fatalf("first typing status = %d, want %d", typingStatuses[0], wechatTypingStatusTyping)
	}
	if typingStatuses[len(typingStatuses)-1] != wechatTypingStatusCancel {
		t.Fatalf("last typing status = %d, want %d", typingStatuses[len(typingStatuses)-1], wechatTypingStatusCancel)
	}
	startCount := 0
	for _, status := range typingStatuses {
		if status == wechatTypingStatusTyping {
			startCount++
		}
	}
	if startCount < 2 {
		t.Fatalf("typing keepalive start count = %d, want >= 2", startCount)
	}
}

func TestMessageToolNormalizesWechatAliasToWechatPersonal(t *testing.T) {
	msgBus := bus.NewMessageBus()
	tool := &tools.MessageTool{
		SendCallback: msgBus.PublishOutbound,
	}
	tool.SetContextWithAccount("wechat_personal", "DevYK", "wx-user")

	result, err := tool.Execute(context.Background(), map[string]any{
		"content": "hello",
		"channel": "wechat",
		"chat_id": "wx-user",
		"media":   []any{"/tmp/test.mp3"},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result, "wechat_personal:wx-user") {
		t.Fatalf("result = %q, want normalized wechat_personal target", result)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	outbound, err := msgBus.ConsumeOutbound(ctx)
	if err != nil {
		t.Fatalf("ConsumeOutbound() error = %v", err)
	}
	if outbound.Channel != "wechat_personal" {
		t.Fatalf("outbound channel = %q, want wechat_personal", outbound.Channel)
	}
	if outbound.AccountID != "DevYK" {
		t.Fatalf("outbound account_id = %q, want DevYK", outbound.AccountID)
	}
}
