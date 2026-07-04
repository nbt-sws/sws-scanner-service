package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/anthropic"
	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/ebay"
	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/vision"
)

const fxEndpoint = "https://api.frankfurter.app/latest"

// UseCase provides pricing and FX operations.
type UseCase struct {
	httpClient     *http.Client
	ebayClient     *ebay.Client
	visionClient   *vision.Client
	anthropicClient *anthropic.Client
}

// NewUseCase creates a pricing use case.
func NewUseCase(ebayClient *ebay.Client, visionClient *vision.Client, anthropicClient *anthropic.Client) *UseCase {
	return &UseCase{
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		ebayClient:      ebayClient,
		visionClient:    visionClient,
		anthropicClient: anthropicClient,
	}
}

// FXResponse mirrors the Frankfurter API response.
type FXResponse struct {
	Amount float64            `json:"amount"`
	Base   string             `json:"base"`
	Date   string             `json:"date"`
	Rates  map[string]float64 `json:"rates"`
}

// FX returns the latest FX rates for a base currency.
func (uc *UseCase) FX(ctx context.Context, base string) (*FXResponse, error) {
	if base == "" {
		base = "USD"
	}
	url := fmt.Sprintf("%s?base=%s", fxEndpoint, base)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := uc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var fx FXResponse
	if err := json.NewDecoder(resp.Body).Decode(&fx); err != nil {
		return nil, err
	}
	return &fx, nil
}

// PriceResult is the pricing response.
type PriceResult struct {
	OK             bool                   `json:"ok"`
	Query          string                 `json:"query,omitempty"`
	TotalSold      int                    `json:"totalSold"`
	Tiers          map[string]*Tier       `json:"tiers"`
	TierOrder      []string               `json:"tierOrder"`
	Overall        *Overall               `json:"overall,omitempty"`
	Mercari        map[string]string      `json:"mercari"`
	FallbackLinks  map[string]string      `json:"fallbackLinks"`
	CanonicalQuery string                 `json:"canonicalQuery"`
	Source         string                 `json:"source"`
	Diagnostics    map[string]interface{} `json:"diagnostics,omitempty"`
	Error          string                 `json:"error,omitempty"`
}

// Tier groups listings by condition.
type Tier struct {
	Count    int           `json:"count"`
	Median   float64       `json:"median"`
	Lowest   float64       `json:"lowest"`
	Highest  float64       `json:"highest"`
	Avg      float64       `json:"avg"`
	LastSold *SoldSnapshot `json:"lastSold,omitempty"`
	Items    []ebay.Item   `json:"items"`
}

// Overall aggregates all listings.
type Overall struct {
	Count    int           `json:"count"`
	Median   float64       `json:"median"`
	Lowest   float64       `json:"lowest"`
	Highest  float64       `json:"highest"`
	LastSold *SoldSnapshot `json:"lastSold,omitempty"`
	Items    []ebay.Item   `json:"items"`
}

// SoldSnapshot is the most recent sold listing.
type SoldSnapshot struct {
	Date    string  `json:"date"`
	PriceUSD float64 `json:"priceUSD"`
}

var rarityEnglish = map[string]string{
	"C": "Common", "UC": "Uncommon", "R": "Rare", "SR": "Super Rare", "SEC": "Secret Rare",
	"L": "Leader", "TR": "Treasure Rare", "SP": "Special SP", "P": "Promo",
	"DON!!": "DON", "DON!! Gold": "Don Gold Parallel", "DON!! R": "Don Foil",
	"MR": "Manga Alt Art",
	"L★":   "Leader Alt Art",
	"SR★":  "Super Rare Alt Art",
	"SEC★": "Secret Rare Alt Art",
	"R★":   "Rare Alt Art",
	"UC★":  "Uncommon Alt Art",
	"C★":   "Common Alt Art",
	"Anniversary Promo": "Anniversary",
	"N": "Common", "UR": "Ultra Rare", "UL": "Ultimate Rare", "SE": "Secret Rare",
	"HR": "Holographic Rare", "PSE": "Prismatic Secret Rare", "20TH": "20th Secret Rare",
	"QCSE": "Quarter Century Secret Rare", "QCUR": "Quarter Century Ultra Rare",
	"CR": "Collectors Rare", "PGR": "Premium Gold Rare",
	"OF-PSE": "Overframe Prismatic Secret", "OF-UR": "Overframe Ultra Rare",
	"UPR": "Ultra Parallel Rare", "EXSE": "Extra Secret Rare",
}

