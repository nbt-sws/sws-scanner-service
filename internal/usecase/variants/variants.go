package variants

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	firebaseinfra "github.com/jatibroski/sws-scanner-service/internal/infrastructure/firebase"
	"golang.org/x/sync/errgroup"
)

const cacheTTL = 5 * time.Minute

// UseCase serves card reference data and variant lookups.
type UseCase struct {
	donCatalog     map[string]interface{}
	cnAnnivCatalog map[string]interface{}
	firestore      *firebaseinfra.Firestore
	storage        *firebaseinfra.Storage
	httpClient     *http.Client
	loadOnce       sync.Once
	variantsCache  *ttlCache[*OPVariantsResponse]
	detailsCache   *ttlCache[*OPDetailsResponse]
}

// NewUseCase creates a variants use case.
func NewUseCase(firestore *firebaseinfra.Firestore, storage *firebaseinfra.Storage) *UseCase {
	return &UseCase{
		firestore:     firestore,
		storage:       storage,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
		variantsCache: newTTLCache[*OPVariantsResponse](cacheTTL),
		detailsCache:  newTTLCache[*OPDetailsResponse](cacheTTL),
	}
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
	Code        string `form:"code"`
	Name        string `form:"name"`
	Rarity      string `form:"rarity"`
	Type        string `form:"type"`
	Color       string `form:"color"`
	Set         string `form:"set"`
	Lang        string `form:"lang"`
	Sort        string `form:"sort"`
	Limit       int    `form:"limit"`
	Lightweight string `form:"lightweight"`
}

// Variant is a single printable variant of a card code.
type Variant struct {
	Rarity            string `json:"rarity"`
	Label             string `json:"label"`
	ImageURL          string `json:"imageUrl"`
	Source            string `json:"source"`
	FromDb            bool   `json:"fromDb"`
	Watermarked       bool   `json:"watermarked,omitempty"`
	VerificationCount int    `json:"verificationCount,omitempty"`
	Synthetic         bool   `json:"synthetic,omitempty"`
	VariantSuffix     string `json:"variantSuffix,omitempty"`
	ProductURL        string `json:"productUrl,omitempty"`
}

// Counts reports how many variants each source contributed.
type Counts struct {
	Verified  int `json:"verified"`
	Optcgapi  int `json:"optcgapi"`
	Apitcg    int `json:"apitcg"`
	Bandai    int `json:"bandai"`
	Cardpiece int `json:"cardpiece"`
	Total     int `json:"total"`
}

// Sources reports lightweight source counts.
type Sources struct {
	Verified int `json:"verified"`
	Optcgapi int `json:"optcgapi"`
	Apitcg   int `json:"apitcg"`
}

// OPVariantsResponse is the variants lookup result.
type OPVariantsResponse struct {
	OK             bool      `json:"ok"`
	Code           string    `json:"code,omitempty"`
	Lang           string    `json:"lang,omitempty"`
	Rarities       []string  `json:"rarities"`
	Variants       []Variant `json:"variants,omitempty"`
	Sources        *Sources  `json:"sources,omitempty"`
	Counts         *Counts   `json:"counts,omitempty"`
	Source         string    `json:"source,omitempty"`
	CardpieceTried []string  `json:"cardpieceTried,omitempty"`
	Error          string    `json:"error,omitempty"`
}

func isLightweight(v string) bool {
	return v == "1" || strings.EqualFold(v, "true")
}

func variantsCacheKey(req OPVariantsRequest) string {
	return fmt.Sprintf("%s:%s:%s:%v", strings.ToUpper(req.Code), strings.ToUpper(req.Lang), req.Rarity, isLightweight(req.Lightweight))
}

