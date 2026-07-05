package http

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jatibroski/sws-scanner-service/internal/config"
	firebaseinfra "github.com/jatibroski/sws-scanner-service/internal/infrastructure/firebase"
	natsinfra "github.com/jatibroski/sws-scanner-service/internal/infrastructure/nats"
	auctionuc "github.com/jatibroski/sws-scanner-service/internal/usecase/auctions"
	contributionsuc "github.com/jatibroski/sws-scanner-service/internal/usecase/contributions"
	marketplaceuc "github.com/jatibroski/sws-scanner-service/internal/usecase/marketplace"
	pricinguc "github.com/jatibroski/sws-scanner-service/internal/usecase/pricing"
	scanuc "github.com/jatibroski/sws-scanner-service/internal/usecase/scan"
	utilityuc "github.com/jatibroski/sws-scanner-service/internal/usecase/utility"
	variantsuc "github.com/jatibroski/sws-scanner-service/internal/usecase/variants"
	visualmatchuc "github.com/jatibroski/sws-scanner-service/internal/usecase/visualmatch"
)

// Handler holds HTTP handlers for the scanner service.
type Handler struct {
	cfg             *config.Config
	pool            *pgxpool.Pool
	firebase        *firebaseinfra.App
	publisher       *natsinfra.Publisher
	scanUC          *scanuc.UseCase
	variantsUC      *variantsuc.UseCase
	pricingUC       *pricinguc.UseCase
	utilityUC       *utilityuc.UseCase
	marketplaceUC   *marketplaceuc.UseCase
	auctionsUC      *auctionuc.UseCase
	visualMatchUC   *visualmatchuc.UseCase
	contributionsUC *contributionsuc.UseCase
}

// NewHandler creates a new HTTP handler.
func NewHandler(
	cfg *config.Config,
	pool *pgxpool.Pool,
	firebase *firebaseinfra.App,
	publisher *natsinfra.Publisher,
	scanUC *scanuc.UseCase,
	variantsUC *variantsuc.UseCase,
	pricingUC *pricinguc.UseCase,
	utilityUC *utilityuc.UseCase,
	marketplaceUC *marketplaceuc.UseCase,
	auctionsUC *auctionuc.UseCase,
	visualMatchUC *visualmatchuc.UseCase,
	contributionsUC *contributionsuc.UseCase,
) *Handler {
	return &Handler{
		cfg:             cfg,
		pool:            pool,
		firebase:        firebase,
		publisher:       publisher,
		scanUC:          scanUC,
		variantsUC:      variantsUC,
		pricingUC:       pricingUC,
		utilityUC:       utilityUC,
		marketplaceUC:   marketplaceUC,
		auctionsUC:      auctionsUC,
		visualMatchUC:   visualMatchUC,
		contributionsUC: contributionsUC,
	}
}

// RegisterRoutes registers all service routes.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	v1 := r.Group("/v1")
	{
		v1.GET("/whoami", h.WhoAmI)
		v1.GET("/fx", h.FX)
		v1.POST("/scan", h.Scan)
		v1.POST("/quality", h.Quality)
		v1.POST("/watermark", h.Watermark)
		v1.POST("/visual-match", h.VisualMatch)
		v1.POST("/scan-phash", h.ScanPhash)
		v1.GET("/prices", h.Prices)
		v1.GET("/op-variants", h.OPVariants)
		v1.GET("/op-details", h.OPDetails)
		v1.GET("/don-cards", h.DonCards)
		v1.GET("/cn-anniv-cards", h.CNAnnivCards)
		v1.POST("/contribute", h.Contribute)
		v1.POST("/contribute-sample", h.ContributeSample)
		v1.POST("/transactions", h.CreateTransaction)
		v1.GET("/transactions", h.ListTransactions)
		v1.POST("/auctions", h.CreateAuction)
		v1.GET("/auctions", h.ListAuctions)
		v1.POST("/auctions/:id/bid", h.PlaceBid)
		v1.POST("/auctions/tick", h.AuctionTick)
		v1.GET("/lookup-by-filename", h.LookupByFilename)
		v1.GET("/proxy-image", h.ProxyImage)
	}

	// Static reference assets served at root to match existing app paths.
	r.Static("/don-pdf", "./static/don-pdf")
	r.Static("/don-pdf-wm", "./static/don-pdf-wm")
	r.Static("/cn-anniv", "./static/cn-anniv")
	r.Static("/logos", "./static/logos")
}

func (h *Handler) notImplemented(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "NOT_IMPLEMENTED"})
}

// WhoAmI echoes back the authenticated user identity.
func (h *Handler) WhoAmI(c *gin.Context) {
	uid := h.currentUser(c)
	if uid == "" {
		uid = "anonymous"
	}
	if uid == "anonymous" {
		c.JSON(http.StatusOK, gin.H{"ok": true, "signedIn": false, "uid": "anonymous", "email": nil, "isAdmin": false})
		return
	}

	email := h.currentUserEmail(c)
	isAdmin := false
	if h.isMockAuth(c) {
		isAdmin = h.cfg.MockUserAdmin
	} else {
		for _, admin := range h.cfg.AdminEmails {
			if strings.EqualFold(strings.TrimSpace(admin), email) {
				isAdmin = true
				break
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "signedIn": true, "uid": uid, "email": email, "isAdmin": isAdmin})
}

// currentUser extracts the user ID from X-User-ID header, mock auth, or Firebase token.
func (h *Handler) currentUser(c *gin.Context) string {
	if h.isMockAuth(c) {
		return h.cfg.MockUserID
	}
	if id := c.GetHeader("X-User-ID"); id != "" {
		return id
	}
	if h.cfg.Env == "production" {
		return ""
	}
	// Dev fallback: verify Firebase token if provided.
	if h.firebase != nil {
		uid, err := h.firebase.VerifyIDToken(c.Request.Context(), c.GetHeader("Authorization"))
		if err == nil {
			return uid
		}
	}
	return ""
}

func (h *Handler) currentUserEmail(c *gin.Context) string {
	if h.isMockAuth(c) {
		return h.cfg.MockUserEmail
	}
	uid := h.currentUser(c)
	if uid == "" || h.firebase == nil {
		return ""
	}
	email, err := h.firebase.GetUserEmail(c.Request.Context(), uid)
	if err != nil {
		return ""
	}
	return email
}

// isMockAuth returns true when mock authentication is enabled and the caller
// supplied the expected secret key (if one is configured).
func (h *Handler) isMockAuth(c *gin.Context) bool {
	if !h.cfg.MockAuthEnabled {
		return false
	}
	if h.cfg.MockAuthKey != "" && c.GetHeader("X-Mock-Auth-Key") != h.cfg.MockAuthKey {
		return false
	}
	return true
}
