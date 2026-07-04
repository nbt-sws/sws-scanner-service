package firebase

import (
	"context"
	"fmt"
	"strings"
)

// GetUserEmail returns the email of a Firebase user by UID.
func (a *App) GetUserEmail(ctx context.Context, uid string) (string, error) {
	if a == nil || a.App == nil {
		return "", fmt.Errorf("firebase not initialized")
	}
	authClient, err := a.App.Auth(ctx)
	if err != nil {
		return "", fmt.Errorf("firebase auth client: %w", err)
	}
	u, err := authClient.GetUser(ctx, uid)
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	return u.Email, nil
}

// VerifyIDToken verifies a Firebase ID token and returns the user's UID.
func (a *App) VerifyIDToken(ctx context.Context, authHeader string) (string, error) {
	if a == nil || a.App == nil {
		return "", fmt.Errorf("firebase not initialized")
	}
	if authHeader == "" {
		return "", fmt.Errorf("missing authorization header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", fmt.Errorf("invalid authorization header format")
	}
	token := parts[1]

	authClient, err := a.App.Auth(ctx)
	if err != nil {
		return "", fmt.Errorf("firebase auth client: %w", err)
	}

	decoded, err := authClient.VerifyIDToken(ctx, token)
	if err != nil {
		return "", fmt.Errorf("verify id token: %w", err)
	}
	if decoded == nil || decoded.UID == "" {
		return "", fmt.Errorf("empty token uid")
	}
	return decoded.UID, nil
}
