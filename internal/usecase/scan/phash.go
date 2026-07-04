package scan

import (
	"context"
	"sort"

	firebaseinfra "github.com/jatibroski/sws-scanner-service/internal/infrastructure/firebase"
)

// PHashMatch represents a single perceptual-hash match.
type PHashMatch struct {
	DocKey      string                 `json:"docKey"`
	Code        string                 `json:"code"`
	Rarity      string                 `json:"rarity"`
	ImageURL    string                 `json:"imageUrl"`
	Distance    int                    `json:"distance"`
	Confidence  float64                `json:"confidence"`
	FromDb      bool                   `json:"fromDb"`
	Watermarked bool                   `json:"watermarked"`
	Data        map[string]interface{} `json:"data"`
}

// PHashResponse is the output of a pHash lookup.
type PHashResponse struct {
	OK       bool         `json:"ok"`
	Matches  []PHashMatch `json:"matches"`
	UserHash string       `json:"userHash"`
	Scanned  int          `json:"scanned"`
	Degraded bool         `json:"degraded,omitempty"`
	Reason   string       `json:"reason,omitempty"`
}

// PHashLookup finds visually similar verified cards.
func (uc *UseCase) PHashLookup(ctx context.Context, image string) (*PHashResponse, error) {
	if uc.firestore == nil {
		return &PHashResponse{OK: true, Matches: []PHashMatch{}, Degraded: true, Reason: "firestore not initialized"}, nil
	}

	userHash, err := AverageHash(image)
	if err != nil {
		return &PHashResponse{OK: true, Matches: []PHashMatch{}, Degraded: true, Reason: err.Error()}, nil
	}

	candidates, err := uc.firestore.FindVerifiedCardsWithPHash(ctx, 5000)
	if err != nil {
		return &PHashResponse{OK: false, Reason: err.Error()}, nil
	}

	var matches []PHashMatch
	for _, v := range candidates {
		if v.Phash == "" {
			continue
		}
		dist := HammingDistance(userHash, v.Phash)
		if dist <= 18 {
			conf := 1.0 - float64(dist)/24.0
			if conf < 0 {
				conf = 0
			}
			matches = append(matches, PHashMatch{
				DocKey:      v.DocKey,
				Code:        v.Code,
				Rarity:      v.Rarity,
				ImageURL:    selectVerifiedImageURL(v, ""),
				Distance:    dist,
				Confidence:  conf,
				FromDb:      true,
				Watermarked: true,
				Data:        v.Data,
			})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Distance < matches[j].Distance
	})
	if len(matches) > 12 {
		matches = matches[:12]
	}

	return &PHashResponse{
		OK:       true,
		Matches:  matches,
		UserHash: userHash,
		Scanned:  len(candidates),
	}, nil
}

// Ensure interface compatibility.
var _ = firebaseinfra.ScanCacheDoc{}
