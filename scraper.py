#!/usr/bin/env python3
"""
TCG Card Scraper - One Piece TCG (Batch Optimized v2)
Fetches card data from optcgapi.com and stores in PostgreSQL using batch inserts.
"""
import os
import re
import time
import json
import requests
from datetime import datetime
from decimal import Decimal, InvalidOperation
from collections import defaultdict

import psycopg2
from psycopg2.extras import execute_values

# ── Config ───────────────────────────────────────────────────────
DB_URL = os.environ.get(
    "DATABASE_URL",
    "postgresql://neondb_owner:npg_0P9uMKUYgTSZ@ep-dry-dew-amvkqf9w-pooler.c-5.us-east-1.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
)
BASE_URL = "https://optcgapi.com/api"
GAME_ID = 1

# ── DB helpers ─────────────────────────────────────────────────
def get_conn():
    return psycopg2.connect(DB_URL)

def run_schema(conn):
    with open("schema.sql", "r", encoding="utf-8") as f:
        sql = f.read()
    with conn.cursor() as cur:
        cur.execute(sql)
    conn.commit()

def to_decimal(val):
    if val is None or val == "" or val == "null":
        return None
    try:
        return Decimal(str(val))
    except (InvalidOperation, ValueError):
        return None

def safe_str(val, max_len=500):
    if val is None:
        return None
    s = str(val)
    return s[:max_len] if len(s) > max_len else s

def parse_date(val):
    if val is None or val == "":
        return None
    try:
        return datetime.strptime(str(val), "%Y-%m-%d").date()
    except Exception:
        return None

# ── Fetch helpers ──────────────────────────────────────────────
def fetch_json(url, retries=3, delay=1):
    for attempt in range(retries):
        try:
            resp = requests.get(url, timeout=30)
            resp.raise_for_status()
            data = resp.json()
            if isinstance(data, dict) and data.get("error"):
                print(f"  API error: {data['error']}")
                return None
            return data
        except requests.RequestException as e:
            print(f"  Attempt {attempt+1}/{retries} failed: {e}")
            if attempt < retries - 1:
                time.sleep(delay)
    return None

# ── Normalize set_code ────────────────────────────────────────
def normalize_set_code(raw_code):
    """
    Normalize set codes like OP01 -> OP-01, ST01 -> ST-01, ST-29 -> ST-29,
    EB01 -> EB-01, PRB01 -> PRB-01, etc.
    """
    if not raw_code:
        return "UNKNOWN"
    raw = str(raw_code).strip()
    # Already has dash, keep as is
    if "-" in raw:
        return raw
    # Match patterns: OP01, ST01, EB01, PRB01, OP1, etc.
    m = re.match(r'^(OP|ST|EB|PRB)(\d+)$', raw, re.IGNORECASE)
    if m:
        prefix = m.group(1).upper()
        num = m.group(2)
        return f"{prefix}-{num.zfill(2)}"
    # Single letters or other formats
    return raw.upper()

# ── Batch insert helpers ───────────────────────────────────────
def insert_sets(conn, sets_data):
    """sets_data: [(game_id, set_code, set_name, set_type, card_count)]"""
    with conn.cursor() as cur:
        execute_values(cur, """
            INSERT INTO sets (game_id, set_code, set_name, set_type, card_count)
            VALUES %s
            ON CONFLICT (game_id, set_code) DO UPDATE
            SET set_name = EXCLUDED.set_name,
                set_type = EXCLUDED.set_type,
                card_count = EXCLUDED.card_count,
                updated_at = NOW()
        """, sets_data)
    conn.commit()

def get_set_id_map(conn):
    with conn.cursor() as cur:
        cur.execute("SELECT id, set_code FROM sets WHERE game_id = %s", (GAME_ID,))
        return {row[1]: row[0] for row in cur.fetchall()}

