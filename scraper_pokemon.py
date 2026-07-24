#!/usr/bin/env python3
"""
TCG Card Scraper - Pokemon TCG (Focused Sets)
Fetches recent and popular sets to stay within time limits.
"""
import os
import time
import requests
from datetime import datetime
from decimal import Decimal, InvalidOperation
from collections import defaultdict

import psycopg2
from psycopg2.extras import execute_values

# â”€â”€ Config â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
from db_config import get_db_url

DB_URL = get_db_url()
BASE_URL = "https://api.pokemontcg.io/v2"
GAME_ID = 2
PAGE_SIZE = 250

# Focus on these series (most recent + some classics)
TARGET_SERIES = [
    "Scarlet & Violet",
]
CLASSIC_SETS = ["base1", "base2", "base3"]  # Base, Jungle, Fossil

# â”€â”€ DB helpers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
def get_conn():
    return psycopg2.connect(DB_URL)

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
        return datetime.strptime(str(val), "%Y/%m/%d").date()
    except Exception:
        try:
            return datetime.strptime(str(val), "%Y-%m-%d").date()
        except Exception:
            return None

# â”€â”€ Fetch helpers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
def fetch_json(url, retries=2, delay=1):
    for attempt in range(retries):
        try:
            resp = requests.get(url, timeout=15)
            resp.raise_for_status()
            return resp.json()
        except requests.RequestException as e:
            print(f"  Attempt {attempt+1}/{retries} failed: {e}")
            if attempt < retries - 1:
                time.sleep(delay)
    return None

def fetch_all_cards_for_set(set_id):
    all_cards = []
    page = 1
    while True:
        url = f"{BASE_URL}/cards?q=set.id:{set_id}&pageSize={PAGE_SIZE}&page={page}"
        data = fetch_json(url)
        if not data or not data.get("data"):
            break
        all_cards.extend(data["data"])
        if len(data["data"]) < PAGE_SIZE:
            break
        page += 1
        # time.sleep(0.05)
    return all_cards

# â”€â”€ Set type classifier â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
def classify_set_type(set_data):
    name = set_data.get("name", "").lower()
    series = set_data.get("series", "").lower()
    set_id = set_data.get("id", "").lower()
    if "promo" in name or set_id.endswith("p"):
        return "promo"
    if "trainer kit" in name or "theme deck" in name:
        return "starter_deck"
    if series in ["pop", "other", "np"] or "mcdonald" in name:
        return "special"
    return "booster"

# â”€â”€ Build card text â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
def build_card_text(card):
    parts = []
    if card.get("flavorText"):
        parts.append(card["flavorText"])
    if card.get("abilities"):
        for ability in card["abilities"]:
            parts.append(f"[{ability.get('type', 'Ability')}] {ability.get('name', '')}: {ability.get('text', '')}")
    if card.get("attacks"):
        for attack in card["attacks"]:
            cost = " ".join(attack.get("cost", []))
            damage = attack.get("damage", "")
            text = attack.get("text", "")
            parts.append(f"[Attack] {attack.get('name', '')} ({cost}) {damage}: {text}")
    if card.get("weaknesses"):
        for w in card["weaknesses"]:
            parts.append(f"Weakness: {w.get('type', '')} {w.get('value', '')}")
    if card.get("resistances"):
        for r in card.get("resistances", []):
            parts.append(f"Resistance: {r.get('type', '')} {r.get('value', '')}")
    if card.get("retreatCost"):
        parts.append(f"Retreat: {len(card['retreatCost'])}")
    if card.get("rules"):
        for rule in card["rules"]:
            parts.append(f"[Rule] {rule}")
    return "\n".join(parts) if parts else None

