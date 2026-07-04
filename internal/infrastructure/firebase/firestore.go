package firebase

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
)

// Firestore provides typed helpers for scanner-related Firestore operations.
type Firestore struct {
	client *firestore.Client
}

// NewFirestore creates a Firestore wrapper from an initialized App.
func (a *App) NewFirestore(ctx context.Context) (*Firestore, error) {
	if a == nil || a.App == nil {
		return nil, fmt.Errorf("firebase not initialized")
	}
	client, err := a.App.Firestore(ctx)
	if err != nil {
		return nil, fmt.Errorf("firestore client: %w", err)
	}
	return &Firestore{client: client}, nil
}

// Close closes the Firestore client.
func (f *Firestore) Close() error {
	if f == nil || f.client == nil {
		return nil
	}
	return f.client.Close()
}

// ScanCacheDoc represents a cached scan result in Firestore.
type ScanCacheDoc struct {
	ImageHash    string                 `firestore:"imageHash"`
	CacheVersion string                 `firestore:"cacheVersion"`
	Status       string                 `firestore:"status"`
	Card         map[string]interface{} `firestore:"card"`
	RawResponse  map[string]interface{} `firestore:"rawResponse"`
	CorrectedBy  string                 `firestore:"correctedBy"`
	CreatedAt    time.Time              `firestore:"createdAt"`
}

// GetScanCache reads the exact-image cache document.
func (f *Firestore) GetScanCache(ctx context.Context, hash string) (*ScanCacheDoc, error) {
	if f == nil || f.client == nil {
		return nil, fmt.Errorf("firestore not initialized")
	}
	doc, err := f.client.Collection("scans").Doc(hash).Get(ctx)
	if err != nil {
		return nil, err
	}
	var cache ScanCacheDoc
	if err := doc.DataTo(&cache); err != nil {
		return nil, fmt.Errorf("decode scan cache: %w", err)
	}
	cache.ImageHash = doc.Ref.ID
	return &cache, nil
}

// PutScanCache writes or merges a cache document.
func (f *Firestore) PutScanCache(ctx context.Context, hash string, payload map[string]interface{}) error {
	if f == nil || f.client == nil {
		return fmt.Errorf("firestore not initialized")
	}
	payload["createdAt"] = firestore.ServerTimestamp
	_, err := f.client.Collection("scans").Doc(hash).Set(ctx, payload, firestore.MergeAll)
	return err
}

// VerifiedCardDoc represents a community-verified card.
type VerifiedCardDoc struct {
	DocKey   string                 `firestore:"-"`
	Code     string                 `firestore:"code"`
	Rarity   string                 `firestore:"rarity"`
	NameEn   string                 `firestore:"nameEn"`
	NameJp   string                 `firestore:"nameJp"`
	NameCn   string                 `firestore:"nameCn"`
	Type     string                 `firestore:"type"`
	Phash    string                 `firestore:"phash"`
	Samples  map[string]interface{} `firestore:"samples"`
	Official map[string]interface{} `firestore:"official"`
	Data     map[string]interface{} `firestore:"-"`
}

// GetVerifiedCard reads a verified card document by canonical key.
func (f *Firestore) GetVerifiedCard(ctx context.Context, key string) (*VerifiedCardDoc, error) {
	if f == nil || f.client == nil {
		return nil, fmt.Errorf("firestore not initialized")
	}
	doc, err := f.client.Collection("verified_cards").Doc(key).Get(ctx)
	if err != nil {
		return nil, err
	}
	var v VerifiedCardDoc
	if err := doc.DataTo(&v); err != nil {
		return nil, fmt.Errorf("decode verified card: %w", err)
	}
	v.DocKey = doc.Ref.ID
	v.Data = doc.Data()
	return &v, nil
}

// FindVerifiedCardsWithPHash returns verified cards that have a non-null phash.
func (f *Firestore) FindVerifiedCardsWithPHash(ctx context.Context, limit int) ([]*VerifiedCardDoc, error) {
	if f == nil || f.client == nil {
		return nil, fmt.Errorf("firestore not initialized")
	}
	if limit <= 0 {
		limit = 5000
	}
	iter := f.client.Collection("verified_cards").Where("phash", "!=", nil).Limit(limit).Documents(ctx)
	defer iter.Stop()

	var out []*VerifiedCardDoc
	for {
		doc, err := iter.Next()
		if err != nil {
			if err.Error() == "iterator done" {
				break
			}
			return nil, err
		}
		var v VerifiedCardDoc
		if err := doc.DataTo(&v); err != nil {
			continue
		}
		v.DocKey = doc.Ref.ID
		v.Data = doc.Data()
		out = append(out, &v)
	}
	return out, nil
}

// UpsertVerifiedCard merges data into a verified_cards document.
func (f *Firestore) UpsertVerifiedCard(ctx context.Context, key string, data map[string]interface{}) error {
	if f == nil || f.client == nil {
		return fmt.Errorf("firestore not initialized")
	}
	_, err := f.client.Collection("verified_cards").Doc(key).Set(ctx, data, firestore.MergeAll)
	return err
}

// PatchScanCache merges data into a scans cache document.
func (f *Firestore) PatchScanCache(ctx context.Context, hash string, data map[string]interface{}) error {
	if f == nil || f.client == nil {
		return fmt.Errorf("firestore not initialized")
	}
	_, err := f.client.Collection("scans").Doc(hash).Set(ctx, data, firestore.MergeAll)
	return err
}

// FindContributionsByPHash searches contributions subcollection group by phash.
func (f *Firestore) FindContributionsByPHash(ctx context.Context, phash string) (*firestore.DocumentSnapshot, error) {
	if f == nil || f.client == nil {
		return nil, fmt.Errorf("firestore not initialized")
	}
	iter := f.client.CollectionGroup("contributions").Where("pHash", "==", phash).Limit(1).Documents(ctx)
	defer iter.Stop()
	doc, err := iter.Next()
	if err != nil {
		return nil, err
	}
	return doc, nil
}
