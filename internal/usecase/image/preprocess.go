package scannerimage

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/rwcarlsen/goexif/exif"
)

const (
	fullTargetLongEdge   = 1600
	cornerTargetLongEdge = 900
	jpegQuality          = 90
	navyHex              = "#0A0F2E"
)

// PreprocessedImages holds the full card view and four corner zooms.
type PreprocessedImages struct {
	Full    string `json:"full"`
	Corners struct {
		TopLeft     string `json:"topLeft"`
		TopRight    string `json:"topRight"`
		BottomLeft  string `json:"bottomLeft"`
		BottomRight string `json:"bottomRight"`
	} `json:"corners"`
	Diagnostics struct {
		CropMethod string `json:"cropMethod"`
		FeatherPx  int    `json:"featherPx"`
		CardW      int    `json:"cardW"`
		CardH      int    `json:"cardH"`
	} `json:"diagnostics"`
}

// PreprocessForScan transforms a raw camera image into the 5-image payload for AI inference.
func PreprocessForScan(imageData string) (*PreprocessedImages, error) {
	data, err := dataURLToBytes(imageData)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image bytes: %w", err)
	}

	img, err = autoOrient(data, img)
	if err != nil {
		return nil, fmt.Errorf("auto orient: %w", err)
	}

	cropped, cropMethod, err := smartCardCrop(img)
	if err != nil {
		return nil, fmt.Errorf("smart crop: %w", err)
	}

	padded, featherPx := featherPad(cropped)
	enhanced := enhance(padded)

	full := resizeLongEdge(enhanced, fullTargetLongEdge)

	w, h := full.Bounds().Dx(), full.Bounds().Dy()
	cornerSize := image.Rect(0, 0, int(float64(w)*0.4), int(float64(h)*0.4))

	corners := struct {
		TopLeft     string `json:"topLeft"`
		TopRight    string `json:"topRight"`
		BottomLeft  string `json:"bottomLeft"`
		BottomRight string `json:"bottomRight"`
	}{
		TopLeft:     encodeCorner(full, image.Rect(0, 0, cornerSize.Dx(), cornerSize.Dy())),
		TopRight:    encodeCorner(full, image.Rect(w-cornerSize.Dx(), 0, w, cornerSize.Dy())),
		BottomLeft:  encodeCorner(full, image.Rect(0, h-cornerSize.Dy(), cornerSize.Dx(), h)),
		BottomRight: encodeCorner(full, image.Rect(w-cornerSize.Dx(), h-cornerSize.Dy(), w, h)),
	}

	result := &PreprocessedImages{
		Full:    encodeJPEGDataURL(full),
		Corners: corners,
	}
	result.Diagnostics.CropMethod = cropMethod
	result.Diagnostics.FeatherPx = featherPx
	result.Diagnostics.CardW = w
	result.Diagnostics.CardH = h

	return result, nil
}

func dataURLToBytes(dataURL string) ([]byte, error) {
	if strings.HasPrefix(dataURL, "data:") {
		parts := strings.SplitN(dataURL, ",", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid data URL")
		}
		return base64.StdEncoding.DecodeString(parts[1])
	}
	return base64.StdEncoding.DecodeString(dataURL)
}

func autoOrient(data []byte, img image.Image) (image.Image, error) {
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return img, nil // no EXIF or invalid EXIF is fine
	}

	orient, err := x.Get(exif.Orientation)
	if err != nil {
		return img, nil
	}
	v, err := orient.Int(0)
	if err != nil {
		return img, nil
	}

	switch v {
	case 2:
		return imaging.FlipH(img), nil
	case 3:
		return imaging.Rotate180(img), nil
	case 4:
		return imaging.FlipV(img), nil
	case 5:
		return imaging.Transpose(img), nil
	case 6:
		return imaging.Rotate270(img), nil
	case 7:
		return imaging.Transverse(img), nil
	case 8:
		return imaging.Rotate90(img), nil
	default:
		return img, nil
	}
}

func smartCardCrop(img image.Image) (image.Image, string, error) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	thresholds := []uint32{12, 18, 24, 32, 42, 55}
	minAspect := 0.55
	maxAspect := 0.95
	minAreaRatio := 0.40

	for _, t := range thresholds {
		bbox := trimEdges(img, t)
		cw, ch := bbox.Dx(), bbox.Dy()
		if cw <= 0 || ch <= 0 {
			continue
		}
		aspect := float64(cw) / float64(ch)
		if aspect < minAspect {
			aspect = float64(ch) / float64(cw)
		}
		areaRatio := float64(cw*ch) / float64(w*h)
		if aspect >= minAspect && aspect <= maxAspect && areaRatio >= minAreaRatio {
			return imaging.Crop(img, bbox), "trim", nil
		}
	}

	// Fallback to 85% center crop.
	cw := int(float64(w) * 0.85)
	ch := int(float64(h) * 0.85)
	x0 := (w - cw) / 2
	y0 := (h - ch) / 2
	bbox := image.Rect(x0, y0, x0+cw, y0+ch)
	return imaging.Crop(img, bbox), "center85", nil
}

