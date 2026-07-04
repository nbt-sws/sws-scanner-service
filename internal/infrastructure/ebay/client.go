package ebay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	findingEndpoint   = "https://svcs.ebay.com/services/search/FindingService/v1"
	browseTokenURL    = "https://api.ebay.com/identity/v1/oauth2/token"
	browseAPI         = "https://api.ebay.com/buy/browse/v1"
	tcgCategory       = "183454"
	soldPageURL       = "https://www.ebay.com/sch/i.html?_nkw=%s&_sacat=0&LH_Sold=1&LH_Complete=1&_ipg=60"
	activePageURL     = "https://www.ebay.com/sch/i.html?_nkw=%s&_sacat=0&_ipg=60"
)

// Item is a normalized eBay listing.
type Item struct {
	Title            string  `json:"title"`
	URL              string  `json:"url"`
	PriceUSD         float64 `json:"priceUSD"`
	Currency         string  `json:"currency"`
	SoldDate         string  `json:"soldDate,omitempty"`
	Thumbnail        string  `json:"thumbnail,omitempty"`
	Condition        string  `json:"condition,omitempty"`
	Country          string  `json:"country,omitempty"`
	LegacyID         string  `json:"legacyId,omitempty"`
	Source           string  `json:"source"`
	ConditionTier    string  `json:"conditionTier,omitempty"`
	VisionConfidence float64 `json:"visionConfidence,omitempty"`
	MatchedBy        string  `json:"matchedBy,omitempty"`
	RawPrice         string  `json:"rawPrice,omitempty"`
}

// Client provides eBay Finding, Browse and HTML-scrape access.
type Client struct {
	appID      string
	certID     string
	httpClient *http.Client

	tokenMu       sync.RWMutex
	browseToken   string
	browseTokenExpiry time.Time
}

