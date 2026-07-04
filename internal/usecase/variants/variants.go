package variants

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	firebaseinfra "github.com/jatibroski/sws-scanner-service/internal/infrastructure/firebase"
)

// UseCase serves card reference data and variant lookups.
type UseCase struct {
	donCatalog     map[string]interface{}
	cnAnnivCatalog map[string]interface{}
	firestore      *firebaseinfra.Firestore
	loadOnce       sync.Once
}

// NewUseCase creates a variants use case.
func NewUseCase(firestore *firebaseinfra.Firestore) *UseCase {
	return &UseCase{firestore: firestore}
}

func (uc *UseCase) ensureLoaded() {
	uc.loadOnce.Do(func() {
		uc.donCatalog = loadJSON("data/don-pdf-catalog.json")
		uc.cnAnnivCatalog = loadJSON("data/cn-anniv-catalog.json")
	})
}

func loadJSON(path string) map[string]interface{} {
	f, err := os.Open(path)
	if err != nil {
		return map[string]interface{}{"error": fmt.Sprintf("missing catalog: %s", path)}
	}
	defer f.Close()
	var data map[string]interface{}
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return data
}

// DonCatalog returns the DON card PDF catalog.
func (uc *UseCase) DonCatalog() map[string]interface{} {
	uc.ensureLoaded()
	return uc.donCatalog
}

// CNAnnivCatalog returns the Chinese anniversary card catalog.
func (uc *UseCase) CNAnnivCatalog() map[string]interface{} {
	uc.ensureLoaded()
	return uc.cnAnnivCatalog
}

// OPVariantsRequest holds filter parameters.
type OPVariantsRequest struct {
	Code   string `form:"code"`
	Name   string `form:"name"`
	Rarity string `form:"rarity"`
	Type   string `form:"type"`
	Color  string `form:"color"`
	Set    string `form:"set"`
	Lang   string `form:"lang"`
	Sort   string `form:"sort"`
	Limit  int    `form:"limit"`
}

// OPVariantsResponse is the variants lookup result.
type OPVariantsResponse struct {
	OK       bool                     `json:"ok"`
	Count    int                      `json:"count"`
	Variants []map[string]interface{} `json:"variants"`
	Error    string                   `json:"error,omitempty"`
}

// OPVariants searches verified cards matching the filters.
func (uc *UseCase) OPVariants(ctx context.Context, req OPVariantsRequest) *OPVariantsResponse {
	if uc.firestore == nil {
		return &OPVariantsResponse{OK: false, Error: "firestore not initialized"}
	}
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 50
	}

	cards, err := uc.firestore.FindVerifiedCardsWithPHash(ctx, req.Limit*2)
	if err != nil {
		return &OPVariantsResponse{OK: false, Error: err.Error()}
	}

	var variants []map[string]interface{}
	for _, c := range cards {
		if req.Code != "" && !strings.Contains(strings.ToLower(c.Code), strings.ToLower(req.Code)) {
			continue
		}
		if req.Name != "" && !strings.Contains(strings.ToLower(c.NameEn), strings.ToLower(req.Name)) {
			continue
		}
		if req.Rarity != "" && !strings.EqualFold(c.Rarity, req.Rarity) {
			continue
		}
		if req.Type != "" && !strings.EqualFold(c.Type, req.Type) {
			continue
		}
		variants = append(variants, c.Data)
		if len(variants) >= req.Limit {
			break
		}
	}

	return &OPVariantsResponse{
		OK:       true,
		Count:    len(variants),
		Variants: variants,
	}
}

// OPDetailsRequest holds detail parameters.
type OPDetailsRequest struct {
	Code   string `form:"code" binding:"required"`
	Rarity string `form:"rarity"`
	Lang   string `form:"lang"`
}

// OPDetailsResponse is the detail lookup result.
type OPDetailsResponse struct {
	OK      bool                   `json:"ok"`
	Card    map[string]interface{} `json:"card,omitempty"`
	DocKey  string                 `json:"docKey,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// OPDetails returns a single verified card by code and rarity.
func (uc *UseCase) OPDetails(ctx context.Context, req OPDetailsRequest) *OPDetailsResponse {
	if uc.firestore == nil {
		return &OPDetailsResponse{OK: false, Error: "firestore not initialized"}
	}
	key := req.Code
	if req.Rarity != "" {
		key = fmt.Sprintf("%s__%s", req.Code, strings.ReplaceAll(req.Rarity, " ", ""))
	}
	card, err := uc.firestore.GetVerifiedCard(ctx, key)
	if err != nil {
		return &OPDetailsResponse{OK: false, Error: err.Error()}
	}
	return &OPDetailsResponse{
		OK:     true,
		Card:   card.Data,
		DocKey: card.DocKey,
	}
}
