package auctions

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UseCase handles auction operations.
type UseCase struct {
	pool *pgxpool.Pool
}

// NewUseCase creates an auctions use case.
func NewUseCase(pool *pgxpool.Pool) *UseCase {
	return &UseCase{pool: pool}
}

// CreateAuctionRequest is the input for creating an auction.
type CreateAuctionRequest struct {
	ExternalID     string  `json:"externalId"`
	SellerID       string  `json:"sellerId"`
	Title          string  `json:"title"`
	Description    string  `json:"description,omitempty"`
	StartingPrice  float64 `json:"startingPrice"`
	CurrentPrice   float64 `json:"currentPrice,omitempty"`
	ReservePrice   float64 `json:"reservePrice,omitempty"`
	Currency       string  `json:"currency,omitempty"`
	EndsAt         time.Time `json:"endsAt"`
}

// CreateAuctionResponse is the created auction result.
type CreateAuctionResponse struct {
	OK      bool                   `json:"ok"`
	ID      string                 `json:"id,omitempty"`
	Auction map[string]interface{} `json:"auction,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// CreateAuction persists a new auction.
func (uc *UseCase) CreateAuction(ctx context.Context, req CreateAuctionRequest) (*CreateAuctionResponse, error) {
	if uc.pool == nil {
		return &CreateAuctionResponse{OK: false, Error: "database not available"}, nil
	}
	if req.ExternalID == "" {
		req.ExternalID = uuid.New().String()
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	if req.CurrentPrice == 0 {
		req.CurrentPrice = req.StartingPrice
	}
	id := uuid.New().String()
	_, err := uc.pool.Exec(ctx, `
		INSERT INTO auctions (id, external_id, seller_id, title, description, starting_price,
			current_price, reserve_price, currency, status, ends_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'active', $10)
	`, id, req.ExternalID, req.SellerID, req.Title, req.Description, req.StartingPrice,
		req.CurrentPrice, req.ReservePrice, req.Currency, req.EndsAt)
	if err != nil {
		return nil, fmt.Errorf("insert auction: %w", err)
	}
	return &CreateAuctionResponse{OK: true, ID: id}, nil
}

// ListAuctionsResponse is the list result.
type ListAuctionsResponse struct {
	OK      bool                     `json:"ok"`
	Count   int                      `json:"count"`
	Auctions []map[string]interface{} `json:"auctions"`
}

// ListAuctions returns active auctions.
func (uc *UseCase) ListAuctions(ctx context.Context) (*ListAuctionsResponse, error) {
	if uc.pool == nil {
		return &ListAuctionsResponse{OK: true, Auctions: []map[string]interface{}{}}, nil
	}
	rows, err := uc.pool.Query(ctx, `
		SELECT id, external_id, seller_id, title, description, starting_price,
			current_price, reserve_price, currency, status, ends_at, created_at
		FROM auctions WHERE status = 'active' ORDER BY ends_at ASC LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]interface{}
	for rows.Next() {
		var id, externalID, sellerID, title, description, currency, status string
		var startingPrice, currentPrice, reservePrice float64
		var endsAt, createdAt time.Time
		if err := rows.Scan(&id, &externalID, &sellerID, &title, &description, &startingPrice,
			&currentPrice, &reservePrice, &currency, &status, &endsAt, &createdAt); err != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"id":            id,
			"externalId":    externalID,
			"sellerId":      sellerID,
			"title":         title,
			"description":   description,
			"startingPrice": startingPrice,
			"currentPrice":  currentPrice,
			"reservePrice":  reservePrice,
			"currency":      currency,
			"status":        status,
			"endsAt":        endsAt,
			"createdAt":     createdAt,
		})
	}
	return &ListAuctionsResponse{OK: true, Count: len(out), Auctions: out}, nil
}

// PlaceBidRequest is the input for placing a bid.
type PlaceBidRequest struct {
	AuctionID string  `json:"auctionId"`
	BidderID  string  `json:"bidderId"`
	Amount    float64 `json:"amount"`
}

// PlaceBidResponse is the bid result.
type PlaceBidResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// PlaceBid records a new bid and updates current price.
func (uc *UseCase) PlaceBid(ctx context.Context, req PlaceBidRequest) (*PlaceBidResponse, error) {
	if uc.pool == nil {
		return &PlaceBidResponse{OK: false, Error: "database not available"}, nil
	}
	if req.Amount <= 0 {
		return &PlaceBidResponse{OK: false, Error: "invalid bid amount"}, nil
	}

	tx, err := uc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentPrice float64
	err = tx.QueryRow(ctx, `SELECT current_price FROM auctions WHERE id = $1 AND status = 'active'`, req.AuctionID).Scan(&currentPrice)
	if err != nil {
		return &PlaceBidResponse{OK: false, Error: "auction not found or not active"}, nil
	}
	minBid := currentPrice * 1.05
	if req.Amount < minBid {
		return &PlaceBidResponse{OK: false, Error: fmt.Sprintf("bid must be at least %.2f", minBid)}, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO auction_bids (auction_id, bidder_id, amount) VALUES ($1, $2, $3)`, req.AuctionID, req.BidderID, req.Amount); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE auctions SET current_price = $1 WHERE id = $2`, req.Amount, req.AuctionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &PlaceBidResponse{OK: true}, nil
}

// Tick advances auctions that have ended.
func (uc *UseCase) Tick(ctx context.Context) error {
	if uc.pool == nil {
		return nil
	}
	_, err := uc.pool.Exec(ctx, `UPDATE auctions SET status = 'ended' WHERE status = 'active' AND ends_at < NOW()`)
	return err
}
