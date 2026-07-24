-- ============================================================
-- TCG Card Database Schema - PostgreSQL
-- Supports: One Piece TCG, Pokemon TCG, etc.
-- ============================================================

-- Drop tables if exist (for clean setup)
DROP TABLE IF EXISTS card_prices CASCADE;
DROP TABLE IF EXISTS card_variants CASCADE;
DROP TABLE IF EXISTS card_translations CASCADE;
DROP TABLE IF EXISTS cards CASCADE;
DROP TABLE IF EXISTS sets CASCADE;
DROP TABLE IF EXISTS games CASCADE;

-- Enable pg_trgm extension for GIN trigram indexes (must be before any gin_trgm_ops usage)
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ============================================================
-- 1. GAMES
-- ============================================================
CREATE TABLE games (
    id          SERIAL PRIMARY KEY,
    slug        VARCHAR(50) UNIQUE NOT NULL,
    name        VARCHAR(100) NOT NULL,
    description TEXT,
    created_at  TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_games_slug ON games(slug);

-- ============================================================
-- 2. SETS
-- ============================================================
CREATE TABLE sets (
    id           SERIAL PRIMARY KEY,
    game_id      INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    set_code     VARCHAR(50) NOT NULL,        -- e.g. OP-01, ST-01, P
    set_name     VARCHAR(300),
    set_type     VARCHAR(50),                  -- booster, starter_deck, promo, extra_booster, don, event_pack
    release_date DATE,
    card_count   INTEGER DEFAULT 0,
    created_at   TIMESTAMP DEFAULT NOW(),
    updated_at   TIMESTAMP DEFAULT NOW(),
    UNIQUE(game_id, set_code)
);

CREATE INDEX idx_sets_game_id   ON sets(game_id);
CREATE INDEX idx_sets_set_code  ON sets(set_code);
CREATE INDEX idx_sets_set_type  ON sets(set_type);

-- ============================================================
-- 3. CARDS
-- ============================================================
CREATE TABLE cards (
    id            SERIAL PRIMARY KEY,
    game_id       INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    set_id        INTEGER REFERENCES sets(id) ON DELETE SET NULL,
    card_set_id   VARCHAR(200) NOT NULL,        -- e.g. OP01-001, P-001
    card_name     VARCHAR(500),
    card_text     TEXT,
    card_type     VARCHAR(50),                  -- Leader, Character, Event, Stage, DON!!, etc.
    card_color    VARCHAR(50),
    rarity        VARCHAR(20),
    card_cost     VARCHAR(20),
    card_power    VARCHAR(20),
    life          VARCHAR(20),
    counter_amount VARCHAR(20),
    attribute     VARCHAR(100),
    sub_types     VARCHAR(300),
    card_image    TEXT,
    card_image_id VARCHAR(100),
    date_scraped  DATE,
    source        VARCHAR(50) DEFAULT 'optcgapi',
    created_at    TIMESTAMP DEFAULT NOW(),
    updated_at    TIMESTAMP DEFAULT NOW(),
    UNIQUE(game_id, card_set_id, set_id)
);

CREATE INDEX idx_cards_game_id      ON cards(game_id);
CREATE INDEX idx_cards_set_id       ON cards(set_id);
CREATE INDEX idx_cards_card_set_id  ON cards(card_set_id);
CREATE INDEX idx_cards_card_type    ON cards(card_type);
CREATE INDEX idx_cards_card_color   ON cards(card_color);
CREATE INDEX idx_cards_rarity       ON cards(rarity);
CREATE INDEX idx_cards_card_name    ON cards USING gin(card_name gin_trgm_ops);
CREATE INDEX idx_cards_sub_types    ON cards USING gin(sub_types gin_trgm_ops);

-- ============================================================
-- 4. CARD VARIANTS (Parallel, Alternate Art, etc.)
-- ============================================================
CREATE TABLE card_variants (
    id              SERIAL PRIMARY KEY,
    card_id         INTEGER NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    variant_code    VARCHAR(200),               -- e.g. OP01-001_p1
    variant_name    VARCHAR(500),              -- e.g. "Roronoa Zoro (Parallel)"
    card_image      TEXT,
    card_image_id   VARCHAR(100),
    market_price    DECIMAL(12,2),
    inventory_price DECIMAL(12,2),
    date_scraped    DATE,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    UNIQUE(card_id, variant_code)
);

CREATE INDEX idx_variants_card_id ON card_variants(card_id);

-- ============================================================
-- 5. CARD PRICES (Price History)
-- ============================================================
CREATE TABLE card_prices (
    id               SERIAL PRIMARY KEY,
    card_variant_id  INTEGER NOT NULL REFERENCES card_variants(id) ON DELETE CASCADE,
    market_price     DECIMAL(12,2),
    inventory_price  DECIMAL(12,2),
    date_scraped     DATE,
    created_at       TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_prices_variant_id ON card_prices(card_variant_id);
CREATE INDEX idx_prices_date ON card_prices(date_scraped);

-- ============================================================
-- 6. CARD TRANSLATIONS (Multi-language support)
-- ============================================================
CREATE TABLE card_translations (
    id         SERIAL PRIMARY KEY,
    card_id    INTEGER NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    language   VARCHAR(10) NOT NULL,            -- en, jp, th, zh, ko, etc.
    card_name  VARCHAR(500),
    card_text  TEXT,
    card_image TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(card_id, language)
);

CREATE INDEX idx_translations_card_id ON card_translations(card_id);
CREATE INDEX idx_translations_language ON card_translations(language);

-- ============================================================
-- Insert initial game data
-- ============================================================
INSERT INTO games (slug, name, description) VALUES
    ('onepiece', 'One Piece Card Game', 'One Piece Trading Card Game by Bandai'),
    ('pokemon', 'Pokemon TCG', 'Pokemon Trading Card Game'),
    ('lorcana', 'Disney Lorcana TCG', 'Disney Lorcana Trading Card Game'),
    ('yugioh', 'Yu-Gi-Oh! TCG', 'Yu-Gi-Oh! Trading Card Game'),
    ('digimon', 'Digimon Card Game', 'Digimon Trading Card Game');
