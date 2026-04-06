package channels

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

var (
	wechatImageExts = map[string]struct{}{
		".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".bmp": {}, ".webp": {}, ".tiff": {}, ".ico": {}, ".svg": {},
	}
	wechatVideoExts = map[string]struct{}{
		".mp4": {}, ".avi": {}, ".mov": {}, ".mkv": {}, ".webm": {}, ".flv": {},
	}
	wechatVoiceExts = map[string]struct{}{
		".mp3": {}, ".wav": {}, ".amr": {}, ".silk": {}, ".ogg": {}, ".m4a": {}, ".aac": {}, ".flac": {},
	}
	wechatVoiceCompatibleExts = map[string]struct{}{
		".amr": {}, ".silk": {},
	}
	wechatTranscodeAudioToSILK = transcodeWechatAudioToSILK
	wechatTranscodeAudioToAMR  = transcodeWechatAudioToAMR
)

type wechatUploadedMedia struct {
	DownloadParam      string
	AESKey             []byte
	FileName           string
	FileSize           int
	FileSizeCiphertext int
	VoiceEncodeType    int
	VoiceBitsPerSample int
	VoiceSampleRate    int
	VoicePlaytimeMS    int
}

type wechatResolvedMedia struct {
	Path               string
	MediaType          int
	Cleanup            func()
	FallbackReason     string
	VoiceEncodeType    int
	VoiceBitsPerSample int
	VoiceSampleRate    int
	VoicePlaytimeMS    int
}

func isWechatAudioPath(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	if _, ok := wechatVoiceExts[ext]; ok {
		return true
	}
	return strings.HasPrefix(mime.TypeByExtension(ext), "audio/")
}

func isWechatVoiceCompatiblePath(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	_, ok := wechatVoiceCompatibleExts[ext]
	return ok
}

func wechatVoiceEncodeTypeByExt(ext string) int {
	switch strings.ToLower(ext) {
	case ".amr":
		return 5
	case ".silk":
		return 6
	case ".mp3":
		return 7
	case ".ogg":
		return 8
	default:
		return 0
	}
}

func trimWechatCommandOutput(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= 400 {
		return raw
	}
	return raw[:400] + "..."
}

func transcodeWechatAudioToAMR(filePath string) (string, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not available: %w", err)
	}
	target := filepath.Join(os.TempDir(), fmt.Sprintf("wechat_voice_%d.amr", time.Now().UnixNano()))
	cmd := exec.Command(
		ffmpegPath,
		"-y",
		"-i", filePath,
		"-vn",
		"-sn",
		"-dn",
		"-ar", "8000",
		"-ac", "1",
		"-c:a", "libopencore_amrnb",
		"-b:a", "12.2k",
		target,
	)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(target)
		details := trimWechatCommandOutput(stderr.String())
		if details != "" {
			return "", fmt.Errorf("ffmpeg amr transcode failed: %w: %s", err, details)
		}
		return "", fmt.Errorf("ffmpeg amr transcode failed: %w", err)
	}
	if _, err := os.Stat(target); err != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("ffmpeg amr transcode missing output: %w", err)
	}
	return target, nil
}