// OPVariants searches verified cards and external sources for every printed variant of a code.
func (uc *UseCase) OPVariants(ctx context.Context, req OPVariantsRequest) *OPVariantsResponse {
	if req.Code == "" {
		return &OPVariantsResponse{OK: false, Error: "Missing code"}
	}
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 50
	}

	if cached, ok := uc.variantsCache.Get(variantsCacheKey(req)); ok {
		return cached
	}

	lang := strings.ToUpper(req.Lang)
	lightweight := isLightweight(req.Lightweight)

	var verified, optcg, apit, bandai []Variant
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { verified = uc.fromVerified(ctx, req.Code, lang); return nil })
	g.Go(func() error { optcg = uc.fromOptcgapi(ctx, req.Code); return nil })
	g.Go(func() error { apit = uc.fromApitcg(ctx, req.Code); return nil })
	if !lightweight {
		g.Go(func() error { bandai = uc.fromBandai(ctx, req.Code, lang); return nil })
	}
	_ = g.Wait()

	if lightweight {
		set := map[string]bool{}
		for _, v := range append(append(verified, optcg...), apit...) {
			if v.Rarity != "" && v.Rarity != "Unknown" && v.Rarity != "Base" {
				set[v.Rarity] = true
			}
		}
		rarities := make([]string, 0, len(set))
		for r := range set {
			rarities = append(rarities, r)
		}
		sort.Strings(rarities)
		resp := &OPVariantsResponse{
			OK:       true,
			Code:     req.Code,
			Rarities: rarities,
			Sources: &Sources{
				Verified: len(verified),
				Optcgapi: len(optcg),
				Apitcg:   len(apit),
			},
		}
		uc.variantsCache.Set(variantsCacheKey(req), resp)
		return resp
	}

	cardpiece := []Variant{} // TODO: port cardpiece search for CN priority

	var variants []Variant
	if lang == "CN" {
		variants = merge(verified, cardpiece, optcg, apit, bandai)
	} else {
		variants = merge(verified, optcg, apit, bandai, cardpiece)
	}

	source := "external-probe"
	if len(verified) > 0 {
		source = "db-first-merged"
	}

	resp := &OPVariantsResponse{
		OK:       true,
		Code:     req.Code,
		Lang:     req.Lang,
		Variants: variants,
		Counts: &Counts{
			Verified:  len(verified),
			Optcgapi:  len(optcg),
			Apitcg:    len(apit),
			Bandai:    len(bandai),
			Cardpiece: len(cardpiece),
			Total:     len(variants),
		},
		Source:         source,
		CardpieceTried: nil,
	}
	uc.variantsCache.Set(variantsCacheKey(req), resp)
	return resp
}

func (uc *UseCase) fromVerified(ctx context.Context, code, lang string) []Variant {
	if uc.firestore == nil {
		return nil
	}
	cards, err := uc.firestore.FindVerifiedCardsByCode(ctx, code, 100)
	if err != nil {
		return nil
	}

	out := make([]Variant, 0, len(cards))
	for _, v := range cards {
		d := v.Data
		if d == nil {
			d = map[string]interface{}{}
		}
		imageURL := firstString(d["watermarkedSampleUrl"], d["sampleImageUrl"], d["officialImageUrl"])
		if imageURL == "" && v.Samples != nil {
			imageURL = firstString(v.Samples[lang], v.Samples["JP"], v.Samples["EN"], v.Samples["CN"])
		}
		if imageURL == "" {
			continue
		}
		isBase := strings.EqualFold(v.Rarity, "base") || strings.HasSuffix(v.DocKey, "__base")
		rarity := v.Rarity
		if isBase {
			rarity = "Base"
		}
		out = append(out, Variant{
			Rarity:            rarity,
			Label:             firstString(d["officialName"], d["nameEn"], d["nameJp"], code),
			ImageURL:          imageURL,
			Source:            map[bool]string{true: "verified_cards (backfill)", false: "verified_cards"}[isBase],
			FromDb:            true,
			Watermarked:       true,
			VerificationCount: intOrZero(d["verificationCount"]),
		})
	}
	return out
}

func (uc *UseCase) fromOptcgapi(ctx context.Context, code string) []Variant {
	u := fmt.Sprintf("https://optcgapi.com/api/cards/code/%s", url.PathEscape(code))
	body, err := uc.fetchJSON(ctx, u)
	if err != nil {
		return nil
	}
	var arr []map[string]interface{}
	switch v := body.(type) {
	case []interface{}:
		for _, it := range v {
			if m, ok := it.(map[string]interface{}); ok {
				arr = append(arr, m)
			}
		}
	case map[string]interface{}:
		if cards, ok := v["cards"].([]interface{}); ok {
			for _, it := range cards {
				if m, ok := it.(map[string]interface{}); ok {
					arr = append(arr, m)
				}
			}
		} else {
			arr = append(arr, v)
		}
	}
	out := make([]Variant, 0, len(arr))
	for _, c := range arr {
		if c["rarity"] == nil && c["images"] == nil && c["image_url"] == nil {
			continue
		}
		out = append(out, Variant{
			Rarity:   stringOr(c["rarity"], "Unknown"),
			Label:    firstString(c["name"], c["card_name"], code),
			ImageURL: pickImageURL(c),
			Source:   "optcgapi.com",
			FromDb:   false,
		})
	}
	return out
}

