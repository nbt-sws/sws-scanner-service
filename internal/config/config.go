package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all service configuration loaded from environment variables.
type Config struct {
	Env       string
	Port      string
	GRPCPort  string
	DBURL     string
	RedisAddr string
	NATSURL   string
	LogLevel  string

	// Firebase
	FirebaseServiceAccountB64 string
	FirebaseStorageBucket     string

	// External APIs
	AnthropicAPIKey   string
	GoogleVisionAPIKey string
	EbayAppID         string
	EbayCertID        string

	// Service behavior
	CORSAllowedOrigins []string
	AdminEmails        []string
	AuctionRequireKYC  bool
	CronSecret         string

	// Mock auth (temporary bypass for pre-vault/user prod testing)
	MockAuthEnabled bool
	MockUserID      string
	MockUserEmail   string
	MockUserAdmin   bool
	MockAuthKey     string
}

// Load reads configuration from environment variables.
// It attempts to load a .env file in development, but never fails if the file is missing.
func Load() *Config {
	if os.Getenv("ENV") != "production" {
		_ = godotenv.Load(".env", ".env.local")
	}

	cfg := &Config{
		Env:       getEnv("ENV", "production"),
		Port:      getEnv("PORT", "8080"),
		GRPCPort:  getEnv("GRPC_PORT", "9090"),
		DBURL:     getEnv("DATABASE_URL", ""),
		RedisAddr: getEnv("REDIS_ADDR", "localhost:6379"),
		NATSURL:   getEnv("NATS_URL", "nats://localhost:4222"),
		LogLevel:  getEnv("LOG_LEVEL", "info"),

		FirebaseServiceAccountB64: getEnv("FIREBASE_SERVICE_ACCOUNT_B64", ""),
		FirebaseStorageBucket:     getEnv("FIREBASE_STORAGE_BUCKET", ""),

		AnthropicAPIKey:    getEnv("ANTHROPIC_API_KEY", ""),
		GoogleVisionAPIKey: getEnv("GOOGLE_VISION_API_KEY", ""),
		EbayAppID:          getEnv("EBAY_APP_ID", ""),
		EbayCertID:         getEnv("EBAY_CERT_ID", ""),

		CORSAllowedOrigins: splitEnv(getEnv("CORS_ALLOWED_ORIGINS", "")),
		AdminEmails:        splitEnv(getEnv("ADMIN_EMAILS", "")),
		AuctionRequireKYC:  getEnvBool("AUCTION_REQUIRE_KYC", false),
		CronSecret:         getEnv("CRON_SECRET", ""),

		MockAuthEnabled: getEnvBool("MOCK_AUTH_ENABLED", false),
		MockUserID:      getEnv("MOCK_USER_ID", ""),
		MockUserEmail:   getEnv("MOCK_USER_EMAIL", ""),
		MockUserAdmin:   getEnvBool("MOCK_USER_ADMIN", false),
		MockAuthKey:     getEnv("MOCK_AUTH_KEY", ""),
	}

	if cfg.DBURL == "" {
		panic("DATABASE_URL is required")
	}

	return cfg
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			panic(fmt.Sprintf("invalid integer for %s: %v", key, err))
		}
		return n
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			panic(fmt.Sprintf("invalid boolean for %s: %v", key, err))
		}
		return b
	}
	return defaultVal
}

func splitEnv(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