def insert_cards(conn, cards_data):
    """cards_data: list of tuples."""
    with conn.cursor() as cur:
        execute_values(cur, """
            INSERT INTO cards (game_id, set_id, card_set_id, card_name, card_text,
                card_type, card_color, rarity, card_cost, card_power, life,
                counter_amount, attribute, sub_types, card_image, card_image_id,
                date_scraped, source)
            VALUES %s
            ON CONFLICT (game_id, card_set_id, set_id) DO UPDATE
            SET card_name = EXCLUDED.card_name,
                card_text = EXCLUDED.card_text,
                card_type = EXCLUDED.card_type,
                card_color = EXCLUDED.card_color,
                rarity = EXCLUDED.rarity,
                card_cost = EXCLUDED.card_cost,
                card_power = EXCLUDED.card_power,
                life = EXCLUDED.life,
                counter_amount = EXCLUDED.counter_amount,
                attribute = EXCLUDED.attribute,
                sub_types = EXCLUDED.sub_types,
                card_image = EXCLUDED.card_image,
                card_image_id = EXCLUDED.card_image_id,
                date_scraped = EXCLUDED.date_scraped,
                updated_at = NOW()
        """, cards_data)
    conn.commit()

def get_card_id_map(conn):
    """Return dict mapping (card_set_id, set_id) -> card_id."""
    with conn.cursor() as cur:
        cur.execute("SELECT id, card_set_id, set_id FROM cards WHERE game_id = %s", (GAME_ID,))
        return {(row[1], row[2]): row[0] for row in cur.fetchall()}

def insert_variants(conn, variants_data):
    """variants_data: list of tuples (card_id, variant_code, variant_name, card_image,
        card_image_id, market_price, inventory_price, date_scraped)"""
    with conn.cursor() as cur:
        execute_values(cur, """
            INSERT INTO card_variants (card_id, variant_code, variant_name, card_image,
                card_image_id, market_price, inventory_price, date_scraped)
            VALUES %s
            ON CONFLICT (card_id, variant_code) DO UPDATE
            SET variant_name = EXCLUDED.variant_name,
                card_image = EXCLUDED.card_image,
                card_image_id = EXCLUDED.card_image_id,
                market_price = EXCLUDED.market_price,
                inventory_price = EXCLUDED.inventory_price,
                date_scraped = EXCLUDED.date_scraped,
                updated_at = NOW()
        """, variants_data)