func (uc *UseCase) fromApitcg(ctx context.Context, code string) []Variant {
	u := fmt.Sprintf("https://www.apitcg.com/api/one-piece/cards?code=%s", url.QueryEscape(code))
	body, err := uc.fetchJSON(ctx, u)
	if err != nil {
		return nil
	}
	var arr []map[string]interface{}
	if m, ok := body.(map[string]interface{}); ok {
		if data, ok := m["data"].([]interface{}); ok {
			for _, it := range data {
				if mm, ok := it.(map[string]interface{}); ok {
					arr = append(arr, mm)
				}
			}
		} else if list, ok := m["data"].([]map[string]interface{}); ok {
			arr = list
		}
	}
	out := make([]Variant, 0, len(arr))
	for _, c := range arr {
		if c["rarity"] == nil && c["images"] == nil && c["image"] == nil {
			continue
		}
		out = append(out, Variant{
			Rarity:   stringOr(c["rarity"], "Unknown"),
			Label:    firstString(c["name"], code),
			ImageURL: pickImageURL(c),
			Source:   "apitcg.com",
			FromDb:   false,
		})
	}
	return out
}

var bandaiHostsByLang = map[string][]string{
	"JP": {"www.onepiece-cardgame.com", "asia-en.onepiece-cardgame.com", "en.onepiece-cardgame.com"},
	"EN": {"en.onepiece-cardgame.com", "asia-en.onepiece-cardgame.com", "www.onepiece-cardgame.com"},
	"AE": {"asia-en.onepiece-cardgame.com", "en.onepiece-cardgame.com", "www.onepiece-cardgame.com"},
	"CN": {"www.onepiece-cardgame.cn", "asia-en.onepiece-cardgame.com", "en.onepiece-cardgame.com"},
}

var bandaiSuffixes = []string{"", "_p1", "_p2", "_p3", "_p4", "_p5", "_alt", "_aa", "_f", "_r"}

func (uc *UseCase) fromBandai(ctx context.Context, code, lang string) []Variant {
	hosts, ok := bandaiHostsByLang[strings.ToUpper(lang)]
	if !ok || len(hosts) == 0 {
		hosts = bandaiHostsByLang["JP"]
	}

	workingHost := ""
	for _, host := range hosts {
		base := fmt.Sprintf("https://%s/images/cardlist/card/%s.png", host, code)
		if uc.isImageURL(ctx, base) {
			workingHost = host
			break
		}
	}
	if workingHost == "" {
		return nil
	}

	patterns := []string{"images/cardlist/card/{code}.png", "images/cardlist/card/{code}.jpg", "images/cardlist/{code}.png"}
	if strings.HasSuffix(workingHost, ".cn") {
		patterns = []string{
			"images/cardlist/{code}.png",
			"images/cardlist/card/{code}.png",
			"wp-content/uploads/cardlist/{code}.png",
			"wp-content/uploads/cardlist/images/{code}.png",
			"assets/cardlist/{code}.png",
		}
	}

	type job struct{ suffix, pattern string }
	jobs := make([]job, 0, len(bandaiSuffixes)*len(patterns))
	for _, s := range bandaiSuffixes {
		for _, p := range patterns {
			jobs = append(jobs, job{suffix: s, pattern: p})
		}
	}

	var mu sync.Mutex
	var found []Variant
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 8)
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			path := strings.ReplaceAll(j.pattern, "{code}", code+j.suffix)
			u := fmt.Sprintf("https://%s/%s", workingHost, path)
			semaphore <- struct{}{}
			ok := uc.isImageURL(ctx, u)
			<-semaphore
			if !ok {
				return
			}
			var rarity string
			switch j.suffix {
			case "":
				rarity = ""
			case "_alt":
				rarity = "Alt Art"
			case "_aa":
				rarity = "All Art"
			case "_f":
				rarity = "Foiled"
			case "_r":
				rarity = "Reprint"
			default:
				rarity = "Parallel"
			}
			source := fmt.Sprintf("onepiece-cardgame.com (%s)", strings.Split(workingHost, ".")[0])
			if strings.HasSuffix(workingHost, ".cn") {
				source = "onepiece-cardgame.cn"
			}
			mu.Lock()
			found = append(found, Variant{
				Rarity:        rarity,
				Label:         code + j.suffix,
				ImageURL:      u,
				Source:        source,
				FromDb:        false,
				VariantSuffix: j.suffix,
			})
			mu.Unlock()
		}(j)
	}
	wg.Wait()

	seen := map[string]bool{}
	out := make([]Variant, 0, len(found))
	for _, v := range found {
		if seen[v.ImageURL] {
			continue
		}
		seen[v.ImageURL] = true
		out = append(out, v)
	}
	return out
}

