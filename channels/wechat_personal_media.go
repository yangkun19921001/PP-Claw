package channels

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type wechatUploadedMedia struct {
	DownloadParam      string
	AESKey             []byte
	FileName           string
	FileSize           int
	FileSizeCiphertext int
}

func pkcs7Pad(src []byte, blockSize int) []byte {
	padding := blockSize - (len(src) % blockSize)
	if padding == 0 {
		padding = blockSize
	}
	return append(src, bytes.Repeat([]byte{byte(padding)}, padding)...)
}

func pkcs7Unpad(src []byte, blockSize int) ([]byte, error) {
	if len(src) == 0 || len(src)%blockSize != 0 {
		return nil, fmt.Errorf("invalid pkcs7 payload")
	}
	padding := int(src[len(src)-1])
	if padding <= 0 || padding > blockSize || padding > len(src) {
		return nil, fmt.Errorf("invalid pkcs7 padding")
	}
	for _, b := range src[len(src)-padding:] {
		if int(b) != padding {
			return nil, fmt.Errorf("invalid pkcs7 padding")
		}
	}
	return src[:len(src)-padding], nil
}

func encryptAESECB(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plaintext = pkcs7Pad(plaintext, block.BlockSize())
	dst := make([]byte, len(plaintext))
	for bs := 0; bs < len(plaintext); bs += block.BlockSize() {
		block.Encrypt(dst[bs:bs+block.BlockSize()], plaintext[bs:bs+block.BlockSize()])
	}
	return dst, nil
}

func decryptAESECB(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("invalid ciphertext size")
	}
	dst := make([]byte, len(ciphertext))
	for bs := 0; bs < len(ciphertext); bs += block.BlockSize() {
		block.Decrypt(dst[bs:bs+block.BlockSize()], ciphertext[bs:bs+block.BlockSize()])
	}
	return pkcs7Unpad(dst, block.BlockSize())
}

func buildWechatCDNDownloadURL(cdnBaseURL, encryptedParam string) string {
	return strings.TrimRight(cdnBaseURL, "/") + "/download?encrypted_query_param=" + url.QueryEscape(encryptedParam)
}

func buildWechatCDNUploadURL(cdnBaseURL, uploadParam, fileKey string) string {
	return strings.TrimRight(cdnBaseURL, "/") + "/upload?encrypted_query_param=" + url.QueryEscape(uploadParam) + "&filekey=" + url.QueryEscape(fileKey)
}

func detectExtension(data []byte, fallback string) string {
	if fallback != "" {
		if ext := filepath.Ext(fallback); ext != "" {
			return ext
		}
	}
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "audio/wav":
		return ".wav"
	case "video/mp4":
		return ".mp4"
	case "application/pdf":
		return ".pdf"
	default:
		if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
			return exts[0]
		}
	}
	return ".bin"
}

func parseWechatAESKey(encoded, hexFallback string) ([]byte, error) {
	if hexFallback != "" {
		return hex.DecodeString(hexFallback)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(raw) == 16 {
		return raw, nil
	}
	if len(raw) == 32 {
		return hex.DecodeString(string(raw))
	}
	return nil, fmt.Errorf("unexpected aes key length: %d", len(raw))
}

func (w *WechatPersonalChannel) saveInboundMedia(data []byte, originalName string) (string, error) {
	if err := w.ensureStateDirs(); err != nil {
		return "", err
	}
	name := fmt.Sprintf("wechat_%d%s", time.Now().UnixNano(), detectExtension(data, originalName))
	target := filepath.Join(w.inboundMediaDir(), name)
	if err := os.WriteFile(target, data, 0644); err != nil {
		return "", err
	}
	return target, nil
}

func (w *WechatPersonalChannel) downloadInboundMedia(cdnBaseURL string, media *wechatCDNMedia, aesKeyHex, originalName string) (string, error) {
	if media == nil || media.EncryptQueryParam == "" {
		return "", nil
	}
	req, err := http.NewRequest(http.MethodGet, buildWechatCDNDownloadURL(cdnBaseURL, media.EncryptQueryParam), nil)
	if err != nil {
		return "", err
	}
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("wechat cdn download %d: %s", resp.StatusCode, string(raw))
	}
	encrypted, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if media.AESKey == "" && aesKeyHex == "" {
		return w.saveInboundMedia(encrypted, originalName)
	}
	key, err := parseWechatAESKey(media.AESKey, aesKeyHex)
	if err != nil {
		return "", err
	}
	plain, err := decryptAESECB(encrypted, key)
	if err != nil {
		return "", err
	}
	return w.saveInboundMedia(plain, originalName)
}

