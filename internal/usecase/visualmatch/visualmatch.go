package visualmatch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/anthropic"
	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/vision"
)

// UseCase matches a user photo against reference images or candidate images.
type UseCase struct {
	httpClient      *http.Client
	visionClient    *vision.Client
	anthropicClient *anthropic.Client
}

// NewUseCase creates a visual-match use case.
func NewUseCase(visionClient *vision.Client, anthropicClient *anthropic.Client) *UseCase {
	return &UseCase{
		httpClient:      &http.Client{Timeout: 20 * time.Second},
		visionClient:    visionClient,
		anthropicClient: anthropicClient,
	}
}

// Candidate describes a candidate image for ranking.
type Candidate struct {
	ID       string  `json:"id"`
	ImageURL string  `json:"imageUrl"`
	MatchScore float64 `json:"matchScore"`
	Matched  bool    `json:"matched"`
}

// Request is the input for visual match.
type Request struct {
	Image             string      `json:"image" binding:"required"`
	ReferenceImageURL string      `json:"referenceImageUrl,omitempty"`
	Candidates        []Candidate `json:"candidates,omitempty"`
	HaikuConfirm      bool        `json:"haikuConfirm,omitempty"`
	HaikuConfirmTopN  int         `json:"haikuConfirmTopN,omitempty"`
}

// Response is the visual-match result.
type Response struct {
	OK                bool        `json:"ok"`
	Degraded          bool        `json:"degraded,omitempty"`
	Mode              string      `json:"mode"`
	Confident         bool        `json:"confident,omitempty"`
	BestMatchURL      string      `json:"bestMatchUrl,omitempty"`
	Candidates        []Candidate `json:"candidates,omitempty"`
	BestMatch         *Candidate  `json:"bestMatch,omitempty"`
	HaikuConfirmation *HaikuPick  `json:"haikuConfirmation,omitempty"`
	Labels            []Label     `json:"labels,omitempty"`
	WebEntities       []Entity    `json:"webEntities,omitempty"`
	Counts            Counts      `json:"counts"`
	Reason            string      `json:"reason,omitempty"`
}

// HaikuPick is Haiku's chosen candidate.
type HaikuPick struct {
	MatchID    string  `json:"matchId"`
	Confidence float64 `json:"confidence"`
}

