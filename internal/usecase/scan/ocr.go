package scan

import (
	"regexp"
	"strings"
)

// OCROutput holds signals extracted from Google Vision OCR.
type OCROutput struct {
	CardCode      string   `json:"cardCode"`
	IsDonCard     bool     `json:"isDonCard"`
	CharacterName string   `json:"characterName"`
	Language      string   `json:"language"`
	PowerValue    string   `json:"powerValue"`
	Signals       []string `json:"signals"`
	OCRSnippet    string   `json:"ocrSnippet"`
}

var (
	donMarkerJP     = regexp.MustCompile(`(?i)ドン!!カード`)
	donMarkerEN     = regexp.MustCompile(`(?i)DON!!\s*CARD`)
	donMarkerCN     = regexp.MustCompile(`(?i)咚!!卡`)
	powerRegex      = regexp.MustCompile(`(?i)\+\s*1000`)
	negativeContext = regexp.MustCompile(`(?i)(Blocker|On Play|登場時|Main|Counter|阻挡|When this|Startup)`)
)

var characterNames = []string{
	"Monkey D. Luffy", "Roronoa Zoro", "Nami", "Usopp", "Sanji", "Tony Tony Chopper",
	"Nico Robin", "Franky", "Brook", "Jinbe", "Shanks", "Marshall D. Teach",
	"Donquixote Doflamingo", "Trafalgar Law", "Eustass Kid", "Kaido", "Charlotte Linlin",
	"Sabo", "Portgas D. Ace", "Gol D. Roger", "Silvers Rayleigh", "Boa Hancock",
	"Dracule Mihawk", "Crocodile", "Enel", "Rob Lucci", "Kuzan", "Sakazuki",
	"Borsalino", "Issho", "Ryokugyu", "Smoker", "Koby", "Helmeppo",
}

// ExtractFromOcr parses Vision OCR text for One Piece signals.
func ExtractFromOcr(ocrText, langHint string) *OCROutput {
	if ocrText == "" {
		return &OCROutput{}
	}
	out := &OCROutput{OCRSnippet: truncate(ocrText, 500)}

	lang := langHint
	if lang == "" {
		lang = detectLanguage(ocrText)
	}
	out.Language = lang

	// Card code extraction
	if code := extractCardCode(ocrText); code != "" {
		out.CardCode = code
		out.Signals = append(out.Signals, "code:"+code)
	}

	// DON card detection
	don := donMarkerJP.MatchString(ocrText) || donMarkerEN.MatchString(ocrText) || donMarkerCN.MatchString(ocrText)
	if don && !negativeContext.MatchString(ocrText) {
		out.IsDonCard = true
		out.Signals = append(out.Signals, "don-marker-"+strings.ToLower(lang))
	}

	// Character name extraction
	if name := extractCharacterName(ocrText); name != "" {
		out.CharacterName = name
		out.Signals = append(out.Signals, "name:"+name)
	}

	// Power value
	if powerRegex.MatchString(ocrText) {
		out.PowerValue = "+1000"
		out.Signals = append(out.Signals, "power:+1000")
	}

	return out
}

func extractCardCode(text string) string {
	if m := opCodeRegex.FindStringSubmatch(text); m != nil {
		return normalizeCode(strings.ToUpper(m[0]))
	}
	if m := opCodeSmushed.FindStringSubmatch(text); m != nil {
		return normalizeCode(strings.ToUpper(m[0]))
	}
	if m := opPromoRegex.FindStringSubmatch(text); m != nil {
		return normalizeCode(strings.ToUpper(m[0]))
	}
	return ""
}

func extractCharacterName(text string) string {
	lower := strings.ToLower(text)
	for _, name := range characterNames {
		if strings.Contains(lower, strings.ToLower(name)) {
			return name
		}
	}
	return ""
}

func detectLanguage(text string) string {
	hasKana := regexp.MustCompile(`[\x{3040}-\x{309F}\x{30A0}-\x{30FF}]`).MatchString(text)
	hasCJK := regexp.MustCompile(`[\x{4E00}-\x{9FFF}]`).MatchString(text)
	hasLatin := regexp.MustCompile(`[A-Za-z]`).MatchString(text)

	if hasKana {
		return "JP"
	}
	if hasCJK && !hasKana {
		return "CN"
	}
	if hasLatin {
		return "EN"
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
