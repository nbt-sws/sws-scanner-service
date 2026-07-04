package http

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	auctionuc "github.com/jatibroski/sws-scanner-service/internal/usecase/auctions"
	contributionsuc "github.com/jatibroski/sws-scanner-service/internal/usecase/contributions"
	scannerimage "github.com/jatibroski/sws-scanner-service/internal/usecase/image"
	marketplaceuc "github.com/jatibroski/sws-scanner-service/internal/usecase/marketplace"
	scanuc "github.com/jatibroski/sws-scanner-service/internal/usecase/scan"
	utilityuc "github.com/jatibroski/sws-scanner-service/internal/usecase/utility"
	variantsuc "github.com/jatibroski/sws-scanner-service/internal/usecase/variants"
	visualmatchuc "github.com/jatibroski/sws-scanner-service/internal/usecase/visualmatch"
)

// Scan runs the full card-scanning pipeline.
func (h *Handler) Scan(c *gin.Context) {
	var req scanuc.ScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}

	resp, err := h.scanUC.Scan(c.Request.Context(), req, h.currentUser(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	if !resp.OK {
		c.JSON(http.StatusOK, resp)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Quality evaluates image quality metrics.
func (h *Handler) Quality(c *gin.Context) {
	var req scanuc.QualityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	resp, err := h.scanUC.Quality(c.Request.Context(), req, h.currentUser(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Watermark applies a vault/preview watermark.
func (h *Handler) Watermark(c *gin.Context) {
	var req scannerimage.WatermarkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	resp, err := scannerimage.ApplyWatermark(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// VisualMatch performs a web-detection based visual match.
func (h *Handler) VisualMatch(c *gin.Context) {
	if h.visualMatchUC == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "VISUAL_MATCH_NOT_AVAILABLE"})
		return
	}
	var req visualmatchuc.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	baseURL := absoluteBaseURL(c)
	req.ReferenceImageURL = resolveRelativeURL(req.ReferenceImageURL, baseURL)
	for i := range req.Candidates {
		req.Candidates[i].ImageURL = resolveRelativeURL(req.Candidates[i].ImageURL, baseURL)
	}
	resp, err := h.visualMatchUC.Match(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func absoluteBaseURL(c *gin.Context) string {
	scheme := c.Request.URL.Scheme
	if scheme == "" {
		scheme = "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
	}
	return scheme + "://" + c.Request.Host
}

func resolveRelativeURL(u, base string) string {
	if u == "" || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	if strings.HasPrefix(u, "/") {
		return base + u
	}
	return u
}

// ScanPhash performs a perceptual hash lookup.
func (h *Handler) ScanPhash(c *gin.Context) {
	var req scanuc.PHashRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	resp, err := h.scanUC.PHashLookup(c.Request.Context(), req.Image)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Prices returns market pricing data.
func (h *Handler) Prices(c *gin.Context) {
	query := map[string]string{}
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			query[k] = v[0]
		}
	}
	c.JSON(http.StatusOK, h.pricingUC.Prices(c.Request.Context(), query))
}

// OPVariants returns One Piece card variants.
func (h *Handler) OPVariants(c *gin.Context) {
	var req variantsuc.OPVariantsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, h.variantsUC.OPVariants(c.Request.Context(), req))
}

// OPDetails returns detailed card metadata.
func (h *Handler) OPDetails(c *gin.Context) {
	var req variantsuc.OPDetailsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, h.variantsUC.OPDetails(c.Request.Context(), req))
}

// DonCards returns DON card reference data.
func (h *Handler) DonCards(c *gin.Context) {
	var req variantsuc.DonCardsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	c.Header("Cache-Control", "public, max-age=3600, s-maxage=3600")
	c.JSON(http.StatusOK, h.variantsUC.DonCards(c.Request.Context(), req))
}

// CNAnnivCards returns Chinese anniversary card reference data.
func (h *Handler) CNAnnivCards(c *gin.Context) {
	var req variantsuc.CNAnnivCardsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	c.Header("Cache-Control", "public, max-age=3600, s-maxage=3600")
	c.JSON(http.StatusOK, h.variantsUC.CNAnnivCards(c.Request.Context(), req))
}

// Contribute accepts a community contribution.
func (h *Handler) Contribute(c *gin.Context) {
	if h.contributionsUC == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "CONTRIBUTIONS_NOT_AVAILABLE"})
		return
	}
	userID := h.currentUser(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHORIZED", "details": "Sign in to contribute"})
		return
	}
	var req contributionsuc.ContributeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	resp, err := h.contributionsUC.Contribute(c.Request.Context(), userID, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ContributeSample accepts a sample image contribution.
func (h *Handler) ContributeSample(c *gin.Context) {
	if h.contributionsUC == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "CONTRIBUTIONS_NOT_AVAILABLE"})
		return
	}
	userID := h.currentUser(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHORIZED", "details": "Sign in to contribute"})
		return
	}
	var req contributionsuc.ContributeSampleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	email := h.currentUserEmail(c)
	resp, err := h.contributionsUC.ContributeSample(c.Request.Context(), userID, email, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// CreateTransaction creates a marketplace transaction or returns a fee preview.
// Deprecated: marketplace logic is moving to sws-svc-swap-order.
func (h *Handler) CreateTransaction(c *gin.Context) {
	c.Header("Deprecation", "true")
	switch c.Query("action") {
	case "preview-fees":
		var req marketplaceuc.FeePreviewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
			return
		}
		c.JSON(http.StatusOK, h.marketplaceUC.PreviewFees(req))
		return
	case "preview-chain":
		var req marketplaceuc.PreviewChainRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
			return
		}
		c.JSON(http.StatusOK, h.marketplaceUC.PreviewChain(req))
		return
	}

	var req marketplaceuc.TransactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	resp, err := h.marketplaceUC.CreateTransaction(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ListTransactions lists marketplace transactions.
// Deprecated: marketplace logic is moving to sws-svc-swap-order.
func (h *Handler) ListTransactions(c *gin.Context) {
	c.Header("Deprecation", "true")
	var req marketplaceuc.ListTransactionsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	resp, err := h.marketplaceUC.ListTransactions(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// CreateAuction creates a new auction.
// Deprecated: auction logic is moving to sws-svc-swap-listing.
func (h *Handler) CreateAuction(c *gin.Context) {
	c.Header("Deprecation", "true")
	var req auctionuc.CreateAuctionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	resp, err := h.auctionsUC.CreateAuction(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ListAuctions lists auctions.
// Deprecated: auction logic is moving to sws-svc-swap-listing.
func (h *Handler) ListAuctions(c *gin.Context) {
	c.Header("Deprecation", "true")
	resp, err := h.auctionsUC.ListAuctions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// PlaceBid places a bid on an auction.
// Deprecated: auction logic is moving to sws-svc-swap-listing.
func (h *Handler) PlaceBid(c *gin.Context) {
	c.Header("Deprecation", "true")
	var req auctionuc.PlaceBidRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	req.AuctionID = c.Param("id")
	resp, err := h.auctionsUC.PlaceBid(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// AuctionTick advances auction state (cron endpoint).
// Deprecated: auction logic is moving to sws-svc-swap-listing.
func (h *Handler) AuctionTick(c *gin.Context) {
	c.Header("Deprecation", "true")
	if h.cfg.CronSecret != "" && c.GetHeader("X-Cron-Secret") != h.cfg.CronSecret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHORIZED"})
		return
	}
	if err := h.auctionsUC.Tick(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// LookupByFilename resolves a card by filename.
func (h *Handler) LookupByFilename(c *gin.Context) {
	var req utilityuc.LookupRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, h.utilityUC.LookupByFilename(c.Request.Context(), req))
}

// ProxyImage proxies an external image URL.
func (h *Handler) ProxyImage(c *gin.Context) {
	url := c.Query("url")
	data, contentType, err := h.utilityUC.ProxyImage(url)
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "host not allowed" {
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.Header("Cache-Control", "public, max-age=86400, immutable")
	c.Data(http.StatusOK, contentType, data)
}

// FX returns foreign exchange rates.
func (h *Handler) FX(c *gin.Context) {
	base := c.Query("base")
	resp, err := h.pricingUC.FX(c.Request.Context(), base)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "FX_ERROR", "details": err.Error()})
		return
	}
	c.Header("Cache-Control", "public, max-age=3600, s-maxage=3600")
	c.JSON(http.StatusOK, resp)
}
