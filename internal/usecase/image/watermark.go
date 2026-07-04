package scannerimage

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/fogleman/gg"
	"golang.org/x/image/font/basicfont"
)

const (
	watermarkFullLongEdge   = 1600
	watermarkCornerLongEdge = 900
	watermarkJPEGQuality    = 88
)

// WatermarkRequest is the input for watermarking.
type WatermarkRequest struct {
	Image  string `json:"image" binding:"required"`
	UserID string `json:"userId,omitempty"`
	Date   string `json:"date,omitempty"`
	Mode   string `json:"mode,omitempty"`
}

// WatermarkResponse is the output of watermarking.
type WatermarkResponse struct {
	OK            string `json:"ok"`
	Mode          string `json:"mode"`
	WatermarkText string `json:"watermarkText"`
	Full          string `json:"full"`
	Corners       struct {
		TopLeft     string `json:"topLeft"`
		TopRight    string `json:"topRight"`
		BottomLeft  string `json:"bottomLeft"`
		BottomRight string `json:"bottomRight"`
	} `json:"corners"`
}

// ApplyWatermark generates preview or vault watermarked images.
func ApplyWatermark(req WatermarkRequest) (*WatermarkResponse, error) {
	data, err := dataURLToBytes(req.Image)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image bytes: %w", err)
	}
	src, _ = autoOrient(data, src)

	bounds := src.Bounds()
	W, H := bounds.Dx(), bounds.Dy()

	mode := strings.ToLower(req.Mode)
	isVault := mode == "vault"

	stamp := ""
	if isVault {
		stamp = formatStamp(req.UserID, req.Date)
	}

	var full image.Image = imaging.Resize(src, watermarkFullLongEdge, 0, imaging.Lanczos)
	if isVault {
		full = stampImage(full, stamp)
	}

	cw := int(math.Round(float64(W) * 0.4))
	ch := int(math.Round(float64(H) * 0.4))
	cornerAt := func(left, top int) image.Image {
		rect := image.Rect(left, top, left+cw, top+ch)
		cropped := imaging.Crop(src, rect)
		var resized image.Image = imaging.Resize(cropped, watermarkCornerLongEdge, 0, imaging.Lanczos)
		if isVault {
			return stampImage(resized, stamp)
		}
		return resized
	}

	resp := &WatermarkResponse{
		OK:            "true",
		Mode:          mode,
		WatermarkText: stamp,
		Full:          encodeJPEGDataURL(full),
	}
	resp.Corners.TopLeft = encodeJPEGDataURL(cornerAt(0, 0))
	resp.Corners.TopRight = encodeJPEGDataURL(cornerAt(W-cw, 0))
	resp.Corners.BottomLeft = encodeJPEGDataURL(cornerAt(0, H-ch))
	resp.Corners.BottomRight = encodeJPEGDataURL(cornerAt(W-cw, H-ch))
	return resp, nil
}

func formatStamp(userID, date string) string {
	now := time.Now().UTC()
	d := date
	if d == "" {
		d = now.Format("2006-01-02")
	}
	dateFmt := formatDateHuman(d)
	timeFmt := now.Format("15:04")
	who := toAsciiSafe(userID)
	if who == "" {
		who = "user"
	}
	return fmt.Sprintf("%s – %s – %s – SwibSwap", who, dateFmt, timeFmt)
}

func formatDateHuman(iso string) string {
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	re := regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})`)
	m := re.FindStringSubmatch(iso)
	if len(m) != 4 {
		return iso
	}
	monthIdx := 0
	fmt.Sscanf(m[2], "%d", &monthIdx)
	if monthIdx < 1 {
		monthIdx = 1
	}
	if monthIdx > 12 {
		monthIdx = 12
	}
	return fmt.Sprintf("%s-%s-%s", m[3], months[monthIdx-1], m[1])
}

func toAsciiSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= ' ' && r <= 'ÿ' {
			return r
		}
		return -1
	}, s)
}

func stampImage(img image.Image, stamp string) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	dc := gg.NewContextForImage(img)

	fontPath := findFont()
	if fontPath != "" {
		_ = dc.LoadFontFace(fontPath, math.Max(14, float64(min(w, h))/36))
	} else {
		dc.SetFontFace(basicfont.Face7x13)
	}

	x := float64(w) * 0.04
	y := float64(h) * 0.965

	// Dark stroke
	dc.SetColor(color.RGBA{0, 0, 0, 140})
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			if dx == 0 && dy == 0 {
				continue
			}
			dc.DrawStringAnchored(stamp, x+float64(dx), y+float64(dy), 0, 0)
		}
	}

	// White fill
	dc.SetColor(color.RGBA{255, 255, 255, 235})
	dc.DrawStringAnchored(stamp, x, y, 0, 0)

	return dc.Image()
}

func findFont() string {
	candidates := []string{
		"C:/Windows/Fonts/DejaVuSans.ttf",
		"C:/Windows/Fonts/Arial.ttf",
		"C:/Windows/Fonts/segoeui.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
