package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jatibroski/sws-scanner-service/internal/config"
	httpdelivery "github.com/jatibroski/sws-scanner-service/internal/delivery/http"
	"github.com/jatibroski/sws-scanner-service/internal/health"
	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/anthropic"
	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/db"
	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/ebay"
	firebaseinfra "github.com/jatibroski/sws-scanner-service/internal/infrastructure/firebase"
	natsinfra "github.com/jatibroski/sws-scanner-service/internal/infrastructure/nats"
	"github.com/jatibroski/sws-scanner-service/internal/infrastructure/vision"
	auctionuc "github.com/jatibroski/sws-scanner-service/internal/usecase/auctions"
	contributionsuc "github.com/jatibroski/sws-scanner-service/internal/usecase/contributions"
	marketplaceuc "github.com/jatibroski/sws-scanner-service/internal/usecase/marketplace"
	pricinguc "github.com/jatibroski/sws-scanner-service/internal/usecase/pricing"
	scanuc "github.com/jatibroski/sws-scanner-service/internal/usecase/scan"
	utilityuc "github.com/jatibroski/sws-scanner-service/internal/usecase/utility"
	variantsuc "github.com/jatibroski/sws-scanner-service/internal/usecase/variants"
	visualmatchuc "github.com/jatibroski/sws-scanner-service/internal/usecase/visualmatch"
	"github.com/jatibroski/sws-scanner-service/migrations"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	pool, err := db.NewPool(ctx, cfg.DBURL)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	if err := migrations.Up(ctx, pool); err != nil {
		panic(err)
	}

	var firebaseApp *firebaseinfra.App
	if cfg.FirebaseServiceAccountB64 != "" {
		firebaseApp, err = firebaseinfra.NewApp(ctx, cfg.FirebaseServiceAccountB64, cfg.FirebaseStorageBucket)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to initialize firebase: %v\n", err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "warning: FIREBASE_SERVICE_ACCOUNT_B64 not set; firebase features disabled")
	}

	var firestore *firebaseinfra.Firestore
	var storage *firebaseinfra.Storage
	if firebaseApp != nil {
		firestore, err = firebaseApp.NewFirestore(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to initialize firestore: %v\n", err)
		}
		defer firestore.Close()
		storage = firebaseApp.NewStorage()
	}

	var publisher *natsinfra.Publisher
	if cfg.NATSURL != "" {
		publisher, err = natsinfra.NewPublisher(cfg.NATSURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to connect to nats: %v\n", err)
		}
	}

	anthropicClient := anthropic.NewClient(cfg.AnthropicAPIKey)
	visionClient := vision.NewClient(cfg.GoogleVisionAPIKey)
	ebayClient := ebay.NewClient(cfg.EbayAppID, cfg.EbayCertID)
	scanUC := scanuc.NewScanUseCase(anthropicClient, visionClient, firestore, storage)
	variantsUC := variantsuc.NewUseCase(firestore, storage)
	pricingUC := pricinguc.NewUseCase(ebayClient, visionClient, anthropicClient)
	utilityUC := utilityuc.NewUseCase(storage)
	marketplaceUC := marketplaceuc.NewUseCase(pool)
	auctionsUC := auctionuc.NewUseCase(pool)
	visualMatchUC := visualmatchuc.NewUseCase(visionClient, anthropicClient)
	contributionsUC := contributionsuc.NewUseCase(firebaseApp, storage, firestore, cfg.AdminEmails)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// CORS
	corsCfg := cors.DefaultConfig()
	allowAll := cfg.Env != "production" && len(cfg.CORSAllowedOrigins) == 0
	for _, o := range cfg.CORSAllowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
	}
	if allowAll {
		corsCfg.AllowAllOrigins = true
		corsCfg.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
		corsCfg.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization", "X-User-ID", "X-Mock-Auth-Key", "X-Cron-Secret"}
		corsCfg.AllowCredentials = false // wildcard + credentials is forbidden by browsers
	} else if len(cfg.CORSAllowedOrigins) > 0 {
		corsCfg.AllowOrigins = cfg.CORSAllowedOrigins
		corsCfg.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
		corsCfg.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization", "X-User-ID", "X-Mock-Auth-Key", "X-Cron-Secret"}
		corsCfg.AllowCredentials = true
	}
	r.Use(cors.New(corsCfg))

	// Request body size limit (matches Vercel serverless limits)
	const maxBodySize = 12 << 20 // 12 MB
	r.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodySize)
	})

	r.GET("/healthz", health.Liveness)
	r.GET("/readyz", health.Readiness(pool))

	handler := httpdelivery.NewHandler(cfg, pool, firebaseApp, publisher, scanUC, variantsUC, pricingUC, utilityUC, marketplaceUC, auctionsUC, visualMatchUC, contributionsUC)
	handler.RegisterRoutes(r)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		fmt.Printf("scanner service listening on :%s\n", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
	}
}
