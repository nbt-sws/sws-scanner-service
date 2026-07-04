package marketplace

import (
	"fmt"
	"math"
)

// Fee engine port of src/lib/fees/calc.js + config.js (NBT Rate 3).
// All calculations use float64 THB amounts and match the JS implementation.

// TierIndex maps tier names to BSA rate table indices.
var TierIndex = map[string]int{
	"user":     0,
	"silver":   1,
	"gold":     2,
	"platinum": 3,
}

// FeeConstants holds per-platform economic constants.
type FeeConstants struct {
	ShippingCharge       float64   `json:"shippingCharge"`
	ShippingCost         float64   `json:"shippingCost"`
	CCFee                float64   `json:"ccFee"`
	PPFee                float64   `json:"ppFee"`
	PPDisc               float64   `json:"ppDisc"`
	VAT                  float64   `json:"vat"`
	ConsignExtraRate     float64   `json:"consignExtraRate"`
	ConsignPayback       float64   `json:"consignPayback"`
	ConsignDecayRates    []float64 `json:"consignDecayRates"`
	AuctionSellerFeeRate float64   `json:"auctionSellerFeeRate"`
}

// BSABracket holds buyer/seller fee rates for a price bracket.
type BSABracket struct {
	Flat  bool      `json:"flat"`
	Rates []float64 `json:"rates"`
}

// FeeConfig is the canonical fee configuration (NBT Rate 3).
type FeeConfig struct {
	Name      string                `json:"name"`
	Version   string                `json:"version"`
	SavedAt   string                `json:"savedAt"`
	Constants FeeConstants          `json:"constants"`
	BSARates  map[string]BSABracket `json:"bsaRates"`
}

// DefaultFeeConfig returns the locked NBT Rate 3 configuration.
func DefaultFeeConfig() *FeeConfig {
	return &FeeConfig{
		Name:    "NBT Rate 3",
		Version: "1.0",
		SavedAt: "2026-05-08T16:43:33.645Z",
		Constants: FeeConstants{
			ShippingCharge:       50,
			ShippingCost:         30,
			CCFee:                0.035,
			PPFee:                0.01,
			PPDisc:               0.025,
			VAT:                  0.07,
			ConsignExtraRate:     0.025,
			ConsignPayback:       0,
			ConsignDecayRates:    []float64{0.0042, 0.006, 0.01, 0.015, 0.018, 0.02, 0.025},
			AuctionSellerFeeRate: 0.5,
		},
		BSARates: map[string]BSABracket{
			"100":   {Flat: true, Rates: []float64{6, 5, 3.5, 2.25}},
			"15000": {Flat: false, Rates: []float64{0.15, 0.08, 0.065, 0.035}},
			"50000": {Flat: false, Rates: []float64{0.13, 0.075, 0.06, 0.035}},
			"50001": {Flat: false, Rates: []float64{0.12, 0.07, 0.055, 0.035}},
		},
	}
}

func r2(n float64) float64 {
	return math.Round(n*100) / 100
}

func getBracket(price float64) string {
	if price <= 100 {
		return "100"
	}
	if price <= 15000 {
		return "15000"
	}
	if price <= 50000 {
		return "50000"
	}
	return "50001"
}

func getBSAFee(price float64, tier string, bsaRates map[string]BSABracket) float64 {
	bracket := getBracket(price)
	entry := bsaRates[bracket]
	idx := TierIndex[tier]
	if idx < 0 || idx >= len(entry.Rates) {
		idx = 0
	}
	rate := entry.Rates[idx]
	if entry.Flat {
		return rate
	}
	return price * rate
}

func getActiveFee(price float64, buyerTier string, feeOverride float64, bsaRates map[string]BSABracket) float64 {
	if feeOverride > 0 {
		return price * (feeOverride / 100)
	}
	return getBSAFee(price, buyerTier, bsaRates)
}

