CREATE TABLE IF NOT EXISTS transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    seller_id TEXT NOT NULL,
    buyer_id TEXT,
    item_id TEXT,
    item_type TEXT,
    price NUMERIC(18, 4) NOT NULL,
    currency TEXT NOT NULL DEFAULT 'USD',
    buyer_tier TEXT,
    seller_tier TEXT,
    tx_type TEXT NOT NULL,
    delivery_mode TEXT,
    payment_method TEXT,
    seller_cost NUMERIC(18, 4),
    fee_override NUMERIC(18, 4),
    fees JSONB NOT NULL,
    metadata JSONB,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transactions_seller ON transactions(seller_id);
CREATE INDEX IF NOT EXISTS idx_transactions_buyer ON transactions(buyer_id);

CREATE TABLE IF NOT EXISTS auctions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id TEXT NOT NULL UNIQUE,
    seller_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    starting_price NUMERIC(18, 4) NOT NULL,
    current_price NUMERIC(18, 4) NOT NULL,
    reserve_price NUMERIC(18, 4),
    currency TEXT NOT NULL DEFAULT 'USD',
    status TEXT NOT NULL DEFAULT 'active',
    ends_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_auctions_seller ON auctions(seller_id);
CREATE INDEX IF NOT EXISTS idx_auctions_status ON auctions(status);

CREATE TABLE IF NOT EXISTS auction_bids (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    auction_id UUID NOT NULL REFERENCES auctions(id) ON DELETE CASCADE,
    bidder_id TEXT NOT NULL,
    amount NUMERIC(18, 4) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_auction_bids_auction ON auction_bids(auction_id);