func (uc *UseCase) isImageURL(ctx context.Context, u string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SwibSwap/13.9)")
	req.Header.Set("Accept", "image/png,image/*;q=0.9,*/*;q=0.8")
	req.Header.Set("Range", "bytes=0-512")
	resp, err := uc.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	return (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent) && strings.HasPrefix(ct, "image/")
}

func (uc *UseCase) fetchJSON(ctx context.Context, u string) (interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SwibSwap/13.9)")
	resp, err := uc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body, nil
}

func merge(lists ...[]Variant) []Variant {
	byURL := map[string]*Variant{}
	for _, list := range lists {
		for i := range list {
			v := &list[i]
			if v.ImageURL == "" {
				continue
			}
			existing, ok := byURL[v.ImageURL]
			if !ok {
				cp := *v
				byURL[v.ImageURL] = &cp
				continue
			}
			if existing.Rarity == "" && v.Rarity != "" {
				existing.Rarity = v.Rarity
			}
			if existing.Label == "" && v.Label != "" {
				existing.Label = v.Label
			}
			if !existing.FromDb && v.FromDb {
				existing.FromDb = true
			}
		}
	}
	all := make([]Variant, 0, len(byURL))
	for _, v := range byURL {
		all = append(all, *v)
	}

	hasReal := false
	for _, v := range all {
		if v.Rarity != "" && v.Rarity != "Base" && v.Rarity != "Unknown" {
			hasReal = true
			break
		}
	}
	if hasReal {
		filtered := make([]Variant, 0, len(all))
		for _, v := range all {
			if v.Rarity != "" && v.Rarity != "Base" && v.Rarity != "Unknown" {
				filtered = append(filtered, v)
			}
		}
		all = filtered
	}

	starRarities := []string{"L", "SR", "SEC", "R", "UC", "C"}
	starRe := regexp.MustCompile(fmt.Sprintf("^(?:%s)(★?)$", strings.Join(starRarities, "|")))
	parallelRe := regexp.MustCompile(`(?i)^(parallel|alt[\s-]?art|alternate[\s-]?art)$`)
	byRarity := map[string]bool{}
	for _, v := range all {
		if v.Rarity != "" {
			byRarity[v.Rarity] = true
		}
	}
	pairs := []Variant{}
	for _, v := range all {
		if v.Rarity == "" {
			continue
		}
		r := strings.TrimSpace(v.Rarity)
		if m := starRe.FindStringSubmatch(r); m != nil {
			base := m[1]
			hadStar := m[2] == "★"
			companion := base
			if hadStar {
				companion = base
			} else {
				companion = base + "★"
			}
			if !byRarity[companion] {
				label := v.Label
				if !hadStar {
					label = fmt.Sprintf("%s (Alt Art ★)", firstString(v.Label, base))
				}
				pairs = append(pairs, Variant{
					Rarity:    companion,
					Label:     label,
					ImageURL:  v.ImageURL,
					Source:    v.Source + " (synthetic)",
					Synthetic: true,
				})
				byRarity[companion] = true
			}
		} else if parallelRe.MatchString(r) {
			baseCandidates := []string{"SR", "SEC", "L", "R", "UC", "C"}
			inferred := "SR"
			for _, b := range baseCandidates {
				if byRarity[b] {
					inferred = b
					break
				}
			}
			if !byRarity[inferred] {
				pairs = append(pairs, Variant{
					Rarity:    inferred,
					Label:     fmt.Sprintf("%s (Base printing)", firstString(v.Label, inferred)),
					ImageURL:  v.ImageURL,
					Source:    v.Source + " (synthetic from Parallel)",
					Synthetic: true,
				})
				byRarity[inferred] = true
			}
		}
	}
	all = append(all, pairs...)
	return all
}

func pickImageURL(c map[string]interface{}) string {
	if images, ok := c["images"].(map[string]interface{}); ok {
		if u, ok := images["large"].(string); ok && u != "" {
			return u
		}
		if u, ok := images["small"].(string); ok && u != "" {
			return u
		}
	}
	for _, k := range []string{"image_url", "image"} {
		if u, ok := c[k].(string); ok && u != "" {
			return u
		}
	}
	return ""
}