func transcodeWechatAudioToSILK(filePath string) (string, int, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", 0, fmt.Errorf("ffmpeg not available: %w", err)
	}
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return "", 0, fmt.Errorf("node not available: %w", err)
	}

	pcmPath := filepath.Join(os.TempDir(), fmt.Sprintf("wechat_voice_%d.pcm", time.Now().UnixNano()))
	silkPath := filepath.Join(os.TempDir(), fmt.Sprintf("wechat_voice_%d.silk", time.Now().UnixNano()))
	defer func() { _ = os.Remove(pcmPath) }()

	ffmpegCmd := exec.Command(
		ffmpegPath,
		"-y",
		"-i", filePath,
		"-vn",
		"-sn",
		"-dn",
		"-f", "s16le",
		"-ar", "24000",
		"-ac", "1",
		pcmPath,
	)
	var ffmpegStderr bytes.Buffer
	ffmpegCmd.Stdout = io.Discard
	ffmpegCmd.Stderr = &ffmpegStderr
	if err := ffmpegCmd.Run(); err != nil {
		_ = os.Remove(silkPath)
		return "", 0, fmt.Errorf("ffmpeg silk pcm transcode failed: %w: %s", err, trimWechatCommandOutput(ffmpegStderr.String()))
	}

	const silkEncodeScript = `
const fs = require('fs');
let mod;
try {
  mod = require('silk-wasm');
} catch (_) {
  mod = require('/tmp/node_modules/silk-wasm');
}
(async () => {
  const input = process.argv[1];
  const output = process.argv[2];
  const pcm = fs.readFileSync(input);
  const result = await mod.encode(pcm, 24000);
  fs.writeFileSync(output, Buffer.from(result.data));
  process.stdout.write(JSON.stringify({ duration: result.duration, bytes: result.data.length }));
})().catch((err) => {
  console.error(String(err && err.stack ? err.stack : err));
  process.exit(1);
});
`
	nodeCmd := exec.Command(nodePath, "-e", silkEncodeScript, pcmPath, silkPath)
	nodeCmd.Env = append(os.Environ(), "NODE_PATH=/tmp/node_modules")
	var nodeStdout bytes.Buffer
	var nodeStderr bytes.Buffer
	nodeCmd.Stdout = &nodeStdout
	nodeCmd.Stderr = &nodeStderr
	if err := nodeCmd.Run(); err != nil {
		_ = os.Remove(silkPath)
		return "", 0, fmt.Errorf("silk-wasm encode failed: %w: %s", err, trimWechatCommandOutput(nodeStderr.String()))
	}
	var result struct {
		Duration int `json:"duration"`
		Bytes    int `json:"bytes"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(nodeStdout.Bytes()), &result); err != nil {
		_ = os.Remove(silkPath)
		return "", 0, fmt.Errorf("silk-wasm output parse failed: %w: %s", err, trimWechatCommandOutput(nodeStdout.String()))
	}
	if _, err := os.Stat(silkPath); err != nil {
		_ = os.Remove(silkPath)
		return "", 0, fmt.Errorf("silk-wasm output missing: %w", err)
	}
	return silkPath, result.Duration, nil
}

func parseWechatProbeInt(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "N/A" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}

func parseWechatProbeDurationMS(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "N/A" {
		return 0
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	if value <= 0 {
		return 0
	}
	return int(math.Round(value * 1000))
}

func probeWechatVoiceMetadata(filePath string) (sampleRate, bitsPerSample, playtimeMS int) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0, 0, 0
	}
	cmd := exec.Command(
		ffprobePath,
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=sample_rate,bits_per_sample:format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, 0, 0
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) > 0 {
		sampleRate = parseWechatProbeInt(lines[0])
	}
	if len(lines) > 1 {
		bitsPerSample = parseWechatProbeInt(lines[1])
	}
	if len(lines) > 2 {
		playtimeMS = parseWechatProbeDurationMS(lines[2])
	}
	return sampleRate, bitsPerSample, playtimeMS
}

func resolveWechatVoiceMetadata(filePath string) (encodeType, sampleRate, bitsPerSample, playtimeMS int) {
	ext := strings.ToLower(filepath.Ext(filePath))
	encodeType = wechatVoiceEncodeTypeByExt(ext)
	sampleRate, bitsPerSample, playtimeMS = probeWechatVoiceMetadata(filePath)
	switch ext {
	case ".amr":
		if sampleRate == 0 {
			sampleRate = 8000
		}
	case ".silk":
		if sampleRate == 0 {
			sampleRate = 24000
		}
		if bitsPerSample == 0 {
			bitsPerSample = 16
		}
	}
	return encodeType, sampleRate, bitsPerSample, playtimeMS
}

func (w *WechatPersonalChannel) resolveOutboundMediaPath(filePath string) *wechatResolvedMedia {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch {
	case func() bool {
		_, ok := wechatImageExts[ext]
		return ok
	}() || strings.HasPrefix(mime.TypeByExtension(ext), "image/"):
		return &wechatResolvedMedia{Path: filePath, MediaType: wechatMediaTypeImage}
	case func() bool {
		_, ok := wechatVideoExts[ext]
		return ok
	}() || strings.HasPrefix(mime.TypeByExtension(ext), "video/"):
		return &wechatResolvedMedia{Path: filePath, MediaType: wechatMediaTypeVideo}
	case !isWechatAudioPath(filePath):
		return &wechatResolvedMedia{Path: filePath, MediaType: wechatMediaTypeFile}
	case isWechatVoiceCompatiblePath(filePath):
		encodeType, sampleRate, bitsPerSample, playtimeMS := resolveWechatVoiceMetadata(filePath)
		return &wechatResolvedMedia{
			Path:               filePath,
			MediaType:          wechatMediaTypeVoice,
			VoiceEncodeType:    encodeType,
			VoiceBitsPerSample: bitsPerSample,
			VoiceSampleRate:    sampleRate,
			VoicePlaytimeMS:    playtimeMS,
		}
	}

	if convertedPath, durationMS, err := wechatTranscodeAudioToSILK(filePath); err == nil {
		return &wechatResolvedMedia{
			Path:               convertedPath,
			MediaType:          wechatMediaTypeVoice,
			Cleanup:            func() { _ = os.Remove(convertedPath) },
			VoiceEncodeType:    6,
			VoiceBitsPerSample: 16,
			VoiceSampleRate:    24000,
			VoicePlaytimeMS:    durationMS,
		}
	} else if convertedPath, err := wechatTranscodeAudioToAMR(filePath); err == nil {
		encodeType, sampleRate, bitsPerSample, playtimeMS := resolveWechatVoiceMetadata(convertedPath)
		return &wechatResolvedMedia{
			Path:               convertedPath,
			MediaType:          wechatMediaTypeVoice,
			Cleanup:            func() { _ = os.Remove(convertedPath) },
			FallbackReason:     "silk unavailable; used amr fallback",
			VoiceEncodeType:    encodeType,
			VoiceBitsPerSample: bitsPerSample,
			VoiceSampleRate:    sampleRate,
			VoicePlaytimeMS:    playtimeMS,
		}
	} else {
		return &wechatResolvedMedia{
			Path:           filePath,
			MediaType:      wechatMediaTypeFile,
			FallbackReason: fmt.Sprintf("silk failed: %v; amr failed: %v", err, err),
		}
	}
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

func isRetryableMediaDownloadStatus(status int) bool {
	return status >= 500
}

func (w *WechatPersonalChannel) downloadInboundMedia(cdnBaseURL string, media *wechatCDNMedia, aesKeyHex, originalName, mediaType string) (string, error) {
	if media == nil || (strings.TrimSpace(media.EncryptQueryParam) == "" && strings.TrimSpace(media.FullURL) == "") {
		return "", nil
	}
	if mediaType != "image" && strings.TrimSpace(media.AESKey) == "" && strings.TrimSpace(aesKeyHex) == "" {
		return "", nil
	}

	downloadURLs := make([]string, 0, 2)
	if fullURL := strings.TrimSpace(media.FullURL); fullURL != "" {
		downloadURLs = append(downloadURLs, fullURL)
	}
	if encryptedParam := strings.TrimSpace(media.EncryptQueryParam); encryptedParam != "" {
		fallback := buildWechatCDNDownloadURL(cdnBaseURL, encryptedParam)
		if len(downloadURLs) == 0 || downloadURLs[len(downloadURLs)-1] != fallback {
			downloadURLs = append(downloadURLs, fallback)
		}
	}

	var encrypted []byte
	var lastErr error
	for idx, targetURL := range downloadURLs {
		req, err := http.NewRequest(http.MethodGet, targetURL, nil)
		if err != nil {
			return "", err
		}
		resp, err := w.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if idx == 0 && len(downloadURLs) > 1 {
				continue
			}
			return "", err
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return "", readErr
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("wechat cdn download %d: %s", resp.StatusCode, string(raw))
			if idx == 0 && len(downloadURLs) > 1 && isRetryableMediaDownloadStatus(resp.StatusCode) {
				continue
			}
			return "", lastErr
		}
		encrypted = raw
		lastErr = nil
		break
	}
	if lastErr != nil {
		return "", lastErr
	}
	if strings.TrimSpace(media.AESKey) == "" && strings.TrimSpace(aesKeyHex) == "" {
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

func uploadBufferToCDN(ctx context.Context, client *http.Client, cdnBaseURL, uploadFullURL, uploadParam, fileKey string, aesKey, plain []byte) (string, int, error) {
	encrypted, err := encryptAESECB(plain, aesKey)
	if err != nil {
		return "", 0, err
	}
	targetURL := strings.TrimSpace(uploadFullURL)
	if targetURL == "" {
		targetURL = buildWechatCDNUploadURL(cdnBaseURL, uploadParam, fileKey)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(encrypted))
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
	resolved := w.resolveOutboundMediaPath(filePath)
	if resolved == nil {
		return nil, 0, fmt.Errorf("wechat resolve outbound media failed")
	}
	if resolved.Cleanup != nil {
		defer resolved.Cleanup()
	}
	if resolved.FallbackReason != "" {
		logFields := []zap.Field{
			zap.String("account_id", account.id),
			zap.String("to_user_id", toUserID),
			zap.String("file_path", filePath),
			zap.String("reason", resolved.FallbackReason),
		}
		if resolved.MediaType == wechatMediaTypeFile {
			w.Logger.Warn("微信语音格式不兼容，降级为普通文件", logFields...)
		} else {
			w.Logger.Warn("微信语音未使用首选格式，采用兼容回退格式", logFields...)
		}
	} else if resolved.Path != filePath && resolved.MediaType == wechatMediaTypeVoice {
		w.Logger.Info("微信语音已转码为兼容格式",
			zap.String("account_id", account.id),
			zap.String("to_user_id", toUserID),
			zap.String("source_path", filePath),
			zap.String("converted_path", resolved.Path),
			zap.String("media_type", wechatMediaTypeName(resolved.MediaType)),
		)
	}

	plain, err := os.ReadFile(resolved.Path)
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
	ext := strings.ToLower(filepath.Ext(resolved.Path))
	mediaType := resolved.MediaType
	w.Logger.Info("微信出站媒体分类",
		zap.String("account_id", account.id),
		zap.String("to_user_id", toUserID),
		zap.String("file_path", resolved.Path),
		zap.String("source_path", filePath),
		zap.String("file_ext", ext),
		zap.String("media_type", wechatMediaTypeName(mediaType)),
		zap.Int("raw_size", len(plain)),
	)
	voiceEncodeType := resolved.VoiceEncodeType
	voiceBitsPerSample := resolved.VoiceBitsPerSample
	voiceSampleRate := resolved.VoiceSampleRate
	voicePlaytimeMS := resolved.VoicePlaytimeMS
	if mediaType == wechatMediaTypeVoice {
		if voiceEncodeType == 0 && voiceSampleRate == 0 && voiceBitsPerSample == 0 && voicePlaytimeMS == 0 {
			voiceEncodeType, voiceSampleRate, voiceBitsPerSample, voicePlaytimeMS = resolveWechatVoiceMetadata(resolved.Path)
		}
		w.Logger.Info("微信语音元数据",
			zap.String("account_id", account.id),
			zap.String("to_user_id", toUserID),
			zap.String("file_path", resolved.Path),
			zap.Int("encode_type", voiceEncodeType),
			zap.Int("sample_rate", voiceSampleRate),
			zap.Int("bits_per_sample", voiceBitsPerSample),
			zap.Int("playtime_ms", voicePlaytimeMS),
		)
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
	if bizErr := resp.bizErr(); bizErr != nil {
		return nil, 0, fmt.Errorf("wechat getuploadurl: %w", bizErr)
	}
	if resp.UploadParam == "" && strings.TrimSpace(resp.UploadFullURL) == "" {
		return nil, 0, fmt.Errorf("wechat getuploadurl returned neither upload_param nor upload_full_url")
	}
	w.Logger.Info("微信出站媒体上传地址就绪",
		zap.String("account_id", account.id),
		zap.String("to_user_id", toUserID),
		zap.String("file_path", resolved.Path),
		zap.String("media_type", wechatMediaTypeName(mediaType)),
		zap.Bool("has_upload_param", strings.TrimSpace(resp.UploadParam) != ""),
		zap.Bool("has_upload_full_url", strings.TrimSpace(resp.UploadFullURL) != ""),
	)
	downloadParam, ciphertextSize, err := uploadBufferToCDN(ctx, w.httpClient, account.cdnBaseURL, resp.UploadFullURL, resp.UploadParam, fileKey, aesKey, plain)
	if err != nil {
		return nil, 0, err
	}
	w.Logger.Info("微信出站媒体 CDN 上传完成",
		zap.String("account_id", account.id),
		zap.String("to_user_id", toUserID),
		zap.String("file_path", resolved.Path),
		zap.String("media_type", wechatMediaTypeName(mediaType)),
		zap.Int("cipher_size", ciphertextSize),
		zap.Bool("has_download_param", strings.TrimSpace(downloadParam) != ""),
	)
	return &wechatUploadedMedia{
		DownloadParam:      downloadParam,
		AESKey:             aesKey,
		FileName:           filepath.Base(filePath),
		FileSize:           len(plain),
		FileSizeCiphertext: ciphertextSize,
		VoiceEncodeType:    voiceEncodeType,
		VoiceBitsPerSample: voiceBitsPerSample,
		VoiceSampleRate:    voiceSampleRate,
		VoicePlaytimeMS:    voicePlaytimeMS,
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
		AESKey:            base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(uploaded.AESKey))),
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
	case wechatMediaTypeVoice:
		return &wechatMessageItem{
			Type: wechatMessageItemVoice,
			VoiceItem: &wechatVoiceItem{
				Media:         cdnMedia,
				EncodeType:    uploaded.VoiceEncodeType,
				BitsPerSample: uploaded.VoiceBitsPerSample,
				SampleRate:    uploaded.VoiceSampleRate,
				Playtime:      uploaded.VoicePlaytimeMS,
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
			FromUserID:   "",
			ToUserID:     toUserID,
			ClientID:     hex.EncodeToString(clientIDBytes),
			MessageType:  wechatMessageTypeBot,
			MessageState: wechatMessageStateFinish,
			ItemList:     []*wechatMessageItem{item},
			ContextToken: contextToken,
		},
		BaseInfo: wechatBaseInfo{ChannelVersion: wechatAPIChannelVersion},
	}
	return w.sendMessage(ctx, account, req)
}