# â”€â”€ Batch insert helpers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
def insert_sets(conn, sets_data):
    with conn.cursor() as cur:
        execute_values(cur, """
            INSERT INTO sets (game_id, set_code, set_name, set_type, release_date, card_count)
            VALUES %s
            ON CONFLICT (game_id, set_code) DO UPDATE
            SET set_name = EXCLUDED.set_name,
                set_type = EXCLUDED.set_type,
                release_date = EXCLUDED.release_date,
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

# â”€â”€ Main scraper â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
def main():
    print("TCG Card Scraper - Pokemon TCG (Focused Sets)")
    print(f"DB: {DB_URL[:60]}...")
    print(f"API: {BASE_URL}")
    print()

    conn = get_conn()
    print("Connected to database.")

    # Step 1: Fetch all sets
    print("\n=== Fetching all sets ===")
    sets_resp = fetch_json(f"{BASE_URL}/sets?pageSize=250")
    if not sets_resp or not sets_resp.get("data"):
        print("Failed to fetch sets.")
        return
    
    all_sets = sets_resp["data"]
    
    # Filter to target sets
    target_sets = []
    for s in all_sets:
        if s.get("series") in TARGET_SERIES or s.get("id") in CLASSIC_SETS:
            target_sets.append(s)
    
    print(f"Total sets: {len(all_sets)}")
    print(f"Target sets: {len(target_sets)}")
    for s in target_sets:
        print(f"  - {s['id']}: {s['name']} ({s['series']}, {s.get('total', 0)} cards)")

    # Insert sets
    sets_to_insert = []
    for s in target_sets:
        set_code = safe_str(s.get("id"), 50)
        set_name = safe_str(s.get("name"), 300)
        set_type = classify_set_type(s)
        release_date = parse_date(s.get("releaseDate"))
        card_count = s.get("total", 0)
        sets_to_insert.append((GAME_ID, set_code, set_name, set_type, release_date, card_count))

    insert_sets(conn, sets_to_insert)
    print(f"\nInserted/updated {len(sets_to_insert)} sets.")

    set_id_map = get_set_id_map(conn)
    print(f"Set ID map: {len(set_id_map)} entries.")

    # Step 2: Fetch cards for each set
    print("\n=== Fetching cards ===")
    all_cards_raw = []
    for i, s in enumerate(target_sets):
        set_code = s.get("id")
        set_name = s.get("name")
        print(f"[{i+1}/{len(target_sets)}] {set_code} - {set_name} ...", end=" ")
        cards = fetch_all_cards_for_set(set_code)
        print(f"{len(cards)} cards")
        all_cards_raw.extend(cards)
        # time.sleep(0.2)

    print(f"\nTotal cards fetched: {len(all_cards_raw)}")

    # Step 3: Process and insert
    print("\n=== Processing cards ===")
    card_dict = {}
    variant_list = []
    today = datetime.now().date()

    for card in all_cards_raw:
        set_code = safe_str(card.get("set", {}).get("id"), 50)
        set_id = set_id_map.get(set_code)
        if set_id is None:
            continue

        card_set_id = safe_str(card.get("id"), 200)
        card_name = safe_str(card.get("name"), 500)
        card_text = build_card_text(card)
        card_type = safe_str(card.get("supertype"), 50)
        card_color = ", ".join(card.get("types", [])) if card.get("types") else None
        rarity = safe_str(card.get("rarity"), 20)
        
        attacks = card.get("attacks", [])
        card_cost = safe_str(str(attacks[0].get("convertedEnergyCost")) if attacks else None, 20)
        card_power = safe_str(card.get("hp"), 20)
        life = None
        counter_amount = safe_str(card.get("level"), 20)
        attribute = safe_str(card.get("artist"), 100)
        
        sub_parts = []
        if card.get("subtypes"):
            sub_parts.extend(card["subtypes"])
        if card.get("evolvesFrom"):
            sub_parts.append(f"Evolves from: {card['evolvesFrom']}")
        if card.get("evolvesTo"):
            sub_parts.append(f"Evolves to: {', '.join(card['evolvesTo'])}")
        sub_types = ", ".join(sub_parts) if sub_parts else None
        
        images = card.get("images", {})
        card_image = images.get("small")
        card_image_id = safe_str(card.get("number"), 100)
        source = "pokemontcg"

        key = (card_set_id, set_id)
        card_tuple = (
            GAME_ID, set_id, card_set_id, card_name, card_text,
            card_type, card_color, rarity, card_cost, card_power, life,
            counter_amount, attribute, sub_types, card_image, card_image_id,
            today, source
        )
        card_dict[key] = card_tuple

        tcgplayer = card.get("tcgplayer")
        if tcgplayer and tcgplayer.get("prices"):
            for price_type, price_data in tcgplayer["prices"].items():
                variant_code = f"{card_set_id}_{price_type}"
                variant_name = f"{card_name} ({price_type})"
                market_price = to_decimal(price_data.get("market"))
                inventory_price = to_decimal(price_data.get("low"))
                variant_list.append((
                    None, variant_code, variant_name, card_image, card_image_id,
                    market_price, inventory_price, today, key
                ))
        else:
            variant_code = card_set_id
            variant_name = card_name
            variant_list.append((
                None, variant_code, variant_name, card_image, card_image_id,
                None, None, today, key
            ))

    all_cards = list(card_dict.values())
    print(f"Total unique cards to insert: {len(all_cards)}")
    print(f"Total variants to insert: {len(variant_list)}")

    print("\n=== Inserting cards ===")
    insert_cards(conn, all_cards)
    print(f"Inserted/updated {len(all_cards)} cards.")

    print("\n=== Inserting variants ===")
    card_id_map = get_card_id_map(conn)
    final_variants = []
    for v in variant_list:
        _, variant_code, variant_name, card_image, card_image_id, market_price, inventory_price, date_scraped, key = v
        card_id = card_id_map.get(key)
        if card_id:
            final_variants.append((
                card_id, variant_code, variant_name, card_image, card_image_id,
                market_price, inventory_price, date_scraped
            ))

    insert_variants(conn, final_variants)
    print(f"Inserted/updated {len(final_variants)} variants.")

    # Update card counts
    print("\n=== Updating set card counts ===")
    with conn.cursor() as cur:
        cur.execute("""
            UPDATE sets s SET card_count = (
                SELECT count(*) FROM cards c WHERE c.set_id = s.id AND c.game_id = %s
            ) WHERE s.game_id = %s
        """, (GAME_ID, GAME_ID))
    conn.commit()

    conn.close()
    print(f"\n=== DONE ===")
    print(f"Total cards: {len(all_cards)}")
    print(f"Total variants: {len(final_variants)}")
    print("Database connection closed.")

if __name__ == "__main__":
    main()
