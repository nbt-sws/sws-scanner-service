package contributions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	firebaseinfra "github.com/jatibroski/sws-scanner-service/internal/infrastructure/firebase"
	"github.com/jatibroski/sws-scanner-service/internal/usecase/scan"
)

const collectionVerifiedCards = "verified_cards"

// UseCase handles community contributions of verified cards and samples.
type UseCase struct {
	storage    *firebaseinfra.Storage
	firestore  *firebaseinfra.Firestore
	firebaseApp *firebaseinfra.App
	httpClient *http.Client
	adminEmails []string
}

// NewUseCase creates a contributions use case.
func NewUseCase(app *firebaseinfra.App, storage *firebaseinfra.Storage, firestore *firebaseinfra.Firestore, adminEmails []string) *UseCase {
	return &UseCase{
		firebaseApp: app,
		storage:     storage,
		firestore:   firestore,
		httpClient:  &http.Client{Timeout: 20 * time.Second},
		adminEmails: adminEmails,
	}
}

// ContributeRequest is the input for contributing a verified card.
type ContributeRequest struct {
	Card          scan.Card `json:"card"`
	TCG           string    `json:"tcg,omitempty"`
	Lang          string    `json:"lang,omitempty"`
	Image         string    `json:"image,omitempty"`
	SampleImageURL string   `json:"sampleImageUrl,omitempty"`
	ScanHash      string    `json:"scanHash,omitempty"`
}

// ContributeResponse is the result of a verified-card contribution.
type ContributeResponse struct {
	OK               bool   `json:"ok"`
	DocKey           string `json:"docKey"`
	SampleImageURL   string `json:"sampleImageUrl,omitempty"`
	WatermarkedSampleURL string `json:"watermarkedSampleUrl,omitempty"`
	OfficialImageURL string `json:"officialImageUrl,omitempty"`
	OfficialSource   string `json:"officialSource,omitempty"`
	Error            string `json:"error,omitempty"`
}

// ContributeSampleRequest is the input for contributing a sample image.
type ContributeSampleRequest struct {
	Image          string `json:"image" binding:"required"`
	Code           string `json:"code" binding:"required"`
	Rarity         string `json:"rarity" binding:"required"`
	Lang           string `json:"lang,omitempty"`
	NameEn         string `json:"nameEn,omitempty"`
	NameJp         string `json:"nameJp,omitempty"`
	Type           string `json:"type,omitempty"`
	Promo          bool   `json:"promo,omitempty"`
	ReplaceExisting bool  `json:"replaceExisting,omitempty"`
	ScanHash       string `json:"scanHash,omitempty"`
	TCG            string `json:"tcg,omitempty"`
}

// ContributeSampleResponse is the result of a sample contribution.
type ContributeSampleResponse struct {
	OK               bool     `json:"ok"`
	Code             string   `json:"code"`
	Rarity           string   `json:"rarity"`
	Lang             string   `json:"lang"`
	SampleImageURL   string   `json:"sampleImageUrl"`
	Docs             []string `json:"docs"`
	ReplaceExisting  bool     `json:"replaceExisting"`
	Admin            bool     `json:"admin"`
	ScanCachePatched bool     `json:"scanCachePatched"`
	Error            string   `json:"error,omitempty"`
}

