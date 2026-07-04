CREATE TABLE IF NOT EXISTS scan_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    image_hash TEXT NOT NULL UNIQUE,
    phash TEXT,
    cache_version TEXT NOT NULL,
    status TEXT NOT NULL,
    card JSONB,
    raw_response JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_scan_results_image_hash ON scan_results(image_hash);
CREATE INDEX IF NOT EXISTS idx_scan_results_phash ON scan_results(phash) WHERE phash IS NOT NULL;

CREATE TABLE IF NOT EXISTS pricing_queries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    query_hash TEXT NOT NULL UNIQUE,
    card_code TEXT,
    card_name TEXT,
    rarity TEXT,
    source TEXT NOT NULL,
    results JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pricing_queries_query_hash ON pricing_queries(query_hash);