func getConsignRate(daysRemaining float64, C FeeConstants) float64 {
	day := int(math.Max(1, math.Min(7, math.Round(daysRemaining))))
	if C.ConsignDecayRates != nil && day-1 < len(C.ConsignDecayRates) && C.ConsignDecayRates[day-1] != 0 {
		return C.ConsignDecayRates[day-1]
	}
	return C.ConsignExtraRate
}

// CalcResult is the full single-transaction fee breakdown.
type CalcResult struct {
	// Echoed inputs
	Price      float64 `json:"price"`
	BuyerTier  string  `json:"buyerTier"`
	TxType     string  `json:"txType"`
	DeliveryMode string `json:"deliveryMode"`
	Payment    string  `json:"payment"`
	SellerCost float64 `json:"sellerCost"`

	// Flags
	IsAuction bool `json:"isAuction"`
	IsConsign bool `json:"isConsign"`

	// Bracket info
	Bracket       string   `json:"bracket"`
	BuyerFeeRate  *float64 `json:"buyerFeeRate"`
	SellerFeeRate *float64 `json:"sellerFeeRate"`

	// Buyer breakdown
	BuyerFeeShare  float64 `json:"buyerFeeShare"`
	ConsignExtra   float64 `json:"consignExtra"`
	ShippingCharge float64 `json:"shippingCharge"`
	BuyerSubtotal  float64 `json:"buyerSubtotal"`
	PPDiscountAmt  float64 `json:"ppDiscountAmt"`
	BuyerTotal     float64 `json:"buyerTotal"`

	// Seller breakdown
	SellerFeeShare float64  `json:"sellerFeeShare"`
	ConsignPayback float64  `json:"consignPayback"`
	SellerReceives float64  `json:"sellerReceives"`
	SellerProfit   float64  `json:"sellerProfit"`
	SellerGM       *float64 `json:"sellerGM"`

	// Platform breakdown
	TotalFeeRevenue float64 `json:"totalFeeRevenue"`
	ShippingCollected float64 `json:"shippingCollected"`
	ShippingCost      float64 `json:"shippingCost"`
	ShippingNet       float64 `json:"shippingNet"`
	PaymentFee        float64 `json:"paymentFee"`
	VATOnFee          float64 `json:"vatOnFee"`
	PlatformNet       float64 `json:"platformNet"`
	PlatformGM        float64 `json:"platformGM"`
}