// Contribute saves a user-verified card record to Firestore and Storage.
func (uc *UseCase) Contribute(ctx context.Context, userID string, req ContributeRequest) (*ContributeResponse, error) {
	if uc.storage == nil || uc.firestore == nil {
		return nil, fmt.Errorf("firebase not initialized")
	}
	code := req.Card.Code
	rarity := req.Card.Rarity
	if code == "" || rarity == "" {
		return nil, fmt.Errorf("missing code or rarity")
	}
	langKey := strings.ToUpper(firstNonEmpty(req.Lang, req.Card.Lang, "JP"))
	rarityKey := cleanRarity(rarity)
	docKey := fmt.Sprintf("%s__%s", code, rarityKey)

	// 1. Upload user scan if provided.
	var userScanURL string
	var userScanHash string
	if req.Image != "" {
		path := samplePath(langKey, code, rarity, req.Card.NameEn, req.Card.NameJp, req.Card.Type)
		url, err := uc.storage.UploadToPath(ctx, path, req.Image, map[string]string{
			"schemaVersion":     "v3-scn104",
			"clientWatermarked": "true",
		})
		if err != nil {
			return nil, fmt.Errorf("upload user scan: %w", err)
		}
		userScanURL = url
		if h, err := scan.AverageHash(req.Image); err == nil {
			userScanHash = h
		}
	}

	// 2. Lookup official details and mirror sample.
	official, err := uc.lookupOfficialDetails(ctx, code)
	var mirrorURL string
	if err == nil && official != nil {
		remote := firstNonEmpty(req.SampleImageURL, official.ImageURL)
		if remote != "" {
			mirrorURL, _ = uc.mirrorRemoteSample(ctx, remote, docKey)
		}
	}

	// 3. Upsert verified_cards.
	record := map[string]interface{}{
		"code":              code,
		"rarity":            rarity,
		"lang":              langKey,
		"tcg":               firstNonEmpty(req.TCG, "op"),
		"nameEn":            req.Card.NameEn,
		"nameJp":            req.Card.NameJp,
		"type":              req.Card.Type,
		"promo":             req.Card.Promo,
		"setCode":           req.Card.Code, // not provided separately in scan.Card
		"sampleImageUrl":    firstNonEmpty(userScanURL, mirrorURL),
		"samples":           map[string]interface{}{langKey: firstNonEmpty(userScanURL, mirrorURL)},
		"officialImageUrl":  mirrorURL,
		"officialName":      nilStr(official, "name"),
		"officialSetName":   nilStr(official, "setName"),
		"officialReleaseDate": nilStr(official, "releaseDate"),
		"officialSource":    nilStr(official, "source"),
		"verifiedBy":        firestore.ArrayUnion(userID),
		"verificationCount": firestore.Increment(1),
		"lastVerifiedAt":    firestore.ServerTimestamp,
	}
	if userScanHash != "" {
		record["perceptualHash"] = userScanHash
	}
	if err := uc.firestore.UpsertVerifiedCard(ctx, docKey, record); err != nil {
		return nil, fmt.Errorf("firestore write: %w", err)
	}

	// 4. Patch scans cache.
	if req.ScanHash != "" {
		_ = uc.firestore.PatchScanCache(ctx, req.ScanHash, map[string]interface{}{
			"card": map[string]interface{}{
				"code":   code,
				"rarity": rarity,
				"lang":   langKey,
			},
			"correctedBy": userID,
			"correctedAt": firestore.ServerTimestamp,
		})
	}

	return &ContributeResponse{
		OK:                   true,
		DocKey:               docKey,
		SampleImageURL:       firstNonEmpty(userScanURL, mirrorURL),
		WatermarkedSampleURL: userScanURL,
		OfficialImageURL:     mirrorURL,
		OfficialSource:       nilStr(official, "source"),
	}, nil
}