func firstString(vals ...interface{}) string {
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func stringOr(v interface{}, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func intOrZero(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// OPDetailsRequest holds detail parameters.
type OPDetailsRequest struct {
	Code   string `form:"code" binding:"required"`
	Rarity string `form:"rarity"`
	Lang   string `form:"lang"`
}

// OPDetailsResponse is the detail lookup result.
type OPDetailsResponse struct {
	OK          bool                   `json:"ok"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Diagnostics map[string]interface{} `json:"diagnostics,omitempty"`
	Error       string                 `json:"error,omitempty"`
}

func detailsCacheKey(req OPDetailsRequest) string {
	return fmt.Sprintf("%s:%s:%s", strings.ToUpper(req.Code), strings.ToUpper(req.Lang), req.Rarity)
}

// OPDetails returns canonical card metadata + a sample image URL.
func (uc *UseCase) OPDetails(ctx context.Context, req OPDetailsRequest) *OPDetailsResponse {
	if req.Code == "" {
		return &OPDetailsResponse{OK: false, Error: "Missing code"}
	}

	if cached, ok := uc.detailsCache.Get(detailsCacheKey(req)); ok {
		return cached
	}

	var optcgMeta, apitMeta map[string]interface{}
	var optcgErr, apitErr error
	var bandai map[string]interface{}

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		optcgMeta, optcgErr = uc.detailsFromOptcgapi(ctx, req.Code)
		return nil
	})
	g.Go(func() error {
		apitMeta, apitErr = uc.detailsFromApitcg(ctx, req.Code)
		return nil
	})
	g.Go(func() error {
		bandai = uc.bandaiDirectImage(ctx, req.Code, req.Lang)
		return nil
	})
	_ = g.Wait()

	errors := []string{}
	metadata := optcgMeta
	if optcgErr != nil {
		errors = append(errors, fmt.Sprintf("optcgapi: %v", optcgErr))
	}
	if metadata == nil {
		metadata = apitMeta
		if apitErr != nil {
			errors = append(errors, fmt.Sprintf("apitcg: %v", apitErr))
		}
	}

	if bandai != nil && bandai["imageUrl"] != nil && bandai["imageUrl"] != "" {
		if metadata == nil {
			metadata = map[string]interface{}{}
		}
		metadata["imageUrl"] = bandai["imageUrl"]
		metadata["source"] = bandai["source"]
	}

	if metadata == nil {
		resp := &OPDetailsResponse{
			OK:          true,
			Details:     nil,
			Diagnostics: map[string]interface{}{"sourceErrors": errors, "mirroredToFirebase": false},
		}
		uc.detailsCache.Set(detailsCacheKey(req), resp)
		return resp
	}

	imageURL, _ := metadata["imageUrl"].(string)
	mirrored := ""
	if imageURL != "" && req.Rarity != "" && uc.storage != nil {
		mirrored, _ = uc.mirrorIfNew(ctx, imageURL, req.Code, req.Rarity)
	}

	details := normalizeDetails(metadata)
	if mirrored != "" {
		details["sampleImageUrl"] = mirrored
	} else {
		details["sampleImageUrl"] = imageURL
	}

	resp := &OPDetailsResponse{
		OK:      true,
		Details: details,
		Diagnostics: map[string]interface{}{
			"sourceErrors":       errors,
			"mirroredToFirebase": mirrored != "",
		},
	}
	uc.detailsCache.Set(detailsCacheKey(req), resp)
	return resp
}

func (uc *UseCase) detailsFromOptcgapi(ctx context.Context, code string) (map[string]interface{}, error) {
	u := fmt.Sprintf("https://optcgapi.com/api/cards/code/%s", url.PathEscape(code))
	body, err := uc.fetchJSON(ctx, u)
	if err != nil {
		return nil, err
	}
	var card map[string]interface{}
	switch v := body.(type) {
	case []interface{}:
		if len(v) > 0 {
			card, _ = v[0].(map[string]interface{})
		}
	case map[string]interface{}:
		if c, ok := v["card"].(map[string]interface{}); ok {
			card = c
		} else {
			card = v
		}
	}
	if card == nil || (card["name"] == nil && card["card_name"] == nil) {
		return nil, fmt.Errorf("no card body")
	}
	return normalizeDetails(map[string]interface{}{
		"code":        firstString(card["code"], card["id"], code),
		"name":        firstString(card["name"], card["card_name"]),
		"type":        firstString(card["type"], card["card_type"]),
		"color":       firstString(card["color"], card["colors"]),
		"cost":        numOrNull(card["cost"]),
		"power":       numOrNull(card["power"]),
		"life":        numOrNull(card["life"]),
		"counter":     numOrNull(card["counter"]),
		"attribute":   firstString(card["attribute"], card["attributes"]),
		"effect":      firstString(card["effect"], card["text"]),
		"setCode":     firstString(card["setCode"], nestedString(card["set"], "code")),
		"setName":     firstString(card["setName"], nestedString(card["set"], "name")),
		"releaseDate": firstString(card["releaseDate"], nestedString(card["set"], "releaseDate")),
		"rarity":      card["rarity"],
		"imageUrl":    pickImageURL(card),
		"source":      "optcgapi.com",
	}), nil
}

func (uc *UseCase) detailsFromApitcg(ctx context.Context, code string) (map[string]interface{}, error) {
	u := fmt.Sprintf("https://www.apitcg.com/api/one-piece/cards?code=%s", url.QueryEscape(code))
	body, err := uc.fetchJSON(ctx, u)
	if err != nil {
		return nil, err
	}
	m, ok := body.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response type")
	}
	var card map[string]interface{}
	if data, ok := m["data"].([]interface{}); ok && len(data) > 0 {
		card, _ = data[0].(map[string]interface{})
	} else if data, ok := m["data"].([]map[string]interface{}); ok && len(data) > 0 {
		card = data[0]
	}
	if card == nil {
		return nil, fmt.Errorf("no card")
	}
	return normalizeDetails(map[string]interface{}{
		"code":        firstString(card["code"], code),
		"name":        card["name"],
		"type":        card["type"],
		"color":       card["color"],
		"cost":        numOrNull(card["cost"]),
		"power":       numOrNull(card["power"]),
		"life":        numOrNull(card["life"]),
		"counter":     numOrNull(card["counter"]),
		"attribute":   card["attribute"],
		"effect":      firstString(card["effect"], card["ability"]),
		"setCode":     nestedString(card["set"], "id"),
		"setName":     nestedString(card["set"], "name"),
		"releaseDate": nestedString(card["set"], "releaseDate"),
		"rarity":      card["rarity"],
		"imageUrl":    pickImageURL(card),
		"source":      "apitcg.com",
	}), nil
}

func (uc *UseCase) bandaiDirectImage(ctx context.Context, code, lang string) map[string]interface{} {
	hosts, ok := bandaiHostsByLang[strings.ToUpper(lang)]
	if !ok || len(hosts) == 0 {
		hosts = bandaiHostsByLang["JP"]
	}
	for _, host := range hosts {
		patterns := []string{
			fmt.Sprintf("https://%s/images/cardlist/card/%s.png", host, code),
			fmt.Sprintf("https://%s/images/cardlist/card/%s.jpg", host, code),
			fmt.Sprintf("https://%s/images/cardlist/card/%s_p1.png", host, code),
			fmt.Sprintf("https://%s/images/cardlist/card/%s_p2.png", host, code),
		}
		if strings.HasSuffix(host, ".cn") {
			patterns = []string{
				fmt.Sprintf("https://%s/images/cardlist/%s.png", host, code),
				fmt.Sprintf("https://%s/images/cardlist/card/%s.png", host, code),
				fmt.Sprintf("https://%s/wp-content/uploads/cardlist/%s.png", host, code),
				fmt.Sprintf("https://%s/wp-content/uploads/cardlist/images/%s.png", host, code),
			}
		}
		for _, u := range patterns {
			if uc.isImageURL(ctx, u) {
				source := fmt.Sprintf("onepiece-cardgame.com (%s)", strings.Split(host, ".")[0])
				if strings.HasSuffix(host, ".cn") {
					source = "onepiece-cardgame.cn"
				}
				return map[string]interface{}{"imageUrl": u, "source": source}
			}
		}
	}
	return nil
}

func (uc *UseCase) mirrorIfNew(ctx context.Context, remoteURL, code, rarity string) (string, error) {
	if uc.storage == nil {
		return "", fmt.Errorf("storage not initialized")
	}
	r := strings.ReplaceAll(strings.ReplaceAll(rarity, " ", ""), "/", "")
	key := fmt.Sprintf("verified_cards/%s__%s", code, r)

	exists, err := uc.storage.Exists(ctx, key+".jpg")
	if err == nil && exists {
		return fmt.Sprintf("https://storage.googleapis.com/%s/%s.jpg", uc.storage.Bucket(), key), nil
	}
	for _, ext := range []string{"png", "webp"} {
		if exists, _ := uc.storage.Exists(ctx, key+"."+ext); exists {
			return fmt.Sprintf("https://storage.googleapis.com/%s/%s.%s", uc.storage.Bucket(), key, ext), nil
		}
	}

	imgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
	if err != nil {
		return "", err
	}
	imgReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SwibSwap/13.9)")
	imgResp, err := uc.httpClient.Do(imgReq)
	if err != nil {
		return "", err
	}
	defer imgResp.Body.Close()
	if imgResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upstream %d", imgResp.StatusCode)
	}
	data, err := io.ReadAll(imgResp.Body)
	if err != nil {
		return "", err
	}
	contentType := imgResp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	ext := "jpg"
	if strings.Contains(contentType, "png") {
		ext = "png"
	} else if strings.Contains(contentType, "webp") {
		ext = "webp"
	}
	dest := fmt.Sprintf("%s.%s", key, ext)
	metadata := map[string]string{"cacheControl": "public, max-age=31536000, immutable"}
	if _, err := uc.storage.UploadBytes(ctx, dest, contentType, data, metadata); err != nil {
		return "", err
	}
	return fmt.Sprintf("https://storage.googleapis.com/%s/%s", uc.storage.Bucket(), dest), nil
}

func normalizeDetails(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range in {
		if v == nil || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func nestedString(m interface{}, key string) string {
	mm, ok := m.(map[string]interface{})
	if !ok {
		return ""
	}
	if s, ok := mm[key].(string); ok {
		return s
	}
	return ""
}

func numOrNull(v interface{}) interface{} {
	if v == nil || v == "" {
		return nil
	}
	switch n := v.(type) {
	case int, int64, float64:
		return n
	case string:
		// attempt to keep as number if parseable
		return v
	}
	return v
}

// DonCardsRequest holds filter parameters for the DON catalog.
type DonCardsRequest struct {
	Name      string `form:"name"`
	Variant   string `form:"variant"`
	SetCode   string `form:"setCode"`
	Character string `form:"character"`
	Verified  string `form:"verified"`
}

// DonCards returns the filtered DON card catalog.
func (uc *UseCase) DonCards(ctx context.Context, req DonCardsRequest) map[string]interface{} {
	uc.ensureLoaded()
	catalog := uc.donCatalog
	items := buildDonItems(catalog)

	if req.Name != "" {
		q := strings.ToLower(req.Name)
		items = filterDonItems(items, func(it map[string]interface{}) bool {
			return strings.Contains(strings.ToLower(stringOr(it["name"], "")), q) ||
				strings.Contains(strings.ToLower(stringOr(it["character"], "")), q) ||
				strings.Contains(strings.ToLower(stringOr(it["setName"], "")), q)
		})
	}
	if req.Character != "" {
		q := strings.ToLower(req.Character)
		items = filterDonItems(items, func(it map[string]interface{}) bool {
			return strings.Contains(strings.ToLower(stringOr(it["character"], "")), q)
		})
	}
	if req.Variant != "" {
		v := strings.ToLower(req.Variant)
		items = filterDonItems(items, func(it map[string]interface{}) bool {
			return strings.Contains(strings.ToLower(stringOr(it["variant"], "")), v)
		})
	}
	if req.SetCode != "" {
		s := strings.ToUpper(req.SetCode)
		items = filterDonItems(items, func(it map[string]interface{}) bool {
			return strings.ToUpper(stringOr(it["setHint"], "")) == s
		})
	}

	verifiedDiag, items := applyVerifiedFilter(ctx, uc.firestore, items, "synthCode", strings.EqualFold(req.Verified, "true"))

	return map[string]interface{}{
		"ok":             true,
		"source":         stringOr(catalog["source"], "PDF catalog"),
		"count":          len(items),
		"items":          items,
		"verifiedFilter": verifiedDiag,
	}
}

func buildDonItems(catalog map[string]interface{}) []map[string]interface{} {
	raw, _ := catalog["items"].([]interface{})
	out := make([]map[string]interface{}, 0, len(raw))
	for _, e := range raw {
		entry, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		displayName := firstString(entry["character"], entry["setName"], fmt.Sprintf("DON %s", stringOr(entry["setCode"], stringOr(entry["id"], ""))))
		synthCode := ""
		if entry["character"] != nil && stringOr(entry["character"], "") != "" {
			synthCode = fmt.Sprintf("%s Don Card", entry["character"])
		} else {
			synthCode = fmt.Sprintf("%s Don Card", firstString(entry["setName"], entry["setCode"], "DON"))
		}
		variant := stringOr(entry["variant"], "")
		if strings.ToLower(variant) == "gold" {
			variant = "Gold"
		} else {
			variant = "Regular"
		}
		out = append(out, map[string]interface{}{
			"id":         entry["id"],
			"name":       displayName,
			"character":  entry["character"],
			"variant":    variant,
			"rarity":     entry["rarity"],
			"synthCode":  synthCode,
			"setHint":    entry["setCode"],
			"setName":    entry["setName"],
			"setLabelJp": entry["setLabelJp"],
			"imageUrl":   entry["imageUrl"],
			"page":       entry["page"],
			"cell":       entry["cell"],
		})
	}
	return out
}

// CNAnnivCardsRequest holds filter parameters for the CN anniversary catalog.
type CNAnnivCardsRequest struct {
	Anniv    string `form:"anniv"`
	SetCode  string `form:"setCode"`
	Verified string `form:"verified"`
}

// CNAnnivCards returns the filtered CN anniversary card catalog.
func (uc *UseCase) CNAnnivCards(ctx context.Context, req CNAnnivCardsRequest) map[string]interface{} {
	uc.ensureLoaded()
	catalog := uc.cnAnnivCatalog
	items := buildCNAnnivItems(catalog)

	if req.Anniv != "" {
		a := strings.ToUpper(req.Anniv)
		items = filterDonItems(items, func(it map[string]interface{}) bool {
			return strings.ToUpper(stringOr(it["anniv"], "")) == a
		})
	}
	if req.SetCode != "" {
		s := strings.ToUpper(req.SetCode)
		items = filterDonItems(items, func(it map[string]interface{}) bool {
			return strings.ToUpper(stringOr(it["setCode"], "")) == s
		})
	}

	verifiedDiag, items := applyVerifiedFilter(ctx, uc.firestore, items, "synthCode", strings.EqualFold(req.Verified, "true"))

	return map[string]interface{}{
		"ok":             true,
		"source":         stringOr(catalog["source"], "CN Anniversary List PDF"),
		"count":          len(items),
		"items":          items,
		"verifiedFilter": verifiedDiag,
	}
}

var annivLabel = map[string]string{
	"1ANV": "1st Anniversary",
	"2ANV": "2nd Anniversary",
	"3ANV": "3rd Anniversary",
}

func buildCNAnnivItems(catalog map[string]interface{}) []map[string]interface{} {
	raw, _ := catalog["items"].([]interface{})
	out := make([]map[string]interface{}, 0, len(raw))
	for _, e := range raw {
		entry, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		anniv := stringOr(entry["anniv"], "")
		label := annivLabel[anniv]
		if label == "" {
			label = anniv
		}
		idx := 0
		switch n := entry["idx"].(type) {
		case int:
			idx = n
		case int64:
			idx = int(n)
		case float64:
			idx = int(n)
		}
		imageUrl := stringOr(entry["imageUrl"], "")
		fileName := imageUrl
		if parts := strings.Split(imageUrl, "/"); len(parts) > 0 {
			fileName = parts[len(parts)-1]
		}
		out = append(out, map[string]interface{}{
			"id":         entry["id"],
			"name":       fmt.Sprintf("CN %s #%03d", label, idx),
			"character":  nil,
			"variant":    anniv,
			"rarity":     "Anniversary Promo",
			"synthCode":  entry["synthCode"],
			"setHint":    entry["setCode"],
			"setCode":    entry["setCode"],
			"setName":    label,
			"imageUrl":   imageUrl,
			"anniv":      anniv,
			"page":       entry["page"],
			"sourceFile": fileName,
		})
	}
	return out
}

func filterDonItems(items []map[string]interface{}, keep func(map[string]interface{}) bool) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		if keep(it) {
			out = append(out, it)
		}
	}
	return out
}

func applyVerifiedFilter(ctx context.Context, firestore *firebaseinfra.Firestore, items []map[string]interface{}, codeField string, require bool) (interface{}, []map[string]interface{}) {
	if !require || len(items) == 0 {
		return nil, items
	}
	codes := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		c := stringOr(it[codeField], "")
		if c != "" && !seen[c] {
			codes = append(codes, c)
			seen[c] = true
		}
	}
	if firestore == nil {
		return map[string]interface{}{"verifiedLookup": "failed", "kept": len(items)}, items
	}
	verifiedCodes, err := firestore.FindExistingVerifiedCodes(ctx, codes)
	if err != nil {
		return map[string]interface{}{"verifiedLookup": "failed", "kept": len(items)}, items
	}
	before := len(items)
	filtered := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		if verifiedCodes[stringOr(it[codeField], "")] {
			filtered = append(filtered, it)
		}
	}
	return map[string]interface{}{
		"verifiedLookup":    "ok",
		"scanned":           before,
		"kept":              len(filtered),
		"unverifiedDropped": before - len(filtered),
	}, filtered
}
