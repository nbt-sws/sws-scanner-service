package scan

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Card is the canonical card identification result.
type Card struct {
	Code       string `json:"code"`
	NameEn     string `json:"nameEn"`
	NameJp     string `json:"nameJp"`
	NameCn     string `json:"nameCn"`
	Rarity     string `json:"rarity"`
	Type       string `json:"type"`
	Color      string `json:"color"`
	Promo      bool   `json:"promo"`
	Confidence int    `json:"confidence"`
	Lang       string `json:"lang"`
	Reasoning  string `json:"reasoning"`
}

// OP rarity table.
var opRarities = map[string]bool{
	"C": true, "UC": true, "R": true, "SR": true, "SEC": true,
	"L": true, "TR": true, "SP": true, "MR": true, "P": true,
	"DON!!": true, "DON!! Gold": true, "DON!! R": true,
	"L★": true, "SR★": true, "SEC★": true, "R★": true,
	"UC★": true, "C★": true,
}

var opCardTypes = map[string]bool{
	"Leader": true, "Character": true, "Event": true, "Stage": true, "DON!!": true,
}

var opCodeRegex = regexp.MustCompile(`(?i)^(OP|ST|EB|PRB)\s*-?\s*(\d{1,2})[\s-]+(\d{1,3})$`)
var opCodeSmushed = regexp.MustCompile(`(?i)^(OP|ST|EB|PRB)(\d{2})(\d{3})$`)
var opPromoRegex = regexp.MustCompile(`(?i)^P\s*-\s*(\d{2,4})$`)

func buildOPPrompt(lang string) string {
	return fmt.Sprintf(`You are a One Piece TCG card identifier. Inspect the full card and the four corner zooms.
Output valid JSON only with these fields: code, nameEn, nameJp, nameCn, rarity, type, color, promo (boolean), confidence (0-100), lang (%s), reasoning.

Card code formats: OPXX-XXX, STXX-XXX, EBXX-XXX, PRB-XX, or P-XXX for promos.
Rarities: C, UC, R, SR, SEC, L, TR, SP, MR, P, DON!!, DON!! Gold, DON!! R, L★, SR★, SEC★, R★, UC★, C★.
Types: Leader, Character, Event, Stage, DON!!.
Detect parallel/star rarity if the rarity symbol has a star above it. Detect SP/TR stamps.`, lang)
}

func buildYGOPrompt(lang string) string {
	return fmt.Sprintf(`You are a Yu-Gi-Oh! OCG card identifier. Inspect the full card and the four corner zooms.
Output valid JSON only with these fields: code, nameEn, nameJp, rarity, type, attribute, level, atk, def, promo (boolean), confidence (0-100), lang (%s), reasoning.

Card code format: SETCODE-LANG### (e.g., LEDE-JP001, MAMA-EN001).
Rarities: N, R, SR, UR, UL, SE, HR, PSE, 20TH, QCSE, QCUR, CR, PGR, OF-PSE, OF-UR, UPR, EXSE, C.
Frame colors map to types: yellow Normal, orange Effect, green Spell, pink Trap, blue Ritual, purple Fusion, white Synchro, black Xyz, half-color Pendulum, dark blue Link.`, lang)
}

// ParseHaikuJSON extracts and validates a card from Haiku response text.
func ParseHaikuJSON(text, tcg string) (*Card, error) {
	text = extractJSON(text)
	var card Card
	if err := json.Unmarshal([]byte(text), &card); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}
	card.Code = normalizeCode(card.Code)

	switch tcg {
	case "op":
		validateOPCard(&card)
	case "ygo":
		validateYGOCard(&card)
	}
	return &card, nil
}

func extractJSON(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

func normalizeCode(code string) string {
	code = strings.TrimSpace(code)
	code = strings.ReplaceAll(code, " ", "")

	if m := opCodeRegex.FindStringSubmatch(code); m != nil {
		prefix := strings.ToUpper(m[1])
		set := m[2]
		num := m[3]
		if len(set) < 2 {
			set = "0" + set
		}
		return fmt.Sprintf("%s%s-%03s", prefix, set, num)
	}
	if m := opCodeSmushed.FindStringSubmatch(code); m != nil {
		return fmt.Sprintf("%s%s-%s", strings.ToUpper(m[1]), m[2], m[3])
	}
	if m := opPromoRegex.FindStringSubmatch(code); m != nil {
		return fmt.Sprintf("P-%s", m[1])
	}
	return code
}

func validateOPCard(card *Card) {
	if !opRarities[strings.TrimSpace(card.Rarity)] {
		card.Rarity = ""
	}
	if !opCardTypes[strings.TrimSpace(card.Type)] {
		card.Type = ""
	}
	if card.Code != "" && !isOPCodeValid(card.Code) {
		card.Code = ""
	}
	if card.Confidence > 100 {
		card.Confidence = 100
	}
	if card.Confidence < 0 {
		card.Confidence = 0
	}
}

func isOPCodeValid(code string) bool {
	if opCodeRegex.MatchString(code) || opCodeSmushed.MatchString(code) || opPromoRegex.MatchString(code) {
		return true
	}
	return strings.EqualFold(code, "DON!!")
}

func validateYGOCard(card *Card) {
	if card.Confidence > 100 {
		card.Confidence = 100
	}
	if card.Confidence < 0 {
		card.Confidence = 0
	}
}
