package scan

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/anthropic"
	firebaseinfra "github.com/jatibroski/sws-scanner-service/internal/infrastructure/firebase"
	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/vision"
	scannerimage "github.com/jatibroski/sws-scanner-service/internal/usecase/image"
)

const cacheVersion = "v14-scn69-don-poison-jp-cn-fix"

// UseCase orchestrates the card-scanning pipeline.
type UseCase struct {
	anthropic    *anthropic.Client
	vision       *vision.Client
	firestore    *firebaseinfra.Firestore
	storage      *firebaseinfra.Storage
	cacheVersion string
}

// NewScanUseCase creates a new scan use case.
func NewScanUseCase(
	anthropicClient *anthropic.Client,
	visionClient *vision.Client,
	firestore *firebaseinfra.Firestore,
	storage *firebaseinfra.Storage,
) *UseCase {
	return &UseCase{
		anthropic:    anthropicClient,
		vision:       visionClient,
		firestore:    firestore,
		storage:      storage,
		cacheVersion: cacheVersion,
	}
}

// Scan runs the full identification pipeline.
func (uc *UseCase) Scan(ctx context.Context, req ScanRequest, userID string) (*ScanResponse, error) {
	if req.Image == "" || req.TCG == "" {
		return &ScanResponse{OK: false, Error: "image and tcg are required"}, nil
	}
	req.TCG = strings.ToLower(req.TCG)
	if req.TCG != "op" && req.TCG != "ygo" {
		return &ScanResponse{OK: false, Error: "unsupported tcg"}, nil
	}

	hash := HashImage(req.Image)

	// Exact-image cache lookup
	if !req.Force && uc.firestore != nil {
		cached, err := uc.firestore.GetScanCache(ctx, hash)
		if err == nil && cached != nil {
			if cached.CacheVersion == uc.cacheVersion || cached.CorrectedBy != "" {
				card := mapToCard(cached.Card)
				return &ScanResponse{
					OK:     true,
					Card:   card,
					Cached: true,
					Hash:   hash,
				}, nil
			}
		}
	}

	// Perceptual hash lookup
	pHash, _ := AverageHash(req.Image)
	if pHash != "" && uc.firestore != nil {
		doc, err := uc.firestore.FindContributionsByPHash(ctx, pHash)
		if err == nil && doc != nil && doc.Ref != nil && doc.Ref.Parent != nil {
			parent, err := doc.Ref.Parent.Parent.Get(ctx)
			if err == nil {
				var v firebaseinfra.VerifiedCardDoc
				_ = parent.DataTo(&v)
				v.DocKey = parent.Ref.ID
				v.Data = parent.Data()
				return &ScanResponse{
					OK:           true,
					Card:         verifiedToCard(&v),
					PHash:        pHash,
					IdentifiedBy: "phash-community",
					Hash:         hash,
				}, nil
			}
		}
	}

	// Image preprocessing
	preprocessed, err := scannerimage.PreprocessForScan(req.Image)
	if err != nil {
		return &ScanResponse{OK: false, Error: "preprocess failed: " + err.Error()}, nil
	}

	// Anthropic image blocks
	blocks := imagesToContentBlocks(preprocessed)
	prompt := buildOPPrompt(req.Lang)
	if req.TCG == "ygo" {
		prompt = buildYGOPrompt(req.Lang)
	}

	// Parallel Haiku + Vision
	var haikuText string
	var visionResult *vision.Result
	var haikuErr error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		haikuText, haikuErr = uc.anthropic.SendMessage(ctx, prompt, blocks)
	}()
	go func() {
		defer wg.Done()
		if req.TCG == "op" && uc.vision != nil {
			visionResult, _ = uc.vision.WebDetect(ctx, "", stripDataURL(req.Image), 50)
		}
	}()
	wg.Wait()

	if haikuErr != nil {
		fmt.Printf("[scan] haiku error: %v\n", haikuErr)
	}
	if uc.anthropic == nil {
		fmt.Println("[scan] anthropic client not initialized (missing ANTHROPIC_API_KEY)")
	}
	if visionResult == nil {
		fmt.Println("[scan] vision result is nil (missing GOOGLE_VISION_API_KEY or Vision disabled)")
	} else if !visionResult.OK {
		fmt.Printf("[scan] vision error: %s\n", visionResult.Error)
	}

	card, haikuFailed := &Card{}, false
	if haikuErr != nil {
		haikuFailed = true
	} else {
		parsed, err := ParseHaikuJSON(haikuText, req.TCG)
		if err != nil {
			haikuFailed = true
		} else {
			card = parsed
		}
	}

	var ocrOut *OCROutput
	var crossCheck *CrossCheckResult
	donResult := &DonIdentification{}

	if req.TCG == "op" {
		ocrText := ""
		if visionResult != nil && visionResult.OK {
			ocrText = visionResult.OCRText
		}
		ocrOut = ExtractFromOcr(ocrText, req.Lang)

		// OCR-first override
		if ocrOut.CardCode != "" {
			card.Code = ocrOut.CardCode
			if card.Confidence < 92 {
				card.Confidence = 92
			}
		}
		if ocrOut.IsDonCard && !strings.Contains(strings.ToLower(ocrText), "blocker") {
			card.Type = "DON!!"
			card.Rarity = "DON!!"
			card.NameEn = ""
			card.NameJp = ""
		}

		// Cross-check Haiku vs Vision code
		visionCode := ""
		if visionResult != nil && visionResult.OK {
			code, _ := visionExtractCardCode(visionResult.Web)
			visionCode = code
		}
		crossCheck = &CrossCheckResult{
			HaikuCode:  card.Code,
			VisionCode: visionCode,
		}
		if visionCode != "" && card.Code != "" && visionCode == card.Code {
			crossCheck.Agreed = true
			crossCheck.Adopted = card.Code
			if card.Confidence < 95 {
				card.Confidence = 95
			}
		} else if visionCode != "" && card.Code == "" {
			crossCheck.Adopted = visionCode
			card.Code = visionCode
			if card.Confidence < 85 {
				card.Confidence = 85
			}
		} else {
			crossCheck.Adopted = card.Code
		}

		// DON rescue
		if !isRealOPCode(card.Code) || ocrOut.IsDonCard {
			var web *vision.WebDetection
			if visionResult != nil {
				web = visionResult.Web
			}
			donResult = IdentifyDonCard(web, ocrText)
			if donResult.IsDonCard && donResult.Confidence >= 0.55 {
				card.Code = donResult.SyntheticCode
				card.NameEn = donResult.FullName
				card.Rarity = donResult.Rarity
				card.Type = "DON!!"
			}
		}
	}

	// Verified lookup
	var verified interface{}
	if card.Code != "" && card.Rarity != "" && uc.firestore != nil {
		key := makeVerifiedKey(card.Code, card.Rarity)
		v, err := uc.firestore.GetVerifiedCard(ctx, key)
		if err != nil {
			v, _ = uc.firestore.GetVerifiedCard(ctx, makeVerifiedKey(card.Code, "base"))
		}
		if v != nil {
			verified = normalizeVerified(v, req.Lang)
		}
	}

	// Trust evaluation
	trustworthy, identifiedBy := evaluateTrust(card, ocrOut, crossCheck, donResult, req.TCG)

	// Cache persistence
	imageURL := ""
	if trustworthy && userID != "" && uc.firestore != nil && uc.storage != nil {
		imageURL, _ = uc.storage.UploadScanImage(ctx, hash, req.Image)
		payload := map[string]interface{}{
			"imageHash":    hash,
			"cacheVersion": uc.cacheVersion,
			"status":       "identified",
			"card":         card,
			"imageUrl":     imageURL,
			"identifiedBy": identifiedBy,
		}
		_ = uc.firestore.PutScanCache(ctx, hash, payload)
	}

	resp := &ScanResponse{
		OK:           true,
		Card:         card,
		Hash:         hash,
		PHash:        pHash,
		ImageURL:     imageURL,
		IdentifiedBy: identifiedBy,
		CrossCheck:   crossCheck,
		DonVision:    donResult,
		OCRExtract:   ocrOut,
		Preprocess:   preprocessed.Diagnostics,
		Verified:     verified,
		HaikuFailed:  haikuFailed,
	}

	if !trustworthy && card.Code == "" {
		resp.OK = false
		resp.Error = "unable to identify card"
	}

	return resp, nil
}

