package marketplace

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UseCase handles marketplace transactions and fee previews.
type UseCase struct {
	pool *pgxpool.Pool
}

// NewUseCase creates a marketplace use case.
func NewUseCase(pool *pgxpool.Pool) *UseCase {
	return &UseCase{pool: pool}
}

// TransactionRequest is the input for creating a transaction.
type TransactionRequest struct {
	SellerID      string  `json:"sellerId"`
	BuyerID       string  `json:"buyerId,omitempty"`
	ItemID        string  `json:"itemId,omitempty"`
	ItemType      string  `json:"itemType,omitempty"`
	Price         float64 `json:"price"`
	Currency      string  `json:"currency,omitempty"`
	BuyerTier     string  `json:"buyerTier,omitempty"`
	SellerTier    string  `json:"sellerTier,omitempty"`
	TxType        string  `json:"txType"`
	DeliveryMode  string  `json:"deliveryMode,omitempty"`
	PaymentMethod string  `json:"paymentMethod,omitempty"`
	SellerCost    float64 `json:"sellerCost,omitempty"`
	FeeOverride   float64 `json:"feeOverride,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// TransactionResponse is the created transaction result.
type TransactionResponse struct {
	OK          bool                   `json:"ok"`
	ID          string                 `json:"id,omitempty"`
	Transaction map[string]interface{} `json:"transaction,omitempty"`
	Error       string                 `json:"error,omitempty"`
}

// CreateTransaction persists a marketplace transaction.
func (uc *UseCase) CreateTransaction(ctx context.Context, req TransactionRequest) (*TransactionResponse, error) {
	if uc.pool == nil {
		return &TransactionResponse{OK: false, Error: "database not available"}, nil
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	fees := map[string]interface{}{"placeholder": true}
	id := uuid.New().String()
	_, err := uc.pool.Exec(ctx, `
		INSERT INTO transactions (id, seller_id, buyer_id, item_id, item_type, price, currency,
			buyer_tier, seller_tier, tx_type, delivery_mode, payment_method, seller_cost,
			fee_override, fees, metadata, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, 'pending')
	`, id, req.SellerID, req.BuyerID, req.ItemID, req.ItemType, req.Price, req.Currency,
		req.BuyerTier, req.SellerTier, req.TxType, req.DeliveryMode, req.PaymentMethod,
		req.SellerCost, req.FeeOverride, fees, req.Metadata)
	if err != nil {
		return nil, fmt.Errorf("insert transaction: %w", err)
	}
	return &TransactionResponse{OK: true, ID: id}, nil
}

// ListTransactionsRequest holds filter parameters.
type ListTransactionsRequest struct {
	SellerID string `form:"sellerId"`
	BuyerID  string `form:"buyerId"`
	Code     string `form:"code"`
	Rarity   string `form:"rarity"`
}

// ListTransactionsResponse is the list result.
type ListTransactionsResponse struct {
	OK           bool                     `json:"ok"`
	Count        int                      `json:"count"`
	Transactions []map[string]interface{} `json:"transactions"`
}

// ListTransactions queries marketplace transactions.
func (uc *UseCase) ListTransactions(ctx context.Context, req ListTransactionsRequest) (*ListTransactionsResponse, error) {
	if uc.pool == nil {
		return &ListTransactionsResponse{OK: true, Transactions: []map[string]interface{}{}}, nil
	}
	query := `SELECT id, seller_id, buyer_id, item_id, price, currency, tx_type, status, fees, metadata, created_at FROM transactions WHERE 1=1`
	args := []interface{}{}
	argIdx := 1
	if req.SellerID != "" {
		query += fmt.Sprintf(" AND seller_id = $%d", argIdx)
		args = append(args, req.SellerID)
		argIdx++
	}
	if req.BuyerID != "" {
		query += fmt.Sprintf(" AND buyer_id = $%d", argIdx)
		args = append(args, req.BuyerID)
		argIdx++
	}
	query += " ORDER BY created_at DESC LIMIT 100"

	rows, err := uc.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]interface{}
	for rows.Next() {
		var id, sellerID, buyerID, itemID, currency, txType, status string
		var price float64
		var fees, metadata map[string]interface{}
		var createdAt time.Time
		if err := rows.Scan(&id, &sellerID, &buyerID, &itemID, &price, &currency, &txType, &status, &fees, &metadata, &createdAt); err != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"id":         id,
			"sellerId":   sellerID,
			"buyerId":    buyerID,
			"itemId":     itemID,
			"price":      price,
			"currency":   currency,
			"txType":     txType,
			"status":     status,
			"fees":       fees,
			"metadata":   metadata,
			"createdAt":  createdAt,
		})
	}
	return &ListTransactionsResponse{OK: true, Count: len(out), Transactions: out}, nil
}

// FeePreviewRequest is the input for fee preview.
type FeePreviewRequest struct {
	Price        float64 `json:"price"`
	BuyerTier    string  `json:"buyerTier"`
	SellerTier   string  `json:"sellerTier"`
	TxType       string  `json:"txType"`
	DeliveryMode string  `json:"deliveryMode"`
	Payment      string  `json:"payment"`
	SellerCost   float64 `json:"sellerCost"`
	FeeOverride  float64 `json:"feeOverride"`
}

// FeePreviewResponse is the fee preview result.
type FeePreviewResponse struct {
	OK     bool                   `json:"ok"`
	Fees   map[string]interface{} `json:"fees"`
	Notice string                 `json:"notice,omitempty"`
}

// PreviewFees calculates a real single-transaction fee breakdown using the
// ported NBT Rate 3 fee engine.
func (uc *UseCase) PreviewFees(req FeePreviewRequest) *FeePreviewResponse {
	cfg := DefaultFeeConfig()
	result := Calc(
		req.Price,
		req.BuyerTier,
		req.SellerTier,
		req.TxType,
		req.DeliveryMode,
		req.Payment,
		req.SellerCost,
		req.FeeOverride,
		cfg.Constants,
		cfg.BSARates,
		false,
	)
	return &FeePreviewResponse{OK: true, Fees: resultToMap(result)}
}

// PreviewChainRequest is the input for a consignment chain preview.
type PreviewChainRequest struct {
	Price            float64    `json:"price"`
	BuyerTier        string     `json:"buyerTier"`
	SellerTier       string     `json:"sellerTier"`
	TxType           string     `json:"txType"`
	Payment          string     `json:"payment"`
	Markup           float64    `json:"markup"`
	FeeOverride      float64    `json:"feeOverride"`
	SubsequentPayback bool      `json:"subsequentPayback"`
	ChainHops        []ChainHop `json:"chainHops"`
}

// PreviewChain calculates a multi-hop consignment chain breakdown.
func (uc *UseCase) PreviewChain(req PreviewChainRequest) *ChainResult {
	cfg := DefaultFeeConfig()
	return CalcChain(
		req.Price,
		req.BuyerTier,
		req.SellerTier,
		req.TxType,
		req.Payment,
		req.ChainHops,
		req.Markup,
		req.FeeOverride,
		cfg.Constants,
		cfg.BSARates,
		req.SubsequentPayback,
	)
}

func resultToMap(r *CalcResult) map[string]interface{} {
	return map[string]interface{}{
		"price":             r.Price,
		"buyerTier":         r.BuyerTier,
		"txType":            r.TxType,
		"deliveryMode":      r.DeliveryMode,
		"payment":           r.Payment,
		"sellerCost":        r.SellerCost,
		"isAuction":         r.IsAuction,
		"isConsign":         r.IsConsign,
		"bracket":           r.Bracket,
		"buyerFeeRate":      r.BuyerFeeRate,
		"sellerFeeRate":     r.SellerFeeRate,
		"buyerFeeShare":     r.BuyerFeeShare,
		"consignExtra":      r.ConsignExtra,
		"shippingCharge":    r.ShippingCharge,
		"buyerSubtotal":     r.BuyerSubtotal,
		"ppDiscountAmt":     r.PPDiscountAmt,
		"buyerTotal":        r.BuyerTotal,
		"sellerFeeShare":    r.SellerFeeShare,
		"consignPayback":    r.ConsignPayback,
		"sellerReceives":    r.SellerReceives,
		"sellerProfit":      r.SellerProfit,
		"sellerGM":          r.SellerGM,
		"totalFeeRevenue":   r.TotalFeeRevenue,
		"shippingCollected": r.ShippingCollected,
		"shippingCost":      r.ShippingCost,
		"shippingNet":       r.ShippingNet,
		"paymentFee":        r.PaymentFee,
		"vatOnFee":          r.VATOnFee,
		"platformNet":       r.PlatformNet,
		"platformGM":        r.PlatformGM,
	}
}