// ContributeSample uploads a watermarked sample image.
func (uc *UseCase) ContributeSample(ctx context.Context, userID, userEmail string, req ContributeSampleRequest) (*ContributeSampleResponse, error) {
	if uc.storage == nil || uc.firestore == nil {
		return nil, fmt.Errorf("firebase not initialized")
	}
	langKey := strings.ToUpper(firstNonEmpty(req.Lang, "JP"))
	isAdmin := uc.isAdmin(userEmail)
	rarityKey := cleanRarity(req.Rarity)

	path := samplePath(langKey, req.Code, req.Rarity, req.NameEn, req.NameJp, req.Type)
	if req.NameEn == "" {
		path = fmt.Sprintf("verified_cards/samples/%s__%s__%s__user.jpeg", req.Code, safePath(rarityKey), langKey)
	}

	if !req.ReplaceExisting {
		exists, err := uc.storage.Exists(ctx, path)
		if err == nil && exists {
			return &ContributeSampleResponse{OK: false, Error: "SAMPLE already exists. Pass replaceExisting:true (admin) to overwrite."}, nil
		}
	} else if !isAdmin {
		return &ContributeSampleResponse{OK: false, Error: "Admin-only operation"}, nil
	}

	url, err := uc.storage.UploadToPath(ctx, path, req.Image, map[string]string{
		"schemaVersion":     "v3-scn104",
		"clientWatermarked": "true",
	})
	if err != nil {
		return nil, fmt.Errorf("storage upload: %w", err)
	}

	perRarityKey := fmt.Sprintf("%s__%s", req.Code, rarityKey)
	baseKey := fmt.Sprintf("%s__base", req.Code)
	now := firestore.ServerTimestamp
	sampleSources := map[string]interface{}{langKey: sourceLabel(isAdmin)}

	if err := uc.firestore.UpsertVerifiedCard(ctx, perRarityKey, map[string]interface{}{
		"code":               req.Code,
		"rarity":             req.Rarity,
		"lang":               langKey,
		"sampleImageUrl":     url,
		"samples":            map[string]interface{}{langKey: url},
		"sampleSources":      sampleSources,
		"nameEn":             req.NameEn,
		"nameJp":             req.NameJp,
		"contributedBy":      firestore.ArrayUnion(userID),
		"sampleBackfilledAt": now,
	}); err != nil {
		return nil, fmt.Errorf("firestore write: %w", err)
	}
	_ = uc.firestore.UpsertVerifiedCard(ctx, baseKey, map[string]interface{}{
		"code":               req.Code,
		"rarity":             "base",
		"samples":            map[string]interface{}{langKey: url},
		"sampleSources":      sampleSources,
		"sampleBackfilledAt": now,
	})

	scanCachePatched := false
	if req.ScanHash != "" {
		err := uc.firestore.PatchScanCache(ctx, req.ScanHash, map[string]interface{}{
			"card": map[string]interface{}{
				"code":       req.Code,
				"rarity":     req.Rarity,
				"nameEn":     req.NameEn,
				"nameJp":     req.NameJp,
				"type":       req.Type,
				"promo":      req.Promo,
				"lang":       langKey,
				"tcg":        firstNonEmpty(req.TCG, "op"),
				"confidence": 99,
				"reasoning":  reasoningLabel(isAdmin),
			},
			"correctedBy": userID,
			"correctedAt": now,
		})
		scanCachePatched = err == nil
	}

	return &ContributeSampleResponse{
		OK:               true,
		Code:             req.Code,
		Rarity:           req.Rarity,
		Lang:             langKey,
		SampleImageURL:   url,
		Docs:             []string{perRarityKey, baseKey},
		ReplaceExisting:  req.ReplaceExisting,
		Admin:            isAdmin,
		ScanCachePatched: scanCachePatched,
	}, nil
}

func (uc *UseCase) isAdmin(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, e := range uc.adminEmails {
		if strings.ToLower(strings.TrimSpace(e)) == email {
			return true
		}
	}
	return false
}

type officialDetails struct {
	Name        string `json:"name"`
	SetName     string `json:"setName"`
	ReleaseDate string `json:"releaseDate"`
	Source      string `json:"source"`
	ImageURL    string `json:"imageUrl"`
}

func (uc *UseCase) lookupOfficialDetails(ctx context.Context, code string) (*officialDetails, error) {
	if o, err := uc.fetchOptcgapi(ctx, code); err == nil && o != nil {
		return o, nil
	}
	return uc.fetchApitcg(ctx, code)
}

func (uc *UseCase) fetchOptcgapi(ctx context.Context, code string) (*officialDetails, error) {
	url := fmt.Sprintf("https://optcgapi.com/api/cards/code/%s", url.QueryEscape(code))
	data, err := uc.fetchJSON(ctx, url)
	if err != nil {
		return nil, err
	}
	var card map[string]interface{}
	switch v := data.(type) {
	case []interface{}:
		if len(v) > 0 {
			card, _ = v[0].(map[string]interface{})
		}
	case map[string]interface{}:
		if c, ok := v["cards"].([]interface{}); ok && len(c) > 0 {
			card, _ = c[0].(map[string]interface{})
		} else {
			card = v
		}
	}
	if card == nil {
		return nil, fmt.Errorf("empty response")
	}
	return &officialDetails{
		Name:     stringField(card, "name"),
		SetName:  setName(card),
		ReleaseDate: stringField(card, "release_date"),
		Source:   "optcgapi.com",
		ImageURL: imageURL(card),
	}, nil
}

