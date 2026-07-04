package scannerimage

import (
	"bytes"
	"fmt"
	stdimage "image"
	"math"

	"github.com/disintegration/imaging"
)

// QualityMetrics holds computer-vision quality signals.
type QualityMetrics struct {
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	SourceWidth  int     `json:"sourceWidth"`
	SourceHeight int     `json:"sourceHeight"`
	Centering    float64 `json:"centeringScore"`
	Corners      float64 `json:"cornerScore"`
	Surface      float64 `json:"surfaceScore"`
	Raw          struct {
		VRatio       float64 `json:"vRatio"`
		HRatio       float64 `json:"hRatio"`
		CornerAvg    float64 `json:"cornerAvg"`
		SurfaceDelta float64 `json:"surfaceDelta"`
	} `json:"raw"`
}

// QualityResult is the combined CV + AI quality result.
type QualityResult struct {
	OK       bool           `json:"ok"`
	Quality  interface{}    `json:"quality,omitempty"`
	Metrics  *QualityMetrics `json:"metrics,omitempty"`
	Cached   bool           `json:"cached,omitempty"`
	Hash     string         `json:"hash,omitempty"`
	Error    string         `json:"error,omitempty"`
}

// ComputeQualityMetrics evaluates centering, corner brightness, and surface texture.
func ComputeQualityMetrics(imageData string) (*QualityMetrics, error) {
	data, err := dataURLToBytes(imageData)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	src, _, err := stdimage.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image bytes: %w", err)
	}

	// Auto-orient and resize to target long edge.
	src, _ = autoOrient(data, src)
	bounds := src.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()

	target := 600
	resized := src
	if sw > target || sh > target {
		resized = imaging.Resize(src, target, 0, imaging.Lanczos)
	}
	gray := imaging.Grayscale(resized)
	bounds = gray.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	pixels := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			_, _, b, _ := gray.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			pixels[y*w+x] = uint8(b >> 8)
		}
	}

	avg := func(xStart, yStart, xEnd, yEnd int) float64 {
		sum := 0.0
		count := 0
		for y := yStart; y < yEnd; y++ {
			for x := xStart; x < xEnd; x++ {
				sum += float64(pixels[y*w+x])
				count++
			}
		}
		if count == 0 {
			return 0
		}
		return sum / float64(count)
	}

	strip := int(math.Max(4, math.Round(math.Min(float64(w), float64(h))*0.05)))
	topAvg := avg(0, 0, w, strip)
	bottomAvg := avg(0, h-strip, w, h)
	leftAvg := avg(0, 0, strip, h)
	rightAvg := avg(w-strip, 0, w, h)

	vRatio := math.Max(topAvg, bottomAvg) / math.Max(1, math.Min(topAvg, bottomAvg))
	hRatio := math.Max(leftAvg, rightAvg) / math.Max(1, math.Min(leftAvg, rightAvg))
	centeringScore := math.Max(0, 10-((vRatio-1)+(hRatio-1))*12)

	cornerBox := int(math.Max(8, math.Round(math.Min(float64(w), float64(h))*0.04)))
	tlAvg := avg(0, 0, cornerBox, cornerBox)
	trAvg := avg(w-cornerBox, 0, w, cornerBox)
	blAvg := avg(0, h-cornerBox, cornerBox, h)
	brAvg := avg(w-cornerBox, h-cornerBox, w, h)
	cornerAvg := (tlAvg + trAvg + blAvg + brAvg) / 4
	cornerScore := math.Max(0, math.Min(10, 10-(cornerAvg/255)*12))

	cx0 := int(math.Round(float64(w) * 0.2))
	cx1 := int(math.Round(float64(w) * 0.8))
	cy0 := int(math.Round(float64(h) * 0.2))
	cy1 := int(math.Round(float64(h) * 0.8))
	prev := float64(pixels[cy0*w+cx0])
	sumDelta := 0.0
	n := 0
	for y := cy0; y < cy1; y += 4 {
		for x := cx0; x < cx1; x += 4 {
			v := float64(pixels[y*w+x])
			sumDelta += math.Abs(v - prev)
			prev = v
			n++
		}
	}
	avgDelta := sumDelta / math.Max(1, float64(n))
	surfaceScore := math.Max(0, 10-math.Abs(avgDelta-17)*0.3)

	metrics := &QualityMetrics{
		Width:        w,
		Height:       h,
		SourceWidth:  sw,
		SourceHeight: sh,
		Centering:    round1(centeringScore),
		Corners:      round1(cornerScore),
		Surface:      round1(surfaceScore),
	}
	metrics.Raw.VRatio = round2(vRatio)
	metrics.Raw.HRatio = round2(hRatio)
	metrics.Raw.CornerAvg = round1(cornerAvg)
	metrics.Raw.SurfaceDelta = round1(avgDelta)

	return metrics, nil
}

func round1(n float64) float64 {
	return math.Round(n*10) / 10
}

func round2(n float64) float64 {
	return math.Round(n*100) / 100
}

// BuildQualityPrompt creates the Haiku judging prompt.
func BuildQualityPrompt(metrics *QualityMetrics, tcg string) string {
	game := "One Piece"
	if tcg == "ygo" {
		game = "Yu-Gi-Oh!"
	}
	return fmt.Sprintf(`You are a TCG card-grading assistant. The user has photographed a %s card. CV metrics:
- Centering score (0-10): %.1f (vRatio %.2f, hRatio %.2f)
- Corner score (0-10): %.1f (corner avg %.1f, higher = more whitening)
- Surface score (0-10): %.1f (delta %.1f, sweet spot ~17)

Return ONLY this JSON:
{"grade":8.5,"subscores":{"centering":8,"corners":9,"edges":8,"surface":8},"estimatedTier":"PSA 8-9 candidate","issues":["light edge whitening"],"confidence":78}
Grade is 1-10 PSA scale. estimatedTier: PSA 10 candidate, PSA 9 candidate, PSA 8-9 candidate, PSA 7-8 candidate, Playable raw, Damaged.`, game, metrics.Centering, metrics.Raw.VRatio, metrics.Raw.HRatio, metrics.Corners, metrics.Raw.CornerAvg, metrics.Surface, metrics.Raw.SurfaceDelta)
}