func uploadBufferToCDN(ctx context.Context, client *http.Client, cdnBaseURL, uploadParam, fileKey string, aesKey, plain []byte) (string, int, error) {
	encrypted, err := encryptAESECB(plain, aesKey)
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildWechatCDNUploadURL(cdnBaseURL, uploadParam, fileKey), bytes.NewReader(encrypted))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("wechat cdn upload %d: %s", resp.StatusCode, string(raw))
	}
	downloadParam := resp.Header.Get("x-encrypted-param")
	if downloadParam == "" {
		return "", 0, fmt.Errorf("wechat cdn upload missing x-encrypted-param")
	}
	return downloadParam, len(encrypted), nil
}

func (w *WechatPersonalChannel) uploadOutboundMedia(ctx context.Context, account *wechatAccountRuntime, filePath, toUserID string) (*wechatUploadedMedia, int, error) {
	plain, err := os.ReadFile(filePath)
	if err != nil {
		return nil, 0, err
	}
	rawMD5 := md5.Sum(plain)
	aesKey := make([]byte, 16)
	if _, err := rand.Read(aesKey); err != nil {
		return nil, 0, err
	}
	fileKeyBytes := make([]byte, 16)
	if _, err := rand.Read(fileKeyBytes); err != nil {
		return nil, 0, err
	}
	fileKey := hex.EncodeToString(fileKeyBytes)
	ext := strings.ToLower(filepath.Ext(filePath))
	mediaType := wechatMediaTypeFile
	if strings.HasPrefix(mime.TypeByExtension(ext), "image/") {
		mediaType = wechatMediaTypeImage
	} else if strings.HasPrefix(mime.TypeByExtension(ext), "video/") {
		mediaType = wechatMediaTypeVideo
	}
	req := &wechatGetUploadURLRequest{
		FileKey:     fileKey,
		MediaType:   mediaType,
		ToUserID:    toUserID,
		RawSize:     len(plain),
		RawFileMD5:  hex.EncodeToString(rawMD5[:]),
		FileSize:    len(pkcs7Pad(append([]byte(nil), plain...), aes.BlockSize)),
		NoNeedThumb: true,
		AESKey:      hex.EncodeToString(aesKey),
		BaseInfo:    wechatBaseInfo{ChannelVersion: wechatAPIChannelVersion},
	}
	resp, err := w.getUploadURL(ctx, account, req)
	if err != nil {
		return nil, 0, err
	}
	if resp.UploadParam == "" {
		return nil, 0, fmt.Errorf("wechat getuploadurl returned empty upload_param")
	}
	downloadParam, ciphertextSize, err := uploadBufferToCDN(ctx, w.httpClient, account.cdnBaseURL, resp.UploadParam, fileKey, aesKey, plain)
	if err != nil {
		return nil, 0, err
	}
	return &wechatUploadedMedia{
		DownloadParam:      downloadParam,
		AESKey:             aesKey,
		FileName:           filepath.Base(filePath),
		FileSize:           len(plain),
		FileSizeCiphertext: ciphertextSize,
	}, mediaType, nil
}

func buildTextItem(text string) *wechatMessageItem {
	return &wechatMessageItem{
		Type:     wechatMessageItemText,
		TextItem: &wechatTextItem{Text: text},
	}
}

func buildMediaItem(mediaType int, uploaded *wechatUploadedMedia) *wechatMessageItem {
	cdnMedia := &wechatCDNMedia{
		EncryptQueryParam: uploaded.DownloadParam,
		AESKey:            base64.StdEncoding.EncodeToString(uploaded.AESKey),
		EncryptType:       1,
	}
	switch mediaType {
	case wechatMediaTypeImage:
		return &wechatMessageItem{
			Type: wechatMessageItemImage,
			ImageItem: &wechatImageItem{
				Media:   cdnMedia,
				MidSize: uploaded.FileSizeCiphertext,
			},
		}
	case wechatMediaTypeVideo:
		return &wechatMessageItem{
			Type: wechatMessageItemVideo,
			VideoItem: &wechatVideoItem{
				Media:     cdnMedia,
				VideoSize: uploaded.FileSizeCiphertext,
			},
		}
	default:
		return &wechatMessageItem{
			Type: wechatMessageItemFile,
			FileItem: &wechatFileItem{
				Media:    cdnMedia,
				FileName: uploaded.FileName,
				Len:      fmt.Sprintf("%d", uploaded.FileSize),
			},
		}
	}
}

func (w *WechatPersonalChannel) sendOutboundItem(ctx context.Context, account *wechatAccountRuntime, toUserID, contextToken string, item *wechatMessageItem) error {
	clientIDBytes := make([]byte, 8)
	if _, err := rand.Read(clientIDBytes); err != nil {
		return err
	}
	req := &wechatSendMessageRequest{
		Msg: &wechatMessage{
			ToUserID:     toUserID,
			ClientID:     hex.EncodeToString(clientIDBytes),
			MessageType:  wechatMessageTypeBot,
			ItemList:     []*wechatMessageItem{item},
			ContextToken: contextToken,
		},
		BaseInfo: wechatBaseInfo{ChannelVersion: wechatAPIChannelVersion},
	}
	return w.sendMessage(ctx, account, req)
}