// trimEdges finds a bounding box by scanning from each edge until pixels differ from the edge color.
func trimEdges(img image.Image, threshold uint32) image.Rectangle {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return bounds
	}

	// Sample background color from a strip near each edge.
	bg := sampleEdgeColor(img, 8)

	var left, right, top, bottom int
	for left = 0; left < w; left++ {
		if columnDiffers(img, left, bg, threshold) {
			break
		}
	}
	for right = w - 1; right >= 0; right-- {
		if columnDiffers(img, right, bg, threshold) {
			break
		}
	}
	for top = 0; top < h; top++ {
		if rowDiffers(img, top, bg, threshold) {
			break
		}
	}
	for bottom = h - 1; bottom >= 0; bottom-- {
		if rowDiffers(img, bottom, bg, threshold) {
			break
		}
	}

	if left >= right || top >= bottom {
		return bounds
	}
	return image.Rect(left, top, right+1, bottom+1)
}

func sampleEdgeColor(img image.Image, margin int) color.RGBA64 {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	var r, g, b, a uint32
	var count uint32

	sample := func(x, y int) {
		pr, pg, pb, pa := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
		r += pr >> 8
		g += pg >> 8
		b += pb >> 8
		a += pa >> 8
		count++
	}

	m := margin
	if m > w/4 {
		m = w / 4
	}
	if m > h/4 {
		m = h / 4
	}
	for x := 0; x < w; x += 4 {
		for y := 0; y < m; y += 2 {
			sample(x, y)
		}
		for y := h - m; y < h; y += 2 {
			sample(x, y)
		}
	}
	for y := m; y < h-m; y += 4 {
		for x := 0; x < m; x += 2 {
			sample(x, y)
		}
		for x := w - m; x < w; x += 2 {
			sample(x, y)
		}
	}

	if count == 0 {
		return color.RGBA64{}
	}
	return color.RGBA64{
		R: uint16(r / count),
		G: uint16(g / count),
		B: uint16(b / count),
		A: uint16(a / count),
	}
}

func colorDistance(c1, c2 color.RGBA64) uint32 {
	dr := int32(c1.R) - int32(c2.R)
	dg := int32(c1.G) - int32(c2.G)
	db := int32(c1.B) - int32(c2.B)
	return uint32(math.Sqrt(float64(dr*dr + dg*dg + db*db)))
}

func columnDiffers(img image.Image, x int, bg color.RGBA64, threshold uint32) bool {
	bounds := img.Bounds()
	for y := 0; y < bounds.Dy(); y++ {
		c := rgba64(img.At(bounds.Min.X+x, bounds.Min.Y+y))
		if colorDistance(c, bg) > threshold {
			return true
		}
	}
	return false
}

func rowDiffers(img image.Image, y int, bg color.RGBA64, threshold uint32) bool {
	bounds := img.Bounds()
	for x := 0; x < bounds.Dx(); x++ {
		c := rgba64(img.At(bounds.Min.X+x, bounds.Min.Y+y))
		if colorDistance(c, bg) > threshold {
			return true
		}
	}
	return false
}

func rgba64(c color.Color) color.RGBA64 {
	r, g, b, a := c.RGBA()
	return color.RGBA64{R: uint16(r), G: uint16(g), B: uint16(b), A: uint16(a)}
}

func featherPad(img image.Image) (image.Image, int) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	longEdge := w
	if h > w {
		longEdge = h
	}
	feather := int(float64(longEdge) * 0.03)
	if feather < 24 {
		feather = 24
	}

	newW, newH := w+feather*2, h+feather*2
	bg := parseHex(navyHex)
	canvas := imaging.New(newW, newH, bg)
	canvas = imaging.Paste(canvas, img, image.Pt(feather, feather))
	return canvas, feather
}

func parseHex(hex string) color.Color {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b uint8
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func enhance(img image.Image) image.Image {
	img = imaging.AdjustGamma(img, 1.15)
	img = normalize(img)
	img = imaging.AdjustSaturation(img, 1.25)
	img = imaging.AdjustBrightness(img, 1.08)
	img = imaging.Sharpen(img, 1.1)
	return img
}

func normalize(img image.Image) image.Image {
	bounds := img.Bounds()
	minV, maxV := uint32(255), uint32(0)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			gray := (r>>8 + g>>8 + b>>8) / 3
			if gray < minV {
				minV = gray
			}
			if gray > maxV {
				maxV = gray
			}
		}
	}
	if maxV == minV {
		return img
	}
	dst := image.NewRGBA(bounds)
	scale := 255.0 / float64(maxV-minV)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			nr := clamp(float64((r>>8)-minV)*scale)
			ng := clamp(float64((g>>8)-minV)*scale)
			nb := clamp(float64((b>>8)-minV)*scale)
			dst.Set(x, y, color.RGBA{R: nr, G: ng, B: nb, A: uint8(a >> 8)})
		}
	}
	return dst
}

func clamp(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func resizeLongEdge(img image.Image, target int) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w >= h {
		return imaging.Resize(img, target, 0, imaging.Lanczos)
	}
	return imaging.Resize(img, 0, target, imaging.Lanczos)
}

func encodeCorner(img image.Image, rect image.Rectangle) string {
	cropped := imaging.Crop(img, rect)
	resized := resizeLongEdge(cropped, cornerTargetLongEdge)
	return encodeJPEGDataURL(resized)
}

func encodeJPEGDataURL(img image.Image) string {
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality})
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}