// NewClient creates an eBay client. Credentials may be empty for scrape-only mode.
func NewClient(appID, certID string) *Client {
	return &Client{
		appID:      appID,
		certID:     certID,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) doJSON(ctx context.Context, method, urlStr string, headers map[string]string, body io.Reader, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ebay HTTP %d: %s", resp.StatusCode, string(b)[:min(len(b), 200)])
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─── Browse API OAuth token ─────────────────────────────────────────────────

func (c *Client) getBrowseToken(ctx context.Context) (string, error) {
	c.tokenMu.RLock()
	tok := c.browseToken
	exp := c.browseTokenExpiry
	c.tokenMu.RUnlock()
	if tok != "" && time.Now().Add(60*time.Second).Before(exp) {
		return tok, nil
	}
	if c.appID == "" || c.certID == "" {
		return "", fmt.Errorf("eBay Browse credentials not configured")
	}
	creds := base64.StdEncoding.EncodeToString([]byte(c.appID + ":" + c.certID))
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("scope", "https://api.ebay.com/oauth/api_scope")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, browseTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("eBay token HTTP %d: %s", resp.StatusCode, string(b)[:min(len(b), 200)])
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return "", err
	}
	c.tokenMu.Lock()
	c.browseToken = t.AccessToken
	c.browseTokenExpiry = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	c.tokenMu.Unlock()
	return t.AccessToken, nil
}

// ─── Browse API: get item by legacy ID ──────────────────────────────────────

func (c *Client) BrowseGetItemByLegacyID(ctx context.Context, legacyID string) (*Item, error) {
	token, err := c.getBrowseToken(ctx)
	if err != nil {
		return nil, err
	}
	urlStr := browseAPI + "/item/get_item_by_legacy_id?legacy_item_id=" + url.QueryEscape(legacyID)
	var it browseItemDetail
	if err := c.doJSON(ctx, http.MethodGet, urlStr, map[string]string{
		"Authorization":           "Bearer " + token,
		"X-EBAY-C-MARKETPLACE-ID": "EBAY_US",
	}, nil, &it); err != nil {
		return nil, err
	}
	if it.Title == "" {
		return nil, fmt.Errorf("empty item")
	}
	price, _ := strconv.ParseFloat(it.Price.Value, 64)
	thumbnail := ""
	if it.Image != nil && it.Image.ImageURL != "" {
		thumbnail = it.Image.ImageURL
	} else if len(it.AdditionalImages) > 0 && it.AdditionalImages[0].ImageURL != "" {
		thumbnail = it.AdditionalImages[0].ImageURL
	}
	return &Item{
		Title:     it.Title,
		URL:       it.ItemWebURL,
		PriceUSD:  price,
		Currency:  it.Price.Currency,
		LegacyID:  legacyID,
		Thumbnail: thumbnail,
		Condition: it.Condition,
		Country:   it.ItemLocation.Country,
		Source:    "ebay-browse",
	}, nil
}

type browseItemDetail struct {
	Title           string `json:"title"`
	ItemWebURL      string `json:"itemWebUrl"`
	Price           struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"price"`
	Image            *browseImage `json:"image,omitempty"`
	AdditionalImages []browseImage `json:"additionalImages,omitempty"`
	Condition        string `json:"condition"`
	ItemLocation     struct{ Country string `json:"country"` } `json:"itemLocation"`
}

type browseImage struct {
	ImageURL string `json:"imageUrl"`
}

// ─── Browse API: active listing search ──────────────────────────────────────

func (c *Client) BrowseActive(ctx context.Context, query string) ([]Item, error) {
	token, err := c.getBrowseToken(ctx)
	if err != nil {
		return nil, err
	}
	urlStr := fmt.Sprintf("%s/item_summary/search?q=%s&category_ids=%s&limit=25",
		browseAPI, url.QueryEscape(query), tcgCategory)
	var data struct {
		ItemSummaries []browseItemSummary `json:"itemSummaries"`
	}
	if err := c.doJSON(ctx, http.MethodGet, urlStr, map[string]string{
		"Authorization":           "Bearer " + token,
		"X-EBAY-C-MARKETPLACE-ID": "EBAY_US",
	}, nil, &data); err != nil {
		return nil, err
	}
	var out []Item
	for _, it := range data.ItemSummaries {
		price, _ := strconv.ParseFloat(it.Price.Value, 64)
		if price <= 0 {
			continue
		}
		out = append(out, Item{
			Title:     it.Title,
			URL:       it.ItemWebURL,
			PriceUSD:  price,
			Currency:  it.Price.Currency,
			Thumbnail: it.Image.ImageURL,
			Condition: it.Condition,
			Country:   it.ItemLocation.Country,
			Source:    "ebay-browse-active",
		})
	}
	return out, nil
}

type browseItemSummary struct {
	Title        string `json:"title"`
	ItemWebURL   string `json:"itemWebUrl"`
	Price        struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"price"`
	Image        struct{ ImageURL string `json:"imageUrl"` } `json:"image"`
	Condition    string `json:"condition"`
	ItemLocation struct{ Country string `json:"country"` } `json:"itemLocation"`
}

// ─── Finding API: sold items ────────────────────────────────────────────────

func (c *Client) FindingSold(ctx context.Context, query string) ([]Item, error) {
	if c.appID == "" {
		return nil, fmt.Errorf("EBAY_APP_ID not configured")
	}
	u, _ := url.Parse(findingEndpoint)
	q := u.Query()
	q.Set("OPERATION-NAME", "findCompletedItems")
	q.Set("SERVICE-VERSION", "1.0.0")
	q.Set("SECURITY-APPNAME", c.appID)
	q.Set("RESPONSE-DATA-FORMAT", "JSON")
	q.Set("REST-PAYLOAD", "")
	q.Set("keywords", query)
	q.Set("categoryId", tcgCategory)
	q.Set("paginationInput.entriesPerPage", "50")
	q.Set("sortOrder", "EndTimeSoonest")
	q.Set("itemFilter(0).name", "SoldItemsOnly")
	q.Set("itemFilter(0).value", "true")
	u.RawQuery = q.Encode()

	var resp findingResponse
	if err := c.doJSON(ctx, http.MethodGet, u.String(), nil, nil, &resp); err != nil {
		return nil, err
	}
	var out []Item
	for _, it := range resp.FindCompletedItemsResponse.SearchResult.Items {
		price := parseFindingPrice(it.SellingStatus.ConvertedCurrentPrice)
		if price <= 0 {
			price = parseFindingPrice(it.SellingStatus.CurrentPrice)
		}
		if price <= 0 {
			continue
		}
		out = append(out, Item{
			Title:     it.Title,
			URL:       it.ViewItemURL,
			PriceUSD:  price,
			Currency:  it.SellingStatus.ConvertedCurrentPrice.CurrencyID,
			SoldDate:  it.ListingInfo.EndTime,
			Thumbnail: it.GalleryURL,
			Condition: it.Condition.DisplayName,
			Country:   it.Country,
			Source:    "ebay-finding-sold",
		})
	}
	return out, nil
}

func parseFindingPrice(p findingPrice) float64 {
	if p.Value == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(p.Value, 64)
	return v
}

type findingResponse struct {
	FindCompletedItemsResponse struct {
		SearchResult struct {
			Items []findingItem `json:"item"`
		} `json:"searchResult"`
	} `json:"findCompletedItemsResponse"`
}

type findingItem struct {
	Title          string         `json:"title"`
	ViewItemURL    string         `json:"viewItemURL"`
	SellingStatus  struct {
		ConvertedCurrentPrice findingPrice `json:"convertedCurrentPrice"`
		CurrentPrice          findingPrice `json:"currentPrice"`
	} `json:"sellingStatus"`
	ListingInfo    struct {
		EndTime string `json:"endTime"`
	} `json:"listingInfo"`
	GalleryURL     string         `json:"galleryURL"`
	Condition      struct {
		DisplayName string `json:"conditionDisplayName"`
	} `json:"condition"`
	Country        string         `json:"country"`
}

type findingPrice struct {
	Value      string `json:"__value__"`
	CurrencyID string `json:"@currencyId"`
}

// ─── HTML scrape of eBay sold listings ──────────────────────────────────────

var (
	sItemRE = regexp.MustCompile(`<li[^>]+class="[^"]*\bs-item\b[^"]*"`)
	sCardRE = regexp.MustCompile(`<li[^>]+class="[^"]*\bs-card\b[^"]*"`)
)

func (c *Client) ScrapeSold(ctx context.Context, query string) ([]Item, error) {
	urlStr := fmt.Sprintf(soldPageURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Referer", "https://www.ebay.com/")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("eBay scrape HTTP %d", resp.StatusCode)
	}
	html, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	items := parseScrapedHTML(string(html))
	for i := range items {
		items[i].Source = "ebay-sold-html"
	}
	return items, nil
}

func parseScrapedHTML(html string) []Item {
	var items []Item
	seen := map[string]bool{}
	for _, splitter := range []*regexp.Regexp{sItemRE, sCardRE} {
		blocks := splitter.Split(html, -1)[1:]
		for _, blk := range blocks {
			it := parseScrapeBlock(blk)
			if it == nil || it.URL == "" {
				continue
			}
			key := strings.Split(it.URL, "?")[0]
			if seen[key] {
				continue
			}
			seen[key] = true
			items = append(items, *it)
			if len(items) >= 60 {
				break
			}
		}
		if len(items) > 0 {
			break
		}
	}
	return items
}

var (
	titleHeadingRE  = regexp.MustCompile(`(?i)<span[^>]*role="heading"[^>]*>([\s\S]*?)</span>`)
	titleItemRE     = regexp.MustCompile(`(?i)<div[^>]*class="[^"]*s-item__title[^"]*"[^>]*>([\s\S]*?)</div>`)
	titleH3RE       = regexp.MustCompile(`(?i)<h3[^>]*s-item__title[^>]*>([\s\S]*?)</h3>`)
	titleCardRE     = regexp.MustCompile(`(?i)<div[^>]*class="[^"]*s-card__title[^"]*"[^>]*>([\s\S]*?)</div>`)
	titleCardLinkRE = regexp.MustCompile(`(?i)<a[^>]*class="[^"]*s-card__title-link[^"]*"[^>]*>([\s\S]*?)</a>`)
	priceItemRE     = regexp.MustCompile(`(?i)class="[^"]*s-item__price[^"]*"[^>]*>([^<]+)<`)
	priceCardRE     = regexp.MustCompile(`(?i)class="[^"]*s-card__price[^"]*"[^>]*>([^<]+)<`)
	priceAnyRE      = regexp.MustCompile(`(?i)[$£€¥]\s*[\d,]+(?:\.\d+)?`)
	urlItemRE       = regexp.MustCompile(`(?i)<a[^>]+class="[^"]*s-item__link[^"]*"[^>]+href="([^"]+)"`)
	urlCardRE       = regexp.MustCompile(`(?i)<a[^>]+class="[^"]*s-card__title-link[^"]*"[^>]+href="([^"]+)"`)
	urlEbayRE       = regexp.MustCompile(`(?i)<a[^>]+href="(https://www\.ebay\.com/itm[^"]+)"`)
	imgSrcRE        = regexp.MustCompile(`(?i)<img[^>]+src="([^"]+)"`)
	imgDataSrcRE    = regexp.MustCompile(`(?i)<img[^>]+data-src="([^"]+)"`)
	datePatterns    = []*regexp.Regexp{
		regexp.MustCompile(`(?i)Sold[\s\S]{0,400}?>\s*([A-Z][a-z]{2,9}\s+\d{1,2},\s*\d{4})`),
		regexp.MustCompile(`(?i)Sold\s+([A-Z][a-z]{2,9}\s+\d{1,2},\s*\d{4})`),
		regexp.MustCompile(`(?i)signal\s+POSITIVE[^>]*>([A-Z][a-z]{2,9}\s+\d{1,2},\s*\d{4})`),
		regexp.MustCompile(`([A-Z][a-z]{2,9}\s+\d{1,2},\s*\d{4})`),
	}
)

func parseScrapeBlock(blk string) *Item {
	title := extractScrapeTitle(blk)
	if title == "" || strings.EqualFold(title, "Shop on eBay") || strings.EqualFold(title, "Sponsored") {
		return nil
	}
	price, currency, raw := extractScrapePrice(blk)
	if price <= 0 {
		return nil
	}
	itemURL := extractScrapeURL(blk)
	if itemURL == "" {
		return nil
	}
	thumb := extractScrapeThumb(blk)
	soldDate := extractScrapeDate(blk)
	return &Item{
		Title:    title,
		URL:      itemURL,
		PriceUSD: price,
		Currency: currency,
		RawPrice: raw,
		SoldDate: soldDate,
		Thumbnail: thumb,
	}
}

func extractScrapeTitle(blk string) string {
	for _, re := range []*regexp.Regexp{titleHeadingRE, titleItemRE, titleH3RE, titleCardRE, titleCardLinkRE} {
		if m := re.FindStringSubmatch(blk); m != nil {
			return plainText(m[1])
		}
	}
	return ""
}

func extractScrapePrice(blk string) (float64, string, string) {
	var raw string
	for _, re := range []*regexp.Regexp{priceItemRE, priceCardRE, priceAnyRE} {
		if m := re.FindStringSubmatch(blk); m != nil {
			raw = htmlUnescape(m[1])
			break
		}
	}
	if raw == "" {
		return 0, "USD", ""
	}
	numRE := regexp.MustCompile(`[\d,]+(?:\.\d+)?`)
	num := numRE.FindString(raw)
	if num == "" {
		return 0, "USD", raw
	}
	price, _ := strconv.ParseFloat(strings.ReplaceAll(num, ",", ""), 64)
	currency := "USD"
	switch {
	case strings.Contains(raw, "£"):
		currency = "GBP"
	case strings.Contains(raw, "€"):
		currency = "EUR"
	case strings.Contains(raw, "¥"):
		currency = "JPY"
	case strings.Contains(raw, "THB"):
		currency = "THB"
	}
	return price, currency, raw
}

func extractScrapeURL(blk string) string {
	for _, re := range []*regexp.Regexp{urlItemRE, urlCardRE, urlEbayRE} {
		if m := re.FindStringSubmatch(blk); m != nil {
			return htmlUnescape(m[1])
		}
	}
	return ""
}

func extractScrapeThumb(blk string) string {
	for _, re := range []*regexp.Regexp{imgSrcRE, imgDataSrcRE} {
		if m := re.FindStringSubmatch(blk); m != nil {
			return htmlUnescape(m[1])
		}
	}
	return ""
}

func extractScrapeDate(blk string) string {
	for _, re := range datePatterns {
		if m := re.FindStringSubmatch(blk); m != nil {
			if t, err := time.Parse("Jan _2, 2006", m[1]); err == nil {
				return t.Format(time.RFC3339)
			}
		}
	}
	return ""
}

func plainText(s string) string {
	s = htmlUnescape(s)
	tagRE := regexp.MustCompile(`<[^>]+>`)
	s = tagRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}

func htmlUnescape(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return s
}

// SoldURL returns the public eBay sold-search URL for a query.
func SoldURL(q string) string {
	return fmt.Sprintf(soldPageURL, url.QueryEscape(q))
}

// ActiveURL returns the public eBay active-search URL for a query.
func ActiveURL(q string) string {
	return fmt.Sprintf(activePageURL, url.QueryEscape(q))
}
