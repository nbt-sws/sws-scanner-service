package scan

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/anthropic"
	scannerimage "github.com/jatibroski/sws-scanner-service/internal/usecase/image"
)

// QualityRequest is the input for the quality endpoint.
type QualityRequest struct {
	Image string `json:"image" binding:"required"`
	TCG   string `json:"tcg,omitempty"`
}

// QualityResponse is the output of the quality endpoint.
type QualityResponse struct {
	OK      bool        `json:"ok"`
	Quality interface{} `json:"quality,omitempty"`
	Metrics *scannerimage.QualityMetrics `json:"metrics,omitempty"`
	Cached  bool        `json:"cached,omitempty"`
	Hash    string      `json:"hash,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// Quality evaluates image quality with CV metrics and optional Haiku judgement.
func (uc *UseCase) Quality(ctx context.Context, req QualityRequest, userID string) (*QualityResponse, error) {
	req.TCG = strings.ToLower(req.TCG)
	if req.TCG == "" {
		req.TCG = "ygo"
	}

	hash := "q_" + HashImage(req.Image)

	// Cache lookup
	if uc.firestore != nil {
		cached, err := uc.firestore.GetScanCache(ctx, hash)
		if err == nil && cached != nil && cached.Card != nil {
			if q, ok := cached.RawResponse["quality"]; ok {
				if m, ok := cached.RawResponse["metrics"]; ok {
					return &QualityResponse{
						OK:      true,
						Quality: q,
						Metrics: mapToMetrics(m),
						Cached:  true,
						Hash:    hash,
					}, nil
				}
			}
		}
	}

	metrics, err := scannerimage.ComputeQualityMetrics(req.Image)
	if err != nil {
		return &QualityResponse{OK: false, Error: "CV failed: " + err.Error()}, nil
	}

	var quality interface{}
	if uc.anthropic != nil {
		prompt := scannerimage.BuildQualityPrompt(metrics, req.TCG)
		imageBlocks := []anthropic.ContentBlock{imageBlock(req.Image)}
		text, err := uc.anthropic.SendMessage(ctx, prompt, imageBlocks)
		if err != nil {
			return &QualityResponse{OK: false, Error: "AI judge failed: " + err.Error(), Metrics: metrics}, nil
		}
		if err := json.Unmarshal([]byte(extractJSON(text)), &quality); err != nil {
			quality = text
		}
	}

	// Cache persistence
	if userID != "" && uc.firestore != nil {
		payload := map[string]interface{}{
			"quality": metrics,
			"metrics": metrics,
			"tcg":     req.TCG,
			"userId":  userID,
		}
		_ = uc.firestore.PutScanCache(ctx, hash, payload)
	}

	return &QualityResponse{
		OK:      true,
		Quality: quality,
		Metrics: metrics,
		Cached:  false,
		Hash:    hash,
	}, nil
}

func mapToMetrics(v interface{}) *scannerimage.QualityMetrics {
	data, _ := json.Marshal(v)
	var m scannerimage.QualityMetrics
	_ = json.Unmarshal(data, &m)
	return &m
}