# ── Main scraper ───────────────────────────────────────────────
def main():
    print("TCG Card Scraper - One Piece TCG (Batch Optimized v2)")
    print(f"DB: {DB_URL[:60]}...")
    print(f"API: {BASE_URL}")
    print()

    conn = get_conn()
    print("Connected to database.")

    print("Running schema setup...")
    run_schema(conn)
    print("Schema ready.\n")

    # Step 1: Fetch all raw data
    print("=== Fetching all raw data ===")
    sets_meta = fetch_json(f"{BASE_URL}/allSets/") or []
    decks_meta = fetch_json(f"{BASE_URL}/allDecks/") or []
    all_set_cards = fetch_json(f"{BASE_URL}/allSetCards/") or []
    all_st_cards = fetch_json(f"{BASE_URL}/allSTCards/") or []
    all_promos = fetch_json(f"{BASE_URL}/allPromos/") or []
    all_don = fetch_json(f"{BASE_URL}/allDonCards/") or []

    print(f"allSets: {len(sets_meta)}")
    print(f"allDecks: {len(decks_meta)}")
    print(f"allSetCards: {len(all_set_cards)}")
    print(f"allSTCards: {len(all_st_cards)}")
    print(f"allPromos: {len(all_promos)}")
    print(f"allDonCards: {len(all_don)}")

    # Step 2: Collect all unique set_codes from raw data (including card data)
    all_raw_cards = all_set_cards + all_st_cards + all_promos + all_don
    set_codes_from_cards = set()
    for item in all_raw_cards:
        raw_code = item.get("set_id")
        if raw_code:
            set_codes_from_cards.add(normalize_set_code(raw_code))

    # Merge with metadata
    sets_to_insert = {}
    for s in sets_meta:
        code = normalize_set_code(s.get("set_id"))
        sets_to_insert[code] = (GAME_ID, code, s.get("set_name", code), "booster", 0)
    for d in decks_meta:
        code = normalize_set_code(d.get("structure_deck_id"))
        sets_to_insert[code] = (GAME_ID, code, d.get("structure_deck_name", code), "starter_deck", 0)
    for code in set_codes_from_cards:
        if code not in sets_to_insert:
            sets_to_insert[code] = (GAME_ID, code, code, "unknown", 0)
    # Promo and DON sets
    sets_to_insert["P"] = (GAME_ID, "P", "One Piece Promotion Cards", "promo", 0)
    sets_to_insert["DON"] = (GAME_ID, "DON", "DON!! Cards", "don", 0)

    insert_sets(conn, list(sets_to_insert.values()))
    print(f"Inserted {len(sets_to_insert)} sets.")

    set_id_map = get_set_id_map(conn)
    print(f"Set ID map: {len(set_id_map)} entries.")

    # Step 3: Process and deduplicate cards
    print("\n=== Processing cards ===")
    card_dict = {}  # key: (card_set_id, set_id) -> card tuple
    variant_list = []  # list of variant tuples

    for item in all_raw_cards:
        raw_set_code = item.get("set_id")
        set_code = normalize_set_code(raw_set_code) if raw_set_code else "P" if item in all_promos else "DON"
        set_id = set_id_map.get(set_code)
        if set_id is None:
            print(f"  Warning: set_code '{set_code}' (raw: '{raw_set_code}') not found in set_id_map")
            continue

        card_set_id = safe_str(item.get("card_set_id") or item.get("optcg_don_name") or item.get("card_image_id"), 100)
        card_name = safe_str(item.get("card_name"), 500)
        card_text = item.get("card_text")
        card_type = safe_str(item.get("card_type"), 50)
        card_color = safe_str(item.get("card_color"), 50)
        rarity = safe_str(item.get("rarity"), 20)
        card_cost = safe_str(item.get("card_cost"), 20)
        card_power = safe_str(item.get("card_power"), 20)
        life = safe_str(item.get("life"), 20)
        counter_amount = safe_str(item.get("counter_amount"), 20)
        attribute = safe_str(item.get("attribute"), 100)
        sub_types = safe_str(item.get("sub_types"), 300)
        card_image = item.get("card_image")
        card_image_id = safe_str(item.get("card_image_id"), 100)
        date_scraped = parse_date(item.get("date_scraped"))
        source = "optcgapi"

        key = (card_set_id, set_id)
        card_tuple = (
            GAME_ID, set_id, card_set_id, card_name, card_text,
            card_type, card_color, rarity, card_cost, card_power, life,
            counter_amount, attribute, sub_types, card_image, card_image_id,
            date_scraped, source
        )
        card_dict[key] = card_tuple

        # Variant
        variant_code = card_image_id or card_set_id
        variant_name = card_name
        market_price = to_decimal(item.get("market_price"))
        inventory_price = to_decimal(item.get("inventory_price"))
        variant_list.append((
            None, variant_code, variant_name, card_image, card_image_id,
            market_price, inventory_price, date_scraped, key
        ))

    all_cards = list(card_dict.values())
    print(f"Total unique cards to insert: {len(all_cards)}")
    print(f"Total variants to insert: {len(variant_list)}")

    # Step 4: Insert cards
    print("\n=== Inserting cards ===")
    insert_cards(conn, all_cards)
    print(f"Inserted/updated {len(all_cards)} cards.")

    # Build card_id map
    card_id_map = get_card_id_map(conn)
    print(f"Card ID map: {len(card_id_map)} entries.")

    # Step 5: Build final variants with card_id (deduplicate)
    print("\n=== Inserting variants ===")
    final_variants_dict = {}
    for v in variant_list:
        _, variant_code, variant_name, card_image, card_image_id, market_price, inventory_price, date_scraped, key = v
        card_id = card_id_map.get(key)
        if card_id:
            v_key = (card_id, variant_code)
            final_variants_dict[v_key] = (
                card_id, variant_code, variant_name, card_image, card_image_id,
                market_price, inventory_price, date_scraped
            )
    final_variants = list(final_variants_dict.values())

    insert_variants(conn, final_variants)
    print(f"Inserted/updated {len(final_variants)} variants.")
    conn.commit()

    # Step 6: Update card counts
    print("\n=== Updating set card counts ===")
    set_counts = defaultdict(int)
    for item in all_raw_cards:
        raw_code = item.get("set_id")
        if raw_code:
            set_counts[normalize_set_code(raw_code)] += 1

    with conn.cursor() as cur:
        for set_code, count in set_counts.items():
            set_id = set_id_map.get(set_code)
            if set_id:
                cur.execute("UPDATE sets SET card_count = %s WHERE id = %s", (count, set_id))
    conn.commit()

    conn.close()
    print(f"\n=== DONE ===")
    print(f"Total cards: {len(all_cards)}")
    print(f"Total variants: {len(final_variants)}")
    print("Database connection closed.")

if __name__ == "__main__":
    main()