func imagesToContentBlocks(p *scannerimage.PreprocessedImages) []anthropic.ContentBlock {
	return []anthropic.ContentBlock{
		{Type: "text", Text: "Full card:"},
		imageBlock(p.Full),
		{Type: "text", Text: "Top-left corner:"},
		imageBlock(p.Corners.TopLeft),
		{Type: "text", Text: "Top-right corner:"},
		imageBlock(p.Corners.TopRight),
		{Type: "text", Text: "Bottom-left corner:"},
		imageBlock(p.Corners.BottomLeft),
		{Type: "text", Text: "Bottom-right corner:"},
		imageBlock(p.Corners.BottomRight),
	}
}

func imageBlock(dataURL string) anthropic.ContentBlock {
	b := anthropic.ContentBlock{Type: "image"}
	b.Source = &struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}{
		Type:      "base64",
		MediaType: mediaType(dataURL),
		Data:      stripDataURL(dataURL),
	}
	return b
}

func stripDataURL(dataURL string) string {
	if idx := strings.Index(dataURL, ","); idx >= 0 {
		return dataURL[idx+1:]
	}
	return dataURL
}

func mediaType(dataURL string) string {
	if strings.HasPrefix(dataURL, "data:image/png") {
		return "image/png"
	}
	if strings.HasPrefix(dataURL, "data:image/webp") {
		return "image/webp"
	}
	if strings.HasPrefix(dataURL, "data:image/gif") {
		return "image/gif"
	}
	return "image/jpeg"
}