// Calc computes a single transaction breakdown.
func Calc(
	price float64, buyerTier, sellerTier, txType, deliveryMode, payment string,
	sellerCost, feeOverride float64, C FeeConstants, bsaRates map[string]BSABracket, deferShipping bool,
) *CalcResult {
	isAuction := txType == "auction"
	isConsign := deliveryMode == "consign"

	bracket := getBracket(price)
	buyerFee := getActiveFee(price, buyerTier, feeOverride, bsaRates)
	sellerFee := getBSAFee(price, sellerTier, bsaRates)

	bracketEntry := bsaRates[bracket]
	var buyerFeeRate, sellerFeeRate *float64
	if !bracketEntry.Flat {
		bi := TierIndex[buyerTier]
		si := TierIndex[sellerTier]
		if bi >= 0 && bi < len(bracketEntry.Rates) {
			r := bracketEntry.Rates[bi]
			buyerFeeRate = &r
		}
		if si >= 0 && si < len(bracketEntry.Rates) {
			r := bracketEntry.Rates[si]
			sellerFeeRate = &r
		}
	}

	var buyerFeeShare, sellerFeeShare float64
	if isAuction {
		buyerFeeShare = buyerFee
		sellerFeeShare = sellerFee * C.AuctionSellerFeeRate
	} else {
		buyerFeeShare = buyerFee
		sellerFeeShare = sellerFee
	}

	consignExtra := 0.0
	consignPayback := 0.0
	if isConsign {
		consignExtra = price * C.ConsignExtraRate
		consignPayback = C.ConsignPayback
	}

	shippingCharge := C.ShippingCharge
	shippingCostAmt := C.ShippingCost
	if isConsign && deferShipping {
		shippingCostAmt = C.ShippingCharge
	}

	buyerSubtotal := price + buyerFeeShare + consignExtra + shippingCharge
	var buyerTotal, ppDiscountAmt float64
	if payment == "cc" {
		ppDiscountAmt = 0
		buyerTotal = buyerSubtotal
	} else {
		ppDiscountAmt = buyerSubtotal * C.PPDisc
		buyerTotal = buyerSubtotal - ppDiscountAmt
	}

	sellerReceives := price - sellerFeeShare + consignPayback
	sellerProfit := sellerReceives - sellerCost
	var sellerGM *float64
	if sellerCost > 0 {
		gm := sellerProfit / sellerCost
		sellerGM = &gm
	}

	totalFeeRevenue := buyerFeeShare + sellerFeeShare + consignExtra
	shippingCollected := shippingCharge
	shippingNet := shippingCollected - shippingCostAmt
	var paymentFee float64
	if payment == "cc" {
		paymentFee = buyerSubtotal * C.CCFee
	} else {
		paymentFee = buyerTotal * C.PPFee
	}
	vatOnFee := totalFeeRevenue * C.VAT
	platformNet := totalFeeRevenue + shippingNet - paymentFee - vatOnFee - ppDiscountAmt - consignPayback
	platformGM := 0.0
	if buyerTotal > 0 {
		platformGM = platformNet / buyerTotal
	}

	return &CalcResult{
		Price:        price,
		BuyerTier:    buyerTier,
		TxType:       txType,
		DeliveryMode: deliveryMode,
		Payment:      payment,
		SellerCost:   sellerCost,
		IsAuction:    isAuction,
		IsConsign:    isConsign,
		Bracket:      bracket,
		BuyerFeeRate:  buyerFeeRate,
		SellerFeeRate: sellerFeeRate,
		BuyerFeeShare:  r2(buyerFeeShare),
		ConsignExtra:   r2(consignExtra),
		ShippingCharge: r2(shippingCharge),
		BuyerSubtotal:  r2(buyerSubtotal),
		PPDiscountAmt:  r2(ppDiscountAmt),
		BuyerTotal:     r2(buyerTotal),
		SellerFeeShare: r2(sellerFeeShare),
		ConsignPayback: r2(consignPayback),
		SellerReceives: r2(sellerReceives),
		SellerProfit:   r2(sellerProfit),
		SellerGM:       sellerGM,
		TotalFeeRevenue: r2(totalFeeRevenue),
		ShippingCollected: r2(shippingCollected),
		ShippingCost:      r2(shippingCostAmt),
		ShippingNet:       r2(shippingNet),
		PaymentFee:        r2(paymentFee),
		VATOnFee:          r2(vatOnFee),
		PlatformNet:       r2(platformNet),
		PlatformGM:        r2(platformGM),
	}
}

// ChainHop describes one consignment chain hop.
type ChainHop struct {
	BuyerTier   string  `json:"buyerTier,omitempty"`
	DaysRemaining float64 `json:"daysRemaining"`
	BuyerDays   float64 `json:"buyerDays,omitempty"`
	SellerTier  string  `json:"sellerTier,omitempty"`
	TxType      string  `json:"txType,omitempty"`
	Payment     string  `json:"payment,omitempty"`
	BuyerMode   string  `json:"buyerMode,omitempty"`
}

// ChainNode is a visual/plotting node in the chain result.
type ChainNode struct {
	Label      string  `json:"label"`
	Role       string  `json:"role"`
	ListPrice  float64 `json:"listPrice"`
	BoughtAt   *float64 `json:"boughtAt,omitempty"`
	Fee        float64 `json:"fee"`
	Receives   *float64 `json:"receives,omitempty"`
	ConsignPayback float64 `json:"consignPayback,omitempty"`
	Profit     *float64 `json:"profit,omitempty"`
	ProfitLabel string `json:"profitLabel"`
	Color       string `json:"color"`
	GotCoinRefund bool `json:"gotCoinRefund,omitempty"`
}