func (uc *UseCase) fetchApitcg(ctx context.Context, code string) (*officialDetails, error) {
	url := fmt.Sprintf("https://www.apitcg.com/api/one-piece/cards?code=%s", url.QueryEscape(code))
	data, err := uc.fetchJSON(ctx, url)
	if err != nil {
		return nil, err
	}
	var card map[string]interface{}
	switch v := data.(type) {
	case map[string]interface{}:
		if d, ok := v["data"].([]interface{}); ok && len(d) > 0 {
			card, _ = d[0].(map[string]interface{})
		}
	case []interface{}:
		if len(v) > 0 {
			card, _ = v[0].(map[string]interface{})
		}
	}
	if card == nil {
		return nil, fmt.Errorf("empty response")
	}
	return &officialDetails{
		Name:     stringField(card, "name"),
		SetName:  stringField(card, "set"),
		ReleaseDate: stringField(card, "release_date"),
		Source:   "apitcg.com",
		ImageURL: imageURL(card),
	}, nil
}

func (uc *UseCase) fetchJSON(ctx context.Context, url string) (interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := uc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (uc *UseCase) mirrorRemoteSample(ctx context.Context, remoteURL, docKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SwibSwap/14)")
	resp, err := uc.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	ext := "jpg"
	if strings.Contains(remoteURL, ".png") {
		ext = "png"
	}
	contentType := "image/jpeg"
	if ext == "png" {
		contentType = "image/png"
	}
	path := fmt.Sprintf("verified_cards/%s_official.%s", docKey, ext)
	if uc.storage == nil {
		return "", fmt.Errorf("storage not initialized")
	}
	dataURL := fmt.Sprintf("data:%s;base64,%s", contentType, base64.StdEncoding.EncodeToString(buf))
	return uc.storage.UploadToPath(ctx, path, dataURL, map[string]string{
		"mirrored":     "true",
		"source":       remoteURL,
		"schemaVersion": "v3-scn104",
	})
}

func samplePath(lang, code, rarity, nameEn, nameJp, cardType string) string {
	namePart := slugify(nameEn)
	if namePart == "" {
		namePart = slugify(nameJp)
	}
	return fmt.Sprintf("verified_cards/samples/%s_%s_%s_%s_%s.jpeg",
		slugify(lang), slugify(code), slugify(rarity), namePart[:minInt(len(namePart), 32)], shortType(cardType))
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "★", "-star")
	s = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(s, "-")
	s = regexp.MustCompile(`-+$`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`^-+`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`-+`).ReplaceAllString(s, "-")
	return s
}

func safePath(s string) string {
	return regexp.MustCompile(`[^\w\-★]`).ReplaceAllString(s, "_")
}

func shortType(t string) string {
	x := strings.ToLower(t)
	switch {
	case strings.Contains(x, "leader"):
		return "leader"
	case strings.Contains(x, "character"):
		return "char"
	case strings.Contains(x, "event"):
		return "event"
	case strings.Contains(x, "stage"):
		return "stage"
	case strings.Contains(x, "don"):
		return "don"
	}
	s := slugify(t)
	if s == "" {
		return "card"
	}
	return s
}

func cleanRarity(r string) string {
	return regexp.MustCompile(`[\s/]+`).ReplaceAllString(r, "")
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func nilStr(o *officialDetails, field string) string {
	if o == nil {
		return ""
	}
	switch field {
	case "name":
		return o.Name
	case "setName":
		return o.SetName
	case "releaseDate":
		return o.ReleaseDate
	case "source":
		return o.Source
	}
	return ""
}

func stringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key].(string)
	if ok {
		return v
	}
	return ""
}

func setName(m map[string]interface{}) string {
	if s, ok := m["set"].(map[string]interface{}); ok {
		if n, ok := s["name"].(string); ok {
			return n
		}
	}
	return stringField(m, "setName")
}

func imageURL(m map[string]interface{}) string {
	if images, ok := m["images"].(map[string]interface{}); ok {
		if large, ok := images["large"].(string); ok {
			return large
		}
		if small, ok := images["small"].(string); ok {
			return small
		}
	}
	for _, k := range []string{"image_url", "image"} {
		if s, ok := m[k].(string); ok {
			return s
		}
	}
	return ""
}

func sourceLabel(admin bool) string {
	if admin {
		return "admin-replace"
	}
	return "user-contributed"
}

func reasoningLabel(admin bool) string {
	if admin {
		return "Admin REPLACE correction"
	}
	return "User-confirmed SAMPLE contribution"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