func selectVerifiedImageURL(v *firebaseinfra.VerifiedCardDoc, lang string) string {
	if v == nil {
		return ""
	}
	d := v.Data
	if d == nil {
		d = map[string]interface{}{}
	}
	if u, ok := d["watermarkedSampleUrl"].(string); ok && u != "" {
		return u
	}
	if u, ok := d["sampleImageUrl"].(string); ok && u != "" {
		return u
	}
	if u, ok := d["officialImageUrl"].(string); ok && u != "" {
		return u
	}
	if samples, ok := d["samples"].(map[string]interface{}); ok {
		for _, k := range []string{strings.ToUpper(lang), "JP", "EN", "CN"} {
			if u, ok := samples[k].(string); ok && u != "" {
				return u
			}
		}
	}
	return ""
}

func normalizeVerified(v *firebaseinfra.VerifiedCardDoc, lang string) map[string]interface{} {
	if v == nil {
		return nil
	}
	d := v.Data
	if d == nil {
		d = map[string]interface{}{}
	}

	langKey := strings.ToUpper(lang)
	imageURL := d["watermarkedSampleUrl"]
	if imageURL == nil || imageURL == "" {
		imageURL = d["sampleImageUrl"]
	}
	if imageURL == nil || imageURL == "" {
		imageURL = d["officialImageUrl"]
	}
	if imageURL == nil || imageURL == "" {
		if samples, ok := d["samples"].(map[string]interface{}); ok {
			for _, k := range []string{langKey, "JP", "EN", "CN"} {
				if u, ok := samples[k].(string); ok && u != "" {
					imageURL = u
					break
				}
			}
		}
	}

	lastVerified := d["lastVerifiedAt"]
	if t, ok := lastVerified.(time.Time); ok {
		lastVerified = t.Format(time.RFC3339)
	}

	return map[string]interface{}{
		"sampleImageUrl":      imageURL,
		"officialImageUrl":    d["officialImageUrl"],
		"officialName":        d["officialName"],
		"officialSetName":     d["officialSetName"],
		"officialReleaseDate": d["officialReleaseDate"],
		"verificationCount":   d["verificationCount"],
		"lastVerifiedAt":      lastVerified,
	}
}

func makeVerifiedKey(code, rarity string) string {
	r := strings.ReplaceAll(rarity, " ", "")
	r = strings.ReplaceAll(r, "/", "")
	return fmt.Sprintf("%s__%s", code, r)
}

func mapToCard(m map[string]interface{}) *Card {
	c := &Card{}
	if v, ok := m["code"].(string); ok {
		c.Code = v
	}
	if v, ok := m["nameEn"].(string); ok {
		c.NameEn = v
	}
	if v, ok := m["nameJp"].(string); ok {
		c.NameJp = v
	}
	if v, ok := m["rarity"].(string); ok {
		c.Rarity = v
	}
	if v, ok := m["type"].(string); ok {
		c.Type = v
	}
	return c
}

func verifiedToCard(v *firebaseinfra.VerifiedCardDoc) *Card {
	return &Card{
		Code:   v.Code,
		NameEn: v.NameEn,
		NameJp: v.NameJp,
		NameCn: v.NameCn,
		Rarity: v.Rarity,
		Type:   v.Type,
	}
}

func isRealOPCode(code string) bool {
	if strings.EqualFold(code, "DON!!") {
		return false
	}
	return opCodeRegex.MatchString(code) || opCodeSmushed.MatchString(code) || opPromoRegex.MatchString(code)
}

func visionExtractCardCode(web *vision.WebDetection) (string, float64) {
	// Simplified trusted-site extraction
	if web == nil {
		return "", 0
	}
	trusted := []string{"cardpiece", "optcgapi", "apitcg", "bandai"}
	scores := map[string]float64{}
	for _, p := range web.PagesWithMatchingImages {
		lower := strings.ToLower(p.URL)
		for _, host := range trusted {
			if strings.Contains(lower, host) {
				if m := opCodeRegex.FindStringSubmatch(p.URL); m != nil {
					code := normalizeCode(strings.ToUpper(m[0]))
					scores[code] += p.Score
				}
				if m := opCodeSmushed.FindStringSubmatch(p.URL); m != nil {
					code := normalizeCode(strings.ToUpper(m[0]))
					scores[code] += p.Score
				}
			}
		}
	}
	best, bestScore := "", 0.0
	for code, score := range scores {
		if score > bestScore {
			best = code
			bestScore = score
		}
	}
	return best, bestScore
}

func evaluateTrust(card *Card, ocr *OCROutput, cross *CrossCheckResult, don *DonIdentification, tcg string) (bool, string) {
	if ocr != nil && ocr.CardCode != "" {
		return true, "ocr-extract"
	}
	if cross != nil && cross.Agreed {
		return true, "vision-cross-check"
	}
	if don != nil && don.IsDonCard && don.Confidence >= 0.55 {
		return true, "don-vision"
	}
	if card != nil && card.Confidence >= 90 {
		return true, "haiku-confident"
	}
	if card != nil && card.Code != "" {
		return true, "haiku"
	}
	return false, ""
}