var langEnglish = map[string]string{
	"JP": "Japanese", "EN": "English", "CN": "Chinese", "AE": "Asian English",
}

func rarityToEnglish(r string) string {
	if r == "" {
		return ""
	}
	if v, ok := rarityEnglish[r]; ok {
		return v
	}
	stripped := strings.TrimSpace(strings.ReplaceAll(r, "★", ""))
	if v, ok := rarityEnglish[stripped]; ok {
		return v
	}
	return stripped
}

func langToEnglish(l string) string {
	v, ok := langEnglish[strings.ToUpper(l)]
	if ok {
		return v
	}
	return l
}

func buildQuery(code, englishName, rarityEN, setQuery, cardType, langEN string) string {
	isSyntheticDon := regexp.MustCompile(`^[\w\s.-]+\s+Don\s+Card$`).MatchString(code)
	if isSyntheticDon {
		cleanRarity := strings.TrimSpace(regexp.MustCompile(`(?i)^don\s*`).ReplaceAllString(strings.ReplaceAll(rarityEN, "!!", ""), ""))
		parts := []string{}
		if code != "" {
			parts = append(parts, code)
		}
		if cleanRarity != "" {
			parts = append(parts, cleanRarity)
		}
		if setQuery != "" {
			parts = append(parts, setQuery)
		}
		q := strings.Join(parts, " ")
		if langEN != "" {
			q += fmt.Sprintf(" - One piece %s", langEN)
		}
		return q
	}
	parts := []string{}
	if englishName != "" {
		parts = append(parts, englishName)
	}
	if code != "" {
		parts = append(parts, code)
	}
	if rarityEN != "" {
		parts = append(parts, rarityEN)
	}
	if setQuery != "" {
		parts = append(parts, setQuery)
	}
	if cardType != "" {
		parts = append(parts, cardType)
	}
	q := strings.Join(parts, " ")
	if langEN != "" {
		q += fmt.Sprintf(" - One piece %s", langEN)
	}
	return q
}

func buildStrategies(code, englishName, rarityEN, setQuery, cardType, langEN string) []string {
	primary := buildQuery(code, englishName, rarityEN, setQuery, cardType, langEN)
	base := []string{
		primary,
		buildQuery("", englishName, rarityEN, setQuery, "", ""),
		buildQuery("", englishName, rarityEN, "", "", ""),
		buildQuery("", englishName, "", "", "", langEN),
		buildQuery("", englishName, code, "", "", ""),
		englishName,
		code,
	}
	seen := map[string]bool{}
	uniq := []string{}
	for _, q := range base {
		q = strings.TrimSpace(q)
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		uniq = append(uniq, q)
	}
	strategies := []string{}
	for _, q := range uniq[:minInt(len(uniq), 2)] {
		strategies = append(strategies, q+" Near Mint")
	}
	strategies = append(strategies, uniq...)
	return strategies
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var titleNormRE = regexp.MustCompile(`[\s\-_/]+`)

func titleNorm(s string) string {
	return titleNormRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "")
}

var gradedTitlePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)psa\s*1?0`),
	regexp.MustCompile(`(?i)bgs\s*1?0`),
	regexp.MustCompile(`(?i)cgc\s*1?0`),
	regexp.MustCompile(`(?i)ars\s*1?0`),
	regexp.MustCompile(`(?i)sgc\s*1?0`),
}

func passesTitleMatch(title, code, englishName, rarityEN string, isGradedTier bool) bool {
	tn := titleNorm(title)
	if tn == "" {
		return false
	}
	// Synthetic DON codes
	if regexp.MustCompile(`^[\w\s.-]+\s+Don\s+Card$`).MatchString(code) {
		charName := strings.TrimSpace(regexp.MustCompile(`(?i)\s+Don\s+Card\s*$`).ReplaceAllString(code, ""))
		nameN := titleNorm(charName)
		return nameN != "" && strings.Contains(tn, nameN) && strings.Contains(strings.ToLower(title), "don")
	}
	// Strict: code in title
	codeN := titleNorm(code)
	if codeN != "" && strings.Contains(tn, codeN) {
		return true
	}
	// Loose: name + (rarity | graded tier)
	nameN := titleNorm(englishName)
	if nameN == "" || !strings.Contains(tn, nameN) {
		return false
	}
	if isGradedTier {
		for _, re := range gradedTitlePatterns {
			if re.MatchString(title) {
				return true
			}
		}
		return false
	}
	rarityN := titleNorm(rarityEN)
	return rarityN != "" && strings.Contains(tn, rarityN)
}

var codeRE = regexp.MustCompile(`(?i)\b(OP|ST|EB|PRB|P)\s*-?\s*0?(\d{1,2})\s*-?\s*(\d{1,3})\b`)

func extractCodes(title string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range codeRE.FindAllStringSubmatch(title, -1) {
		prefix := strings.ToUpper(m[1])
		setNum := fmt.Sprintf("%02s", m[2])
		cardNum := fmt.Sprintf("%03s", m[3])
		code := fmt.Sprintf("%s%s-%s", prefix, setNum, cardNum)
		if !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	return out
}

func normalizeCode(code string) string {
	m := regexp.MustCompile(`(?i)^([A-Z]+)\s*-?\s*0?(\d{1,2})\s*-?\s*(\d{1,3})$`).FindStringSubmatch(code)
	if m == nil {
		return ""
	}
	return fmt.Sprintf("%s%02s-%s", strings.ToUpper(m[1]), m[2], fmt.Sprintf("%03s", m[3]))
}

func filterByCardCode(items []ebay.Item, searchedCode string) []ebay.Item {
	norm := normalizeCode(searchedCode)
	if norm == "" {
		return items
	}
	var out []ebay.Item
	for _, it := range items {
		codes := extractCodes(it.Title)
		if len(codes) == 0 {
			out = append(out, it)
			continue
		}
		for _, c := range codes {
			if c == norm {
				out = append(out, it)
				break
			}
		}
	}
	return out
}

var competingRarityTokens = map[string][]string{
	"Rare Alternate Art":         {"TREASURE", "SECRET RARE", "SUPER RARE", "PROMO"},
	"Super Rare Alternate Art":   {"TREASURE", "SECRET RARE"},
	"Secret Rare Alternate Art":  {"TREASURE", "SUPER RARE"},
	"Common Alternate Art":       {"TREASURE", "SECRET RARE", "SUPER RARE", "RARE ", "LEADER"},
	"Uncommon Alternate Art":     {"TREASURE", "SECRET RARE", "SUPER RARE", "RARE ", "LEADER"},
	"Leader Alternate Art":       {"TREASURE", "SECRET RARE", "SUPER RARE"},
	"Common":                     {"TREASURE", "SECRET", "SUPER RARE", "RARE", "LEADER", "PROMO"},
	"Uncommon":                   {"TREASURE", "SECRET", "SUPER RARE", "RARE ", "LEADER"},
	"Rare":                       {"TREASURE", "SECRET RARE", "SUPER RARE", "ALTERNATE ART"},
	"Super Rare":                 {"TREASURE", "SECRET RARE", "ALTERNATE ART"},
	"Secret Rare":                {"TREASURE", "SUPER RARE", "ALTERNATE ART"},
	"Leader":                     {"TREASURE", "SECRET RARE", "SUPER RARE", "ALTERNATE ART"},
}

func filterByRarityTokens(items []ebay.Item, rarityEN, code string) []ebay.Item {
	if len(items) == 0 {
		return items
	}
	competing, ok := competingRarityTokens[rarityEN]
	if !ok {
		return items
	}
	search := strings.ToUpper(rarityEN)
	var out []ebay.Item
	for _, it := range items {
		title := strings.ToUpper(it.Title)
		if code != "" && strings.Contains(title, strings.ToUpper(code)) {
			out = append(out, it)
			continue
		}
		ok := true
		for _, tok := range competing {
			if strings.Contains(search, tok) {
				continue
			}
			if strings.Contains(title, tok) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, it)
		}
	}
	return out
}

func classifyCondition(title string) string {
	t := " " + strings.ToUpper(title) + " "
	if regexp.MustCompile(`[^A-Z]PSA[\s.-]*10[^0-9]`).MatchString(t) {
		return "PSA 10"
	}
	if regexp.MustCompile(`[^A-Z]BGS[\s.-]*10\s*BL`).MatchString(t) {
		return "BGS 10 BL"
	}
	if regexp.MustCompile(`[^A-Z]BGS[\s.-]*10[^0-9]`).MatchString(t) {
		return "BGS 10"
	}
	if regexp.MustCompile(`[^A-Z]CGC[\s.-]*10[^0-9]`).MatchString(t) {
		return "CGC 10"
	}
	if regexp.MustCompile(`[^A-Z]ARS[\s.-]*10[^0-9]`).MatchString(t) {
		return "ARS 10"
	}
	if regexp.MustCompile(`\b(PSA|BGS|CGC|ARS)\b[\s.-]*\d`).MatchString(t) {
		return "Lower grades"
	}
	if regexp.MustCompile(`\b(GMA|AGS|TAG|HGA|SGC)\b[\s.-]*\d`).MatchString(t) {
		return "Lower grades"
	}
	return "Raw"
}

var knownTiersOrder = []string{
	"PSA 10", "BGS 10 BL", "BGS 10", "CGC 10", "ARS 10",
	"Lower grades", "Raw",
}

const deviationThreshold = 0.20

func filterPriceDeviations(items []ebay.Item) []ebay.Item {
	if len(items) < 3 {
		return items
	}
	prices := []float64{}
	for _, it := range items {
		if it.PriceUSD > 0 {
			prices = append(prices, it.PriceUSD)
		}
	}
	if len(prices) < 3 {
		return items
	}
	sort.Float64s(prices)
	median := prices[len(prices)/2]
	if median <= 0 {
		return items
	}
	lo := median * (1 - deviationThreshold)
	hi := median * (1 + deviationThreshold)
	var out []ebay.Item
	for _, it := range items {
		if it.PriceUSD >= lo && it.PriceUSD <= hi {
			out = append(out, it)
		}
	}
	return out
}

func groupByCondition(items []ebay.Item) (map[string]*Tier, []string) {
	grouped := map[string][]ebay.Item{}
	for _, it := range items {
		cond := it.ConditionTier
		if cond == "" {
			cond = classifyCondition(it.Title)
		}
		grouped[cond] = append(grouped[cond], it)
	}
	tiers := map[string]*Tier{}
	for cond, rawList := range grouped {
		list := filterPriceDeviations(rawList)
		sorted := make([]ebay.Item, len(list))
		copy(sorted, list)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].SoldDate > sorted[j].SoldDate
		})
		prices := []float64{}
		for _, it := range list {
			if it.PriceUSD > 0 {
				prices = append(prices, it.PriceUSD)
			}
		}
		if len(prices) == 0 {
			continue
		}
		sort.Float64s(prices)
		var lastSold *SoldSnapshot
		if len(sorted) > 0 && sorted[0].SoldDate != "" {
			lastSold = &SoldSnapshot{Date: sorted[0].SoldDate, PriceUSD: sorted[0].PriceUSD}
		}
		cap := 5
		if len(sorted) < cap {
			cap = len(sorted)
		}
		tiers[cond] = &Tier{
			Count:    len(prices),
			Median:   prices[len(prices)/2],
			Lowest:   prices[0],
			Highest:  prices[len(prices)-1],
			Avg:      avg(prices),
			LastSold: lastSold,
			Items:    sorted[:cap],
		}
	}
	present := map[string]bool{}
	for k := range tiers {
		present[k] = true
	}
	order := []string{}
	for _, t := range knownTiersOrder {
		if present[t] {
			order = append(order, t)
		}
	}
	for k := range tiers {
		if !presentIn(k, knownTiersOrder) {
			order = append(order, k)
		}
	}
	return tiers, order
}

func presentIn(s string, arr []string) bool {
	for _, a := range arr {
		if a == s {
			return true
		}
	}
	return false
}

func avg(nums []float64) float64 {
	if len(nums) == 0 {
		return 0
	}
	sum := 0.0
	for _, n := range nums {
		sum += n
	}
	return math.Round((sum/float64(len(nums)))*100) / 100
}

func buildMercariUrls(nameJp, nameEn, code, rarity string) map[string]string {
	parts := []string{}
	if nameJp != "" {
		parts = append(parts, nameJp)
	} else if nameEn != "" {
		parts = append(parts, nameEn)
	}
	if code != "" {
		parts = append(parts, code)
	}
	if rarity != "" {
		parts = append(parts, rarity)
	}
	keyword := strings.Join(parts, " ")
	return map[string]string{
		"onSaleUrl": "https://jp.mercari.com/search?keyword=" + url.QueryEscape(keyword) + "&category_id=1259&status=on_sale",
		"soldUrl":   "https://jp.mercari.com/search?keyword=" + url.QueryEscape(keyword) + "&category_id=1259&status=sold_out",
	}
}

func calcOverall(items []ebay.Item) *Overall {
	prices := []float64{}
	for _, it := range items {
		if it.PriceUSD > 0 {
			prices = append(prices, it.PriceUSD)
		}
	}
	if len(prices) == 0 {
		return nil
	}
	sort.Float64s(prices)
	sorted := make([]ebay.Item, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].SoldDate > sorted[j].SoldDate
	})
	var lastSold *SoldSnapshot
	if len(sorted) > 0 && sorted[0].SoldDate != "" {
		lastSold = &SoldSnapshot{Date: sorted[0].SoldDate, PriceUSD: sorted[0].PriceUSD}
	}
	cap := 5
	if len(sorted) < cap {
		cap = len(sorted)
	}
	return &Overall{
		Count:    len(prices),
		Median:   prices[len(prices)/2],
		Lowest:   prices[0],
		Highest:  prices[len(prices)-1],
		LastSold: lastSold,
		Items:    sorted[:cap],
	}
}

// Prices returns market pricing data for a card.
func (uc *UseCase) Prices(ctx context.Context, query map[string]string) *PriceResult {
	code := query["code"]
	name := query["name"]
	nameJp := query["nameJp"]
	nameEn := query["nameEn"]
	if nameEn == "" {
		nameEn = name
	}
	rarity := query["rarity"]
	lang := query["lang"]
	sampleImageURL := query["sampleImageUrl"]
	setQuery := query["set"]
	cardType := query["cardType"]

	if code == "" && nameEn == "" && nameJp == "" && sampleImageURL == "" {
		return &PriceResult{OK: false, Error: "Missing identifying fields"}
	}

	rarityEN := rarityToEnglish(rarity)
	langEN := langToEnglish(lang)
	if langEN == "" {
		langEN = "Japanese"
	}

	primary := buildQuery(code, nameEn, rarityEN, setQuery, cardType, langEN)
	strategies := buildStrategies(code, nameEn, rarityEN, setQuery, cardType, langEN)

	diagnostics := map[string]interface{}{}
	var chosen struct {
		Query  string
		Items  []ebay.Item
		Source string
		Mode   string
	}

	keywordFilter := func(items []ebay.Item) ([]ebay.Item, int) {
		kept := []ebay.Item{}
		rejected := 0
		for _, it := range items {
			if passesTitleMatch(it.Title, code, nameEn, rarityEN, false) {
				kept = append(kept, it)
			} else {
				rejected++
			}
		}
		return kept, rejected
	}
	totalRejected := 0

	// Pass 0: vision reverse-image search (sample image provided).
	if sampleImageURL != "" && uc.visionClient != nil && uc.ebayClient != nil {
		visionItems, vdiag := uc.visionImageListings(ctx, sampleImageURL, code, nameEn, rarityEN, langEN)
		diagnostics["vision"] = vdiag
		if len(visionItems) >= 1 {
			chosen.Query = primary
			chosen.Items = visionItems
			chosen.Source = "active"
			chosen.Mode = "vision-image"
		}
	}

	// Pass 1: Finding API sold history.
	if len(chosen.Items) == 0 && uc.ebayClient != nil {
		for _, q := range strategies {
			items, err := uc.ebayClient.FindingSold(ctx, q)
			if err != nil {
				diagnostics["findingError"] = err.Error()
				continue
			}
			kept, rejected := keywordFilter(items)
			totalRejected += rejected
			if len(kept) > 0 {
				chosen.Query = q
				chosen.Items = kept
				chosen.Source = "sold"
				chosen.Mode = "keyword-sold"
				break
			}
		}
	}

	// Pass 2: HTML scrape sold history.
	if len(chosen.Items) == 0 && uc.ebayClient != nil {
		for _, q := range strategies {
			items, err := uc.ebayClient.ScrapeSold(ctx, q)
			if err != nil {
				diagnostics["scrapeError"] = err.Error()
				continue
			}
			kept, rejected := keywordFilter(items)
			totalRejected += rejected
			if len(kept) > 0 {
				chosen.Query = q
				chosen.Items = kept
				chosen.Source = "sold"
				chosen.Mode = "keyword-scrape"
				break
			}
		}
	}

	// Pass 3: Browse API active listings.
	if len(chosen.Items) == 0 && uc.ebayClient != nil {
		activeQueries := uniqueStrings(append([]string{primary}, strategies...))
		for _, q := range activeQueries {
			items, err := uc.ebayClient.BrowseActive(ctx, q)
			if err != nil {
				diagnostics["browseError"] = err.Error()
				continue
			}
			kept, rejected := keywordFilter(items)
			totalRejected += rejected
			if len(kept) > 0 {
				chosen.Query = q
				chosen.Items = kept
				chosen.Source = "active"
				chosen.Mode = "keyword-active"
				break
			}
		}
	}

	// Parallel graded searches.
	gradedTiers := []string{"PSA 10", "BGS 10", "BGS 10 BL", "CGC 10", "ARS 10"}
	type gradedResult struct {
		tier  string
		items []ebay.Item
	}
	gradedCh := make(chan gradedResult, len(gradedTiers))
	for _, tier := range gradedTiers {
		go func(tier string) {
			q := primary + " " + tier
			var items []ebay.Item
			if uc.ebayClient != nil {
				if sold, err := uc.ebayClient.ScrapeSold(ctx, q); err == nil {
					items = sold[:minInt(len(sold), 10)]
				}
				if len(items) == 0 {
					if active, err := uc.ebayClient.BrowseActive(ctx, q); err == nil {
						items = active[:minInt(len(active), 10)]
					}
				}
			}
			kept := []ebay.Item{}
			for _, it := range items {
				if passesTitleMatch(it.Title, code, nameEn, rarityEN, true) {
					kept = append(kept, it)
				}
			}
			totalRejected += len(items) - len(kept)
			gradedCh <- gradedResult{tier: tier, items: kept}
		}(tier)
	}
	seenURLs := map[string]bool{}
	for _, it := range chosen.Items {
		seenURLs[it.URL] = true
	}
	for i := 0; i < len(gradedTiers); i++ {
		g := <-gradedCh
		for _, it := range g.items {
			if seenURLs[it.URL] {
				continue
			}
			seenURLs[it.URL] = true
			it.ConditionTier = g.tier
			chosen.Items = append(chosen.Items, it)
		}
	}

	chosen.Items = filterByCardCode(chosen.Items, code)
	chosen.Items = filterByRarityTokens(chosen.Items, rarityEN, code)

	// Sort sold first, then by sold date desc.
	sort.Slice(chosen.Items, func(i, j int) bool {
		aSold := chosen.Items[i].SoldDate != ""
		bSold := chosen.Items[j].SoldDate != ""
		if aSold != bSold {
			return aSold
		}
		return chosen.Items[i].SoldDate > chosen.Items[j].SoldDate
	})

	tiers, tierOrder := groupByCondition(chosen.Items)
	overall := calcOverall(chosen.Items)
	mercari := buildMercariUrls(nameJp, nameEn, code, rarity)

	canonical := chosen.Query
	if canonical == "" {
		canonical = primary
	}
	if canonical == "" {
		canonical = nameEn
	}
	if canonical == "" {
		canonical = code
	}

	sourceLabel := "eBay (no results)"
	switch chosen.Source {
	case "sold":
		sourceLabel = "eBay sold (Finding API / HTML scrape · last 90 days)"
	case "active":
		sourceLabel = "eBay active listings (Browse API · asking prices, not sold)"
	}

	diagnostics["totalRejected"] = totalRejected
	diagnostics["mode"] = chosen.Mode

	return &PriceResult{
		OK:             true,
		Query:          chosen.Query,
		TotalSold:      len(chosen.Items),
		Tiers:          tiers,
		TierOrder:      tierOrder,
		Overall:        overall,
		Mercari:        mercari,
		FallbackLinks:  map[string]string{"ebaySold": ebay.SoldURL(canonical), "ebayActive": ebay.ActiveURL(canonical)},
		CanonicalQuery: canonical,
		Source:         sourceLabel,
		Diagnostics:    diagnostics,
	}
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// visionImageListings uses Google Vision + eBay Browse to find listings whose
// product photo matches the SAMPLE image, then filters by title.
func (uc *UseCase) visionImageListings(ctx context.Context, sampleImageURL, code, englishName, rarityEN, langEN string) ([]ebay.Item, map[string]interface{}) {
	diag := map[string]interface{}{}
	res, err := uc.visionClient.WebDetect(ctx, sampleImageURL, "", 50)
	if err != nil || res == nil || !res.OK {
		reason := "Vision call failed"
		if err != nil {
			reason = err.Error()
		} else if res != nil {
			reason = res.Error
		}
		diag["reason"] = reason
		return nil, diag
	}
	ids := extractEbayItemIds(res.Web, 25)
	diag["visionIdsFound"] = len(ids)
	if len(ids) == 0 {
		diag["reason"] = "No eBay item IDs in Vision web detection"
		return nil, diag
	}

	var out []ebay.Item
	confMap := map[string]float64{}
	for _, id := range ids {
		confMap[id.ItemID] = id.Confidence
		item, err := uc.ebayClient.BrowseGetItemByLegacyID(ctx, id.ItemID)
		if err != nil || item == nil || item.PriceUSD <= 0 {
			continue
		}
		if !passesTitleMatch(item.Title, code, englishName, rarityEN, false) {
			continue
		}
		matchBy := "name+rarity"
		if titleNorm(code) != "" && strings.Contains(titleNorm(item.Title), titleNorm(code)) {
			matchBy = "code"
		}
		item.VisionConfidence = confMap[id.ItemID]
		item.Source = "vision-image"
		item.MatchedBy = matchBy
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].VisionConfidence > out[j].VisionConfidence
	})
	diag["hydrated"] = len(out)
	return out, diag
}

type ebayIDMatch struct {
	ItemID     string
	Confidence float64
	Source     string
}

var ebayItemRE = regexp.MustCompile(`(?i)ebay\.[a-z.]+/itm/(?:[^/?#]+/)?(\d{9,16})`)

func extractEbayItemIds(web *vision.WebDetection, max int) []ebayIDMatch {
	byID := map[string]ebayIDMatch{}
	add := func(u string, confidence float64, source string) {
		if u == "" {
			return
		}
		m := ebayItemRE.FindStringSubmatch(u)
		if m == nil {
			return
		}
		id := m[1]
		existing := byID[id]
		if existing.ItemID == "" || confidence > existing.Confidence {
			byID[id] = ebayIDMatch{ItemID: id, Confidence: confidence, Source: source}
		}
	}
	if web != nil {
		for _, p := range web.PagesWithMatchingImages {
			score := p.Score
			hasFull := len(p.FullMatchingImages) > 0
			hasPartial := len(p.PartialMatchingImages) > 0
			conf := 0.7
			if hasFull {
				conf = 1.0
			} else if hasPartial {
				conf = 0.9
			} else {
				conf = math.Max(0.7, math.Min(0.95, score))
			}
			add(p.URL, conf, "pagesWithMatchingImages")
		}
		for _, it := range web.FullMatchingImages {
			add(it.URL, 1.0, "fullMatchingImages")
		}
		for _, it := range web.PartialMatchingImages {
			add(it.URL, 0.85, "partialMatchingImages")
		}
		for _, it := range web.VisuallySimilarImages {
			add(it.URL, 0.6, "visuallySimilarImages")
		}
	}
	out := make([]ebayIDMatch, 0, len(byID))
	for _, v := range byID {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Confidence > out[j].Confidence
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}
