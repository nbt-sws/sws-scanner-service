#!/usr/bin/env python3
"""
TCG Card Scraper - One Piece TCG (Complete v4 with Image Fallback)
Fetches ALL card data including detailed promo variants and tries to find
alternative images for cards missing images from other endpoints.
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

# â”€â”€ Config â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
from db_config import get_db_url

DB_URL = get_db_url()
BASE_URL = "https://optcgapi.com/api"
GAME_ID = 1

# â”€â”€ DB helpers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
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

# â”€â”€ Fetch helpers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
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

# â”€â”€ Normalize set_code â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
def normalize_set_code(raw_code):
    if not raw_code:
        return "UNKNOWN"
    raw = str(raw_code).strip()
    if "-" in raw:
        return raw
    m = re.match(r'^(OP|ST|EB|PRB)(\d+)$', raw, re.IGNORECASE)
    if m:
        prefix = m.group(1).upper()
        num = m.group(2)
        return f"{prefix}-{num.zfill(2)}"
    return raw.upper()

# â”€â”€ Batch insert helpers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
def insert_sets(conn, sets_data):
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
    with conn.cursor() as cur:
        cur.execute("SELECT id, card_set_id, set_id FROM cards WHERE game_id = %s", (GAME_ID,))
        return {(row[1], row[2]): row[0] for row in cur.fetchall()}

def insert_variants(conn, variants_data):
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
    conn.commit()

# â”€â”€ Build image lookup from all endpoints â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
def build_image_lookup(all_set_cards, all_st_cards, all_promos):
    """Build a lookup of card_image_id -> card_image from all endpoints."""
    lookup = {}
    for item in all_set_cards + all_st_cards + all_promos:
        image_id = item.get("card_image_id")
        image_url = item.get("card_image")
        if image_id and image_url:
            lookup[image_id] = image_url
    return lookup

# â”€â”€ Process a single card item into card tuple + variant tuple â”€â”€
def process_card_item(item, set_id, image_lookup=None, source="optcgapi"):
    """Returns (card_tuple, list_of_variant_tuples)"""
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
    card_image_id = safe_str(item.get("card_image_id"), 100)
    
    # Try to find image from lookup if missing
    card_image = item.get("card_image")
    if not card_image and image_lookup and card_image_id:
        card_image = image_lookup.get(card_image_id)
    
    date_scraped = parse_date(item.get("date_scraped"))

    card_tuple = (
        GAME_ID, set_id, card_set_id, card_name, card_text,
        card_type, card_color, rarity, card_cost, card_power, life,
        counter_amount, attribute, sub_types, card_image, card_image_id,
        date_scraped, source
    )

    variant_code = card_image_id or card_set_id
    variant_name = card_name
    market_price = to_decimal(item.get("market_price"))
    inventory_price = to_decimal(item.get("inventory_price"))
    variant_tuple = (
        None, variant_code, variant_name, card_image, card_image_id,
        market_price, inventory_price, date_scraped, (card_set_id, set_id)
    )

    return card_tuple, variant_tuple

# â”€â”€ Fetch detailed promo variants â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
def fetch_promo_variants(p_id, delay=0.3):
    url = f"{BASE_URL}/promos/card/{p_id}/"
    data = fetch_json(url)
    if delay:
        time.sleep(delay)
    return data or []

# â”€â”€ Main scraper â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
def main():
    print("TCG Card Scraper - One Piece TCG (Complete v4 with Image Fallback)")
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

    # Build image lookup from all endpoints for fallback
    print("\n=== Building image lookup ===")
    image_lookup = build_image_lookup(all_set_cards, all_st_cards, all_promos)
    print(f"Image lookup: {len(image_lookup)} entries")

    # Step 2: Collect all unique set_codes
    all_raw_cards = all_set_cards + all_st_cards + all_promos + all_don
    set_codes_from_cards = set()
    for item in all_raw_cards:
        raw_code = item.get("set_id")
        if raw_code:
            set_codes_from_cards.add(normalize_set_code(raw_code))

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
    sets_to_insert["P"] = (GAME_ID, "P", "One Piece Promotion Cards", "promo", 0)
    sets_to_insert["DON"] = (GAME_ID, "DON", "DON!! Cards", "don", 0)

    insert_sets(conn, list(sets_to_insert.values()))
    print(f"Inserted {len(sets_to_insert)} sets.")

    set_id_map = get_set_id_map(conn)
    print(f"Set ID map: {len(set_id_map)} entries.")

    # Step 3: Process regular cards
    print("\n=== Processing regular cards ===")
    regular_cards = all_set_cards + all_st_cards + all_don
    card_dict = {}
    variant_list = []

    for item in regular_cards:
        raw_set_code = item.get("set_id")
        set_code = normalize_set_code(raw_set_code) if raw_set_code else "DON"
        set_id = set_id_map.get(set_code)
        if set_id is None:
            print(f"  Warning: set_code '{set_code}' not found")
            continue

        card_tuple, variant_tuple = process_card_item(item, set_id, image_lookup)
        key = (card_tuple[2], set_id)
        card_dict[key] = card_tuple
        variant_list.append(variant_tuple)

    print(f"Regular cards: {len(card_dict)} unique, {len(variant_list)} variants")

    # Step 4: Process promo cards with detailed variants
    print("\n=== Processing promo cards with detailed variants ===")
    p_ids = sorted(set(
        item.get("card_set_id") for item in all_promos
        if item.get("card_set_id", "").startswith("P-")
    ))
    print(f"Unique P- IDs found: {len(p_ids)}")

    promo_card_dict = {}
    promo_variant_list = []
    missing_images_count = 0
    fixed_images_count = 0

    for idx, p_id in enumerate(p_ids, 1):
        variants = fetch_promo_variants(p_id, delay=0.3)
        if not variants:
            print(f"  [{idx}/{len(p_ids)}] {p_id}: NO variants found")
            continue

        base_variant = variants[0]
        set_id = set_id_map.get("P")

        card_tuple, _ = process_card_item(base_variant, set_id, image_lookup)
        key = (card_tuple[2], set_id)
        promo_card_dict[key] = card_tuple

        for v in variants:
            card_set_id = safe_str(v.get("card_set_id") or v.get("card_image_id"), 100)
            variant_code = safe_str(v.get("card_image_id"), 100) or card_set_id
            variant_name = safe_str(v.get("card_name"), 500)
            card_image_id = safe_str(v.get("card_image_id"), 100)
            
            # Try to find image from lookup if missing
            card_image = v.get("card_image")
            if not card_image and image_lookup:
                card_image = image_lookup.get(card_image_id)
                if card_image:
                    fixed_images_count += 1
            
            market_price = to_decimal(v.get("market_price"))
            inventory_price = to_decimal(v.get("inventory_price"))
            date_scraped = parse_date(v.get("date_scraped"))

            if not card_image:
                missing_images_count += 1

            promo_variant_list.append((
                None, variant_code, variant_name, card_image, card_image_id,
                market_price, inventory_price, date_scraped, key
            ))

        if idx % 10 == 0 or idx == len(p_ids):
            print(f"  [{idx}/{len(p_ids)}] {p_id}: {len(variants)} variants (missing: {missing_images_count}, fixed: {fixed_images_count})")

    print(f"\nPromo cards: {len(promo_card_dict)} unique")
    print(f"Promo variants: {len(promo_variant_list)}")
    print(f"Missing images: {missing_images_count}")
    print(f"Fixed images: {fixed_images_count}")

    # Merge
    all_card_dict = {**card_dict, **promo_card_dict}
    all_variant_list = variant_list + promo_variant_list

    all_cards = list(all_card_dict.values())
    print(f"\nTotal unique cards: {len(all_cards)}")
    print(f"Total variants: {len(all_variant_list)}")

    # Step 5: Insert cards
    print("\n=== Inserting cards ===")
    insert_cards(conn, all_cards)
    print(f"Inserted/updated {len(all_cards)} cards.")

    card_id_map = get_card_id_map(conn)
    print(f"Card ID map: {len(card_id_map)} entries.")

    # Step 6: Insert variants
    print("\n=== Inserting variants ===")
    final_variants_dict = {}
    for v in all_variant_list:
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

    # Step 7: Update card counts
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

    # Summary
    print("\n" + "="*50)
    print("FINAL SUMMARY")
    print("="*50)
    print(f"Total sets: {len(sets_to_insert)}")
    print(f"Total unique cards: {len(all_cards)}")
    print(f"Total variants: {len(final_variants)}")
    print(f"Promo P- IDs: {len(p_ids)}")
    print(f"Missing images: {missing_images_count}")
    print(f"Fixed images: {fixed_images_count}")

    conn.close()
    print("\nDatabase connection closed.")

if __name__ == "__main__":
    main()
