package utility

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// UseCase provides small utility operations.
type UseCase struct {
	httpClient *http.Client
}

// NewUseCase creates a utility use case.
func NewUseCase() *UseCase {
	return &UseCase{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ProxyImage fetches an external image and returns its bytes + content type.
func (uc *UseCase) ProxyImage(url string) ([]byte, string, error) {
	if url == "" {
		return nil, "", fmt.Errorf("url required")
	}
	resp, err := uc.httpClient.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	return data, contentType, nil
}

// LookupRequest holds filename lookup parameters.
type LookupRequest struct {
	Lang     string `form:"lang"`
	SetCode  string `form:"setCode"`
	Rarity   string `form:"rarity"`
	CardName string `form:"cardName"`
	Type     string `form:"type"`
}

// LookupResponse is the lookup result.
type LookupResponse struct {
	OK           string        `json:"ok"`
	Pattern      string        `json:"pattern"`
	Count        int           `json:"count"`
	TotalScanned int           `json:"totalScanned"`
	Matches      []LookupMatch `json:"matches"`
}

// LookupMatch represents a single filename match.
type LookupMatch struct {
	Filename string  `json:"filename"`
	URL      string  `json:"url"`
	Source   string  `json:"source"`
	Score    int     `json:"score"`
	Ratio    float64 `json:"ratio"`
}

// LookupByFilename searches local static catalogs by filename pattern.
func (uc *UseCase) LookupByFilename(req LookupRequest) *LookupResponse {
	pattern := buildPattern(req)
	patternStr := strings.Join(pattern, "_")

	var all []LookupMatch
	all = append(all, scanDir("static/don-pdf-wm", "/don-pdf-wm", pattern)...)
	all = append(all, scanDir("static/cn-anniv", "/cn-anniv", pattern)...)
	all = append(all, scanDir("static/don-pdf", "/don-pdf", pattern)...)

	sort.Slice(all, func(i, j int) bool {
		if all[i].Ratio != all[j].Ratio {
			return all[i].Ratio > all[j].Ratio
		}
		return all[i].Score > all[j].Score
	})

	top := all
	if len(top) > 12 {
		top = top[:12]
	}

	return &LookupResponse{
		OK:           "true",
		Pattern:      patternStr,
		Count:        len(top),
		TotalScanned: len(all),
		Matches:      top,
	}
}

func buildPattern(req LookupRequest) []string {
	return []string{
		slugifyPart(req.Lang),
		slugifyPart(req.SetCode),
		slugifyPart(req.Rarity),
		slugifyPart(req.CardName),
		shortType(req.Type),
	}
}

func slugifyPart(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "★", "-star")
	re := regexp.MustCompile(`[^a-z0-9-]+`)
	s = re.ReplaceAllString(s, "-")
	s = regexp.MustCompile(`-+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

func shortType(t string) string {
	t = strings.ToLower(t)
	switch {
	case strings.Contains(t, "leader"):
		return "leader"
	case strings.Contains(t, "character"):
		return "char"
	case strings.Contains(t, "event"):
		return "event"
	case strings.Contains(t, "stage"):
		return "stage"
	case strings.Contains(t, "don"):
		return "don"
	}
	return slugifyPart(t)
}

func scanDir(absDir, urlPrefix string, pattern []string) []LookupMatch {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil
	}
	var out []LookupMatch
	extRegex := regexp.MustCompile(`(?i)\.(jpe?g|png|webp)$`)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !extRegex.MatchString(name) {
			continue
		}
		m := matchesPattern(name, pattern)
		if m == nil {
			continue
		}
		source := "don-pdf"
		if strings.Contains(urlPrefix, "cn-anniv") {
			source = "cn-anniv-pdf"
		} else if strings.Contains(urlPrefix, "don-pdf-wm") {
			source = "don-pdf-wm"
		}
		out = append(out, LookupMatch{
			Filename: name,
			URL:      fmt.Sprintf("%s/%s", urlPrefix, filepath.ToSlash(name)),
			Source:   source,
			Score:    m.score,
			Ratio:    m.ratio,
		})
	}
	return out
}

type matchScore struct {
	score int
	ratio float64
}

func matchesPattern(filename string, pattern []string) *matchScore {
	base := strings.ToLower(strings.TrimSuffix(filename, filepath.Ext(filename)))
	segs := strings.Split(base, "_")
	required := 0
	score := 0
	for _, p := range pattern {
		if p == "" || p == "*" {
			continue
		}
		required++
		hit := false
		for _, s := range segs {
			if s == p {
				hit = true
				break
			}
		}
		if !hit && strings.Contains(base, p) {
			hit = true
		}
		if hit {
			score++
		}
	}
	if required == 0 {
		return nil
	}
	ratio := float64(score) / float64(required)
	if ratio < 0.6 {
		return nil
	}
	return &matchScore{score: score, ratio: ratio}
}