// Label is a Vision label annotation.
type Label struct {
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

// Entity is a Vision web entity.
type Entity struct {
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

// Counts summarises Vision web-detection matches.
type Counts struct {
	Full    int `json:"full"`
	Partial int `json:"partial"`
	Similar int `json:"similar"`
}

// Match runs the visual-match pipeline.
func (uc *UseCase) Match(ctx context.Context, req Request) (*Response, error) {
	b64 := dataURLToRawBase64(req.Image)
	if b64 == "" {
		return nil, fmt.Errorf("image must be base64 or data-URL")
	}

	var visionData *vision.Result
	if uc.visionClient != nil {
		res, err := uc.visionClient.WebDetect(ctx, "", b64, 20)
		if err == nil && res != nil {
			visionData = res
		}
	}

	fullSet, partialSet, similarSet := extractSets(visionData)

	// Mode B — candidate ranking.
	if len(req.Candidates) > 0 {
		ranked := rankCandidates(req.Candidates, fullSet, partialSet, similarSet)
		best := findBest(ranked)

		var haikuConfirmation *HaikuPick
		if req.HaikuConfirm && uc.anthropicClient != nil {
			topN := req.HaikuConfirmTopN
			if topN < 2 || topN > 12 {
				topN = 12
			}
			shortlist := ranked
			if len(ranked) > topN {
				shortlist = ranked[:topN]
			}
			picked, err := uc.haikuConfirmMatch(ctx, b64, shortlist)
			if err == nil && picked != nil {
				haikuConfirmation = picked
				for i := range ranked {
					if ranked[i].ID == picked.MatchID {
						cp := ranked[i]
						cp.Matched = true
						best = &cp
						break
					}
				}
			}
		}

		return &Response{
			OK:                true,
			Degraded:          visionData == nil || !visionData.OK,
			Mode:              "candidate-ranking",
			Candidates:        ranked,
			BestMatch:         best,
			HaikuConfirmation: haikuConfirmation,
			Labels:            labels(visionData),
			WebEntities:       entities(visionData),
			Counts:            counts(fullSet, partialSet, similarSet),
		}, nil
	}

	// Mode A — single URL verification.
	resp := &Response{
		OK:       true,
		Degraded: visionData == nil || !visionData.OK,
		Mode:     "single-verification",
		Counts:   counts(fullSet, partialSet, similarSet),
		Labels:   labels(visionData),
		WebEntities: entities(visionData),
	}
	if req.ReferenceImageURL != "" {
		score := scoreCandidate(req.ReferenceImageURL, fullSet, partialSet, similarSet)
		resp.Confident = score >= 0.7
		if resp.Confident {
			resp.BestMatchURL = req.ReferenceImageURL
		}
	} else {
		for _, u := range append(append(setSlice(fullSet), setSlice(partialSet)...), setSlice(similarSet)...) {
			resp.BestMatchURL = u
			break
		}
	}
	return resp, nil
}

func extractSets(res *vision.Result) (full, partial, similar map[string]bool) {
	full, partial, similar = map[string]bool{}, map[string]bool{}, map[string]bool{}
	if res == nil || res.Web == nil {
		return
	}
	for _, it := range res.Web.FullMatchingImages {
		full[it.URL] = true
	}
	for _, it := range res.Web.PartialMatchingImages {
		partial[it.URL] = true
	}
	for _, it := range res.Web.VisuallySimilarImages {
		similar[it.URL] = true
	}
	return
}

func rankCandidates(candidates []Candidate, fullSet, partialSet, similarSet map[string]bool) []Candidate {
	out := make([]Candidate, len(candidates))
	for i, c := range candidates {
		score := scoreCandidate(c.ImageURL, fullSet, partialSet, similarSet)
		out[i] = Candidate{
			ID:         c.ID,
			ImageURL:   c.ImageURL,
			MatchScore: score,
			Matched:    score >= 0.7,
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].MatchScore > out[j].MatchScore
	})
	return out
}

func findBest(ranked []Candidate) *Candidate {
	for _, c := range ranked {
		if c.Matched {
			return &c
		}
	}
	return nil
}

func scoreCandidate(candidateURL string, fullSet, partialSet, similarSet map[string]bool) float64 {
	if candidateURL == "" {
		return 0
	}
	strip := func(u string) string {
		return strings.Split(u, "?")[0]
	}
	c := strip(candidateURL)
	if fullSet[candidateURL] {
		return 1.0
	}
	for u := range fullSet {
		if strip(u) == c {
			return 1.0
		}
	}
	for u := range partialSet {
		if strip(u) == c {
			return 0.9
		}
	}
	for u := range similarSet {
		if strip(u) == c {
			return 0.85
		}
	}
	cBase := pathBase(candidateURL)
	if cBase == "" {
		return 0
	}
	for u := range fullSet {
		if pathBase(u) == cBase {
			return 0.7
		}
	}
	for u := range partialSet {
		if pathBase(u) == cBase {
			return 0.7
		}
	}
	for u := range similarSet {
		if pathBase(u) == cBase {
			return 0.7
		}
	}
	return 0
}

func pathBase(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	parts := strings.Split(parsed.Path, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func setSlice(s map[string]bool) []string {
	out := make([]string, 0, len(s))
	for u := range s {
		out = append(out, u)
	}
	return out
}

func counts(full, partial, similar map[string]bool) Counts {
	return Counts{Full: len(full), Partial: len(partial), Similar: len(similar)}
}

func labels(res *vision.Result) []Label {
	if res == nil {
		return nil
	}
	// Vision client does not currently expose labelAnnotations in Result.
	return nil
}

func entities(res *vision.Result) []Entity {
	if res == nil || res.Web == nil {
		return nil
	}
	out := make([]Entity, 0, len(res.Web.WebEntities))
	for _, e := range res.Web.WebEntities {
		out = append(out, Entity{Description: e.Description, Score: e.Score})
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func (uc *UseCase) haikuConfirmMatch(ctx context.Context, userPhotoB64 string, shortlist []Candidate) (*HaikuPick, error) {
	if uc.anthropicClient == nil || len(shortlist) == 0 {
		return nil, fmt.Errorf("missing dependencies")
	}
	type fetchedImage struct {
		id        string
		base64    string
		mediaType string
	}
	fetched := make([]fetchedImage, 0, len(shortlist))
	for _, cand := range shortlist {
		imgURL := cand.ImageURL
		if !strings.HasPrefix(imgURL, "http") {
			continue
		}
		b64, mime, err := uc.fetchImageBase64(ctx, imgURL)
		if err != nil || b64 == "" {
			continue
		}
		fetched = append(fetched, fetchedImage{id: cand.ID, base64: b64, mediaType: mime})
	}
	if len(fetched) == 0 {
		return nil, fmt.Errorf("no candidate images fetched")
	}

	blocks := []anthropic.ContentBlock{
		{Type: "text", Text: "PHOTO_FROM_USER (the card you must identify):"},
		{Type: "image", Source: &struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		}{Type: "base64", MediaType: "image/jpeg", Data: userPhotoB64}},
		{Type: "text", Text: "\nCANDIDATES — pick the SAME physical card (same character, same set print, same parallel/variant). Reply with the candidate id number that matches, or -1 if none match."},
	}
	for _, s := range fetched {
		blocks = append(blocks, anthropic.ContentBlock{Type: "text", Text: fmt.Sprintf("\nCandidate id=%s:", s.id)})
		blocks = append(blocks, anthropic.ContentBlock{Type: "image", Source: &struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		}{Type: "base64", MediaType: s.mediaType, Data: s.base64}})
	}
	blocks = append(blocks, anthropic.ContentBlock{Type: "text", Text: "\nAnswer with JSON only: {\"matchId\": <id-or-minus-one>, \"confidence\": <0..1>}"})

	text, err := uc.anthropicClient.SendMessage(ctx, "", blocks)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`\{[^{}]*"matchId"[^{}]*\}`)
	m := re.FindString(text)
	if m == "" {
		return nil, fmt.Errorf("no json in haiku response")
	}
	var parsed struct {
		MatchID    string  `json:"matchId"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(m), &parsed); err != nil {
		return nil, err
	}
	if parsed.MatchID == "" || parsed.MatchID == "-1" {
		return nil, nil
	}
	return &HaikuPick{MatchID: parsed.MatchID, Confidence: parsed.Confidence}, nil
}

func (uc *UseCase) fetchImageBase64(ctx context.Context, imgURL string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := uc.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	mime := "image/jpeg"
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		mime = strings.Split(ct, ";")[0]
	}
	return base64.StdEncoding.EncodeToString(data), mime, nil
}

func dataURLToRawBase64(s string) string {
	if strings.HasPrefix(s, "data:") {
		parts := strings.SplitN(s, ",", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	return s
}


