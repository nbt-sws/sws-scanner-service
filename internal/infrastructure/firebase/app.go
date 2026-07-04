package firebase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/storage"
	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"
)

// App wraps a Firebase Admin SDK app and provides storage helpers.
type App struct {
	App           *firebase.App
	StorageBucket string
}

// NewApp initializes the Firebase Admin SDK from a base64-encoded service account JSON.
func NewApp(ctx context.Context, serviceAccountB64, storageBucket string) (*App, error) {
	saJSON, err := base64.StdEncoding.DecodeString(serviceAccountB64)
	if err != nil {
		return nil, fmt.Errorf("decode service account: %w", err)
	}

	var sa map[string]interface{}
	if err := json.Unmarshal(saJSON, &sa); err != nil {
		return nil, fmt.Errorf("parse service account: %w", err)
	}

	cfg := &firebase.Config{StorageBucket: storageBucket}
	opt := option.WithCredentialsJSON(saJSON)
	app, err := firebase.NewApp(ctx, cfg, opt)
	if err != nil {
		return nil, fmt.Errorf("init firebase app: %w", err)
	}

	return &App{App: app, StorageBucket: storageBucket}, nil
}

// FirestoreClient returns the Firestore client.
func (a *App) FirestoreClient(ctx context.Context) (*firestore.Client, error) {
	return a.App.Firestore(ctx)
}

// StorageClient returns the Firebase Storage client.
func (a *App) StorageClient(ctx context.Context) (*storage.Client, error) {
	return a.App.Storage(ctx)
}
