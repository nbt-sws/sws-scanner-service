package scan

import (
	"regexp"
	"strings"

	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/vision"
)

// DonIdentification represents a DON-card rescue result.
type DonIdentification struct {
	IsDonCard     bool     `json:"isDonCard"`
	Confidence    float64  `json:"confidence"`
	FullName      string   `json:"fullName"`
	Variant       string   `json:"variant"`
	SetCode       string   `json:"setCode"`
	SyntheticCode string   `json:"syntheticCode"`
	Rarity        string   `json:"rarity"`
	Tier          string   `json:"tier"`
	OCRSnippet    string   `json:"ocrSnippet"`
	OCRSignals    []string `json:"ocrSignals"`
	OCRLanguage   string   `json:"ocrLanguage"`
	Evidence      []string `json:"evidence"`
}

var (
	donSetRegex   = regexp.MustCompile(`(?i)(PRB|EB|OP|ST)\s*-?\s*(\d{1,2})`)
	variantRegex  = regexp.MustCompile(`(?i)(gold|alt.?art|foil|reprint|manga)`)
	contextRegex  = regexp.MustCompile(`(?i)(one piece|onepiece|optcg|don card|ドン!!カード)`)
)

var donCharacters = []string{
	"Monkey D. Luffy", "Roronoa Zoro", "Nami", "Usopp", "Sanji", "Tony Tony Chopper",
	"Nico Robin", "Franky", "Brook", "Jinbe", "Shanks", "Donquixote Doflamingo",
	"Trafalgar Law", "Kaido", "Charlotte Linlin", "Sabo", "Portgas D. Ace",
}

// IdentifyDonCard attempts to identify a One Piece DON!! card from Vision/OCR signals.
func IdentifyDonCard(web *vision.WebDetection, ocrText string) *DonIdentification {
	out := &DonIdentification{}
	if web == nil && ocrText == "" {
		return out
	}

	corpus := buildCorpus(web)
	out.OCRSnippet = truncate(ocrText, 500)

	ocrResult := ExtractFromOcr(ocrText, "")
	out.OCRSignals = ocrResult.Signals
	out.OCRLanguage = ocrResult.Language

	// Confirm DON context
	if !ocrResult.IsDonCard && !corpusSaysDonCardWithOpContext(corpus) {
		return out
	}

	name := ""
	if ocrResult.CharacterName != "" {
		name = ocrResult.CharacterName
		out.Tier = "ocr-text+ocr-name"
		out.Confidence = 0.85
		out.Evidence = append(out.Evidence, "ocr-name")
	} else {
		name = extractNameFromCorpus(corpus)
		if name != "" {
			out.Tier = "ocr-text+web-name"
			out.Confidence = 0.65
			out.Evidence = append(out.Evidence, "web-name")
		} else {
			out.Tier = "web-corpus-op-context"
			out.Confidence = 0.25
		}
	}

	if ocrResult.IsDonCard {
		out.IsDonCard = true
		if out.Confidence < 0.55 {
			out.Confidence = 0.55
		}
		out.Evidence = append(out.Evidence, "ocr-don-marker")
	}

	variant := detectVariant(corpus)
	setCode := extractSetCode(corpus)

	if name != "" {
		out.FullName = name
		out.SyntheticCode = name + " Don Card"
		out.Confidence += 0.10
		out.Evidence = append(out.Evidence, "name")
	}
	if setCode != "" {
		out.SetCode = setCode
		out.Confidence += 0.05
		out.Evidence = append(out.Evidence, "set-code")
	}
	if variant != "" {
		out.Variant = variant
		out.Confidence += 0.03
		out.Evidence = append(out.Evidence, "variant")
	}

	if out.Confidence > 0.99 {
		out.Confidence = 0.99
	}

	out.Rarity = deriveDonRarity(out.Variant)
	return out
}

func corpusSaysDonCardWithOpContext(corpus []string) bool {
	don := false
	op := false
	for _, s := range corpus {
		lower := strings.ToLower(s)
		if strings.Contains(lower, "don") || strings.Contains(lower, "ドン") {
			don = true
		}
		if contextRegex.MatchString(s) {
			op = true
		}
		if don && op {
			return true
		}
	}
	return false
}

func buildCorpus(web *vision.WebDetection) []string {
	var corpus []string
	if web == nil {
		return corpus
	}
	for _, l := range web.BestGuessLabels {
		corpus = append(corpus, l.Label)
	}
	for _, e := range web.WebEntities {
		corpus = append(corpus, e.Description)
	}
	for _, p := range web.PagesWithMatchingImages {
		corpus = append(corpus, p.URL)
	}
	for _, i := range web.FullMatchingImages {
		corpus = append(corpus, i.URL)
	}
	for _, i := range web.PartialMatchingImages {
		corpus = append(corpus, i.URL)
	}
	return corpus
}

func extractNameFromCorpus(corpus []string) string {
	for _, s := range corpus {
		lower := strings.ToLower(s)
		for _, name := range donCharacters {
			if strings.Contains(lower, strings.ToLower(name)) {
				return name
			}
		}
	}
	return ""
}

func detectVariant(corpus []string) string {
	for _, s := range corpus {
		if m := variantRegex.FindStringSubmatch(s); m != nil {
			return strings.ToLower(m[1])
		}
	}
	return "regular"
}

func extractSetCode(corpus []string) string {
	for _, s := range corpus {
		if m := donSetRegex.FindStringSubmatch(s); m != nil {
			return strings.ToUpper(m[1]) + "-" + m[2]
		}
	}
	return ""
}

func deriveDonRarity(variant string) string {
	switch variant {
	case "gold":
		return "DON!! Gold"
	case "altart", "foil":
		return "DON!! R"
	default:
		return "DON!!"
	}
}
