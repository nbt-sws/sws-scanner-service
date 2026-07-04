package health

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Pinger is implemented by *pgxpool.Pool and other connection pools.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Liveness returns a basic alive probe.
func Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "alive"})
}

// Readiness returns a ready probe that pings the provided data source.
func Readiness(pinger Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if pinger != nil {
			if err := pinger.Ping(c.Request.Context()); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
