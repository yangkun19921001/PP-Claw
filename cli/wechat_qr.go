package cli

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

const qrRenderQuietZone = 2

var (
	dataURLImageRE = regexp.MustCompile(`data:image/[^;]+;base64,[A-Za-z0-9+/=]+`)
	imgSrcRE       = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)
)

func renderWechatQRCodeToTerminal(qrImage string) error {
	img, err := loadQRCodeImage(qrImage)
	if err != nil {
		return renderQRCodeFromText(qrImage)
	}
	printImageAsQRCode(img)
	return nil
}

func renderQRCodeFromText(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty qr text")
	}
	code, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return err
	}
	bitmap := code.Bitmap()
	if len(bitmap) == 0 {
		return fmt.Errorf("empty qr bitmap")
	}
	printBitmapAsQRCode(bitmap)
	return nil
}

func loadQRCodeImage(src string) (image.Image, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("empty qr image")
	}

	switch {
	case strings.HasPrefix(src, "data:image/"):
		data, err := decodeDataURLImage(src)
		if err != nil {
			return nil, err
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return img, nil
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		return loadQRCodeImageFromURL(src, 0)
	default:
		return nil, fmt.Errorf("unsupported qr image source")
	}
}

func decodeDataURLImage(src string) ([]byte, error) {
	idx := strings.Index(src, ",")
	if idx <= 0 {
		return nil, fmt.Errorf("invalid data url")
	}
	raw := src[idx+1:]
	return base64.StdEncoding.DecodeString(raw)
}

func fetchRemoteImage(src string) ([]byte, string, error) {
	resp, err := (&http.Client{}).Get(src)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("download qr image failed: %d %s", resp.StatusCode, string(body))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func loadQRCodeImageFromURL(src string, depth int) (image.Image, error) {
	if depth > 2 {
		return nil, fmt.Errorf("qr page nested too deep")
	}
	data, contentType, err := fetchRemoteImage(src)
	if err != nil {
		return nil, err
	}

	if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
		return img, nil
	}

	if !strings.Contains(strings.ToLower(contentType), "text/html") && !bytes.Contains(bytes.ToLower(data), []byte("<html")) {
		return nil, fmt.Errorf("image: unknown format")
	}

	html := string(data)
	if matched := dataURLImageRE.FindString(html); matched != "" {
		return loadQRCodeImage(matched)
	}

	matches := imgSrcRE.FindStringSubmatch(html)
	if len(matches) == 2 {
		nextURL := resolveRelativeURL(src, matches[1])
		if nextURL != "" {
			return loadQRCodeImageFromURL(nextURL, depth+1)
		}
	}

	return nil, fmt.Errorf("html page did not contain a usable qr image")
}

func resolveRelativeURL(baseRaw, refRaw string) string {
	baseURL, err := url.Parse(baseRaw)
	if err != nil {
		return ""
	}
	refURL, err := url.Parse(strings.TrimSpace(refRaw))
	if err != nil {
		return ""
	}
	return baseURL.ResolveReference(refURL).String()
}

func printImageAsQRCode(img image.Image) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return
	}

	cellsX := width + qrRenderQuietZone*2
	cellsY := height + qrRenderQuietZone*2
	matrix := make([][]bool, cellsY)
	for y := 0; y < cellsY; y++ {
		matrix[y] = make([]bool, cellsX)
		for x := 0; x < cellsX; x++ {
			if x < qrRenderQuietZone || y < qrRenderQuietZone || x >= width+qrRenderQuietZone || y >= height+qrRenderQuietZone {
				matrix[y][x] = false
				continue
			}
			r, g, b, _ := img.At(bounds.Min.X+x-qrRenderQuietZone, bounds.Min.Y+y-qrRenderQuietZone).RGBA()
			gray := (299*r + 587*g + 114*b) / 1000
			matrix[y][x] = gray < 0x7fff
		}
	}

	// Top/bottom white margin improves scan reliability in many terminals.
	fmt.Println()
	for y := 0; y < cellsY; y += 2 {
		var line strings.Builder
		for x := 0; x < cellsX; x++ {
			top := matrix[y][x]
			bottom := false
			if y+1 < cellsY {
				bottom = matrix[y+1][x]
			}
			switch {
			case top && bottom:
				line.WriteRune('█')
			case top && !bottom:
				line.WriteRune('▀')
			case !top && bottom:
				line.WriteRune('▄')
			default:
				line.WriteRune(' ')
			}
		}
		fmt.Println(line.String())
	}
	fmt.Println()
}

func printBitmapAsQRCode(bitmap [][]bool) {
	if len(bitmap) == 0 || len(bitmap[0]) == 0 {
		return
	}
	width := len(bitmap[0]) + qrRenderQuietZone*2
	height := len(bitmap) + qrRenderQuietZone*2
	matrix := make([][]bool, height)
	for y := 0; y < height; y++ {
		matrix[y] = make([]bool, width)
		for x := 0; x < width; x++ {
			if x < qrRenderQuietZone || y < qrRenderQuietZone || x >= len(bitmap[0])+qrRenderQuietZone || y >= len(bitmap)+qrRenderQuietZone {
				matrix[y][x] = false
				continue
			}
			matrix[y][x] = bitmap[y-qrRenderQuietZone][x-qrRenderQuietZone]
		}
	}
	fmt.Println()
	for y := 0; y < height; y += 2 {
		var line strings.Builder
		for x := 0; x < width; x++ {
			top := matrix[y][x]
			bottom := false
			if y+1 < height {
				bottom = matrix[y+1][x]
			}
			switch {
			case top && bottom:
				line.WriteRune('█')
			case top && !bottom:
				line.WriteRune('▀')
			case !top && bottom:
				line.WriteRune('▄')
			default:
				line.WriteRune(' ')
			}
		}
		fmt.Println(line.String())
	}
	fmt.Println()
}
