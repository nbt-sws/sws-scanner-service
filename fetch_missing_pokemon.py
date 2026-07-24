#!/usr/bin/env python3
"""Fetch missing Pokemon sets that timed out."""
import os
import time
import requests
from datetime import datetime
from decimal import Decimal, InvalidOperation

import psycopg2
from psycopg2.extras import execute_values

from db_config import get_db_url

DB_URL = get_db_url()
BASE_URL = "https://api.pokemontcg.io/v2"
GAME_ID = 2

MISSING_SETS = ["sv8", "sv9", "sv10"]

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

def fetch_json(url, retries=3, delay=2):
    for attempt in range(retries):
        try:
            resp = requests.get(url, timeout=30)
            resp.raise_for_status()
            return resp.json()
        except requests.RequestException as e:
            print(f"  Attempt {attempt+1}/{retries} failed: {e}")
            if attempt < retries - 1:
                time.sleep(delay * (attempt + 1))
    return None

def fetch_all_cards_for_set(set_id):
    all_cards = []
    page = 1
    while True:
        url = f"{BASE_URL}/cards?q=set.id:{set_id}&pageSize=250&page={page}"
        data = fetch_json(url)
        if not data or not data.get("data"):
            break
        all_cards.extend(data["data"])
        if len(data["data"]) < 250:
            break
        page += 1
        time.sleep(0.5)
    return all_cards

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

def main():
    conn = get_conn()
    cur = conn.cursor()
    cur.execute("SELECT id, set_code FROM sets WHERE game_id = %s", (GAME_ID,))
    set_id_map = {row[1]: row[0] for row in cur.fetchall()}
    cur.close()

    today = datetime.now().date()
    all_cards_raw = []

    for set_code in MISSING_SETS:
        set_id = set_id_map.get(set_code)
        if not set_id:
            print(f"Set {set_code} not found in DB")
            continue
        print(f"Fetching {set_code} ...", end=" ")
        cards = fetch_all_cards_for_set(set_code)
        print(f"{len(cards)} cards")
        all_cards_raw.extend(cards)

    if not all_cards_raw:
        print("No cards fetched.")
        return

    print(f"\nProcessing {len(all_cards_raw)} cards...")

    card_dict = {}
    variant_list = []
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
    print(f"Inserting {len(all_cards)} cards...")

    with conn.cursor() as cur:
        execute_values(cur, """
            INSERT INTO cards (game_id, set_id, card_set_id, card_name, card_text,
                card_type, card_color, rarity, card_cost, card_power, life,
                counter_amount, attribute, sub_types, card_image, card_image_id,
                date_scraped, source)
            VALUES %s
            ON CONFLICT (game_id, card_set_id, set_id) DO UPDATE
            SET card_name = EXCLUDED.card_name, card_text = EXCLUDED.card_text,
                card_type = EXCLUDED.card_type, card_color = EXCLUDED.card_color,
                rarity = EXCLUDED.rarity, card_cost = EXCLUDED.card_cost,
                card_power = EXCLUDED.card_power, life = EXCLUDED.life,
                counter_amount = EXCLUDED.counter_amount, attribute = EXCLUDED.attribute,
                sub_types = EXCLUDED.sub_types, card_image = EXCLUDED.card_image,
                card_image_id = EXCLUDED.card_image_id, date_scraped = EXCLUDED.date_scraped,
                updated_at = NOW()
        """, all_cards)
    conn.commit()

    # Get card_id map
    with conn.cursor() as cur:
        cur.execute("SELECT id, card_set_id, set_id FROM cards WHERE game_id = %s", (GAME_ID,))
        card_id_map = {(row[1], row[2]): row[0] for row in cur.fetchall()}

    final_variants = []
    for v in variant_list:
        _, variant_code, variant_name, card_image, card_image_id, market_price, inventory_price, date_scraped, key = v
        card_id = card_id_map.get(key)
        if card_id:
            final_variants.append((
                card_id, variant_code, variant_name, card_image, card_image_id,
                market_price, inventory_price, date_scraped
            ))

    print(f"Inserting {len(final_variants)} variants...")
    with conn.cursor() as cur:
        execute_values(cur, """
            INSERT INTO card_variants (card_id, variant_code, variant_name, card_image,
                card_image_id, market_price, inventory_price, date_scraped)
            VALUES %s
            ON CONFLICT (card_id, variant_code) DO UPDATE
            SET variant_name = EXCLUDED.variant_name, card_image = EXCLUDED.card_image,
                card_image_id = EXCLUDED.card_image_id, market_price = EXCLUDED.market_price,
                inventory_price = EXCLUDED.inventory_price, date_scraped = EXCLUDED.date_scraped,
                updated_at = NOW()
        """, final_variants)
    conn.commit()

    # Update card counts
    with conn.cursor() as cur:
        cur.execute("""
            UPDATE sets s SET card_count = (
                SELECT count(*) FROM cards c WHERE c.set_id = s.id AND c.game_id = %s
            ) WHERE s.game_id = %s AND s.set_code IN %s
        """, (GAME_ID, GAME_ID, tuple(MISSING_SETS)))
    conn.commit()

    conn.close()
    print(f"Done! Inserted {len(all_cards)} cards, {len(final_variants)} variants.")

if __name__ == "__main__":
    main()