// ChainTransaction is a single hop in the consignment chain.
type ChainTransaction struct {
	Step          int         `json:"step"`
	Seller        string      `json:"seller"`
	Buyer         string      `json:"buyer"`
	Price         float64     `json:"price"`
	Tx            *CalcResult `json:"tx"`
	SellerBuyIn   *float64    `json:"sellerBuyIn,omitempty"`
	IsDelivery    bool        `json:"isDelivery"`
	CoinRefund    float64     `json:"coinRefund"`
	AdjustedPlatNet float64   `json:"adjustedPlatNet"`
	DaysRemaining float64     `json:"daysRemaining"`
	ConsignRate   float64     `json:"consignRate"`
}

// ChainResult is the full consignment chain breakdown.
type ChainResult struct {
	Nodes         []ChainNode      `json:"nodes"`
	Transactions  []ChainTransaction `json:"transactions"`
}

// CalcChain models Store A → Reseller(s) → Final Buyer.
func CalcChain(
	p0 float64, buyerTier, sellerTier, txType, payment string,
	chainHops []ChainHop, markup, feeOverride float64, C FeeConstants, bsaRates map[string]BSABracket, subsequentPayback bool,
) *ChainResult {
	chainLen := len(chainHops)
	nodes := []ChainNode{}
	transactions := []ChainTransaction{}
	nodeColors := []string{"#8E44AD", "#2980B9", "#16A085", "#C0392B", "#1ABC9C", "#7F8C8D"}

	makeTx := func(price float64, hopBuyerTier string, buyIn float64, isDelivery bool, hop ChainHop, isFirstHop bool) (tx *CalcResult, coinRefund, adjustedPlatNet, consignRate float64) {
		hopConsignRate := getConsignRate(hop.DaysRemaining, C)
		subPaybackRate := 0.0
		if C.ConsignExtraRate > 0 {
			subPaybackRate = (C.ConsignPayback / C.ConsignExtraRate) * hopConsignRate
		}
		isBuyout := isDelivery && hop.BuyerMode != "consignExpire"
		hopPayback := 0.0
		if !isBuyout {
			if isFirstHop {
				hopPayback = C.ConsignPayback
			} else if subsequentPayback {
				hopPayback = subPaybackRate
			}
		}
		buyerExtraRate := 0.0
		if !isBuyout && isDelivery {
			buyerExtraRate = getConsignRate(hop.BuyerDays, C)
		}
		hopC := C
		hopC.ConsignExtraRate = hopConsignRate + buyerExtraRate
		hopC.ConsignPayback = hopPayback

		hopDeliveryMode := "consign"
		if isBuyout {
			hopDeliveryMode = "deliver"
		}
		hopSellerTier := hop.SellerTier
		if hopSellerTier == "" {
			hopSellerTier = sellerTier
		}
		hopTxType := hop.TxType
		if hopTxType == "" {
			hopTxType = txType
		}
		hopPayment := hop.Payment
		if hopPayment == "" {
			hopPayment = payment
		}
		tx = Calc(price, hopBuyerTier, hopSellerTier, hopTxType, hopDeliveryMode, hopPayment, buyIn, feeOverride, hopC, bsaRates, !isDelivery)
		coinRefund = 0.0
		if !isDelivery {
			coinRefund = C.ShippingCharge
		}
		effectiveConsignRate := 0.0
		if !isBuyout {
			effectiveConsignRate = hopConsignRate + buyerExtraRate
		}
		return tx, coinRefund, tx.PlatformNet, effectiveConsignRate
	}

	buyerTier1 := chainHops[0].BuyerTier
	if buyerTier1 == "" {
		if chainLen == 1 {
			buyerTier1 = buyerTier
		} else {
			buyerTier1 = "platinum"
		}
	}
	tx0, cr0, apn0, rate0 := makeTx(p0, buyerTier1, 0, chainLen == 1, chainHops[0], true)
	firstBuyer := "Final Buyer"
	if chainLen > 1 {
		firstBuyer = "Reseller 1"
	}
	transactions = append(transactions, ChainTransaction{
		Step: 1, Seller: "Store A", Buyer: firstBuyer,
		Price: p0, Tx: tx0, SellerBuyIn: nil,
		IsDelivery: chainLen == 1, CoinRefund: cr0, AdjustedPlatNet: apn0,
		DaysRemaining: chainHops[0].DaysRemaining, ConsignRate: rate0,
	})
	nodes = append(nodes, ChainNode{
		Label: "Store A", Role: "Lister",
		ListPrice: p0, BoughtAt: nil,
		Fee: tx0.SellerFeeShare, Receives: &tx0.SellerReceives,
		ConsignPayback: tx0.ConsignPayback,
		Profit: nil, ProfitLabel: "Net Received", Color: "#F39C12",
	})

	if chainLen == 1 {
		nodes = append(nodes, ChainNode{
			Label: "Final Buyer", Role: "Final Buyer",
			ListPrice: p0, BoughtAt: &tx0.BuyerTotal,
			Fee: tx0.BuyerFeeShare + tx0.ConsignExtra,
			Receives: nil, Profit: nil, ProfitLabel: "Total Paid", Color: "#27AE60",
		})
		return &ChainResult{Nodes: nodes, Transactions: transactions}
	}

	prevPrice := p0
	prevEffectiveCost := tx0.BuyerTotal - cr0

	for i := 0; i < chainLen-1; i++ {
		buyIn := prevEffectiveCost
		sellPrice := math.Round(prevPrice * (1 + markup))
		isLast := (i == chainLen-2)
		hop := chainHops[i+1]
		sellerLbl := fmt.Sprintf("Reseller %d", i+1)
		nextLbl := fmt.Sprintf("Reseller %d", i+2)
		if isLast {
			nextLbl = "Final Buyer"
		}
		hopBuyerTier := hop.BuyerTier
		if hopBuyerTier == "" {
			if isLast {
				hopBuyerTier = buyerTier
			} else {
				hopBuyerTier = "platinum"
			}
		}
		txR, crR, apnR, rateR := makeTx(sellPrice, hopBuyerTier, buyIn, isLast, hop, false)
		transactions = append(transactions, ChainTransaction{
			Step: i + 2, Seller: sellerLbl, Buyer: nextLbl,
			Price: sellPrice, Tx: txR, SellerBuyIn: &buyIn,
			IsDelivery: isLast, CoinRefund: crR, AdjustedPlatNet: apnR,
			DaysRemaining: hop.DaysRemaining, ConsignRate: rateR,
		})
		nodes = append(nodes, ChainNode{
			Label: sellerLbl, Role: fmt.Sprintf("Flip #%d", i+1),
			ListPrice: sellPrice, BoughtAt: &buyIn,
			Fee: txR.SellerFeeShare, Receives: &txR.SellerReceives,
			ConsignPayback: txR.ConsignPayback,
			Profit: &txR.SellerProfit, ProfitLabel: "Profit",
			Color: nodeColors[i%len(nodeColors)],
			GotCoinRefund: true,
		})
		prevPrice = sellPrice
		prevEffectiveCost = txR.BuyerTotal - crR
	}

	lastTx := transactions[len(transactions)-1].Tx
	nodes = append(nodes, ChainNode{
		Label: "Final Buyer", Role: "Final Buyer",
		ListPrice: transactions[len(transactions)-1].Price,
		BoughtAt: &lastTx.BuyerTotal,
		Fee: lastTx.BuyerFeeShare + lastTx.ConsignExtra,
		Receives: nil, Profit: nil, ProfitLabel: "Total Paid", Color: "#27AE60",
	})

	return &ChainResult{Nodes: nodes, Transactions: transactions}
}

// PPBreakevenDiscount returns the PromptPay discount rate at which platform
// earns the same net on credit card vs PromptPay.
func PPBreakevenDiscount(C FeeConstants) float64 {
	return (C.CCFee - C.PPFee) / (1 - C.PPFee)
}
