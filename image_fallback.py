#!/usr/bin/env python3
"""
Image Fallback Scraper - Fetches missing card images from official OPTCG website
"""
import os
import time
import requests
import psycopg2

from db_config import get_db_url

DB_URL = get_db_url()

# Official image sources
OFFICIAL_EN = "https://en.onepiece-cardgame.com/images/cardlist/card/{card_id}.png"
OFFICIAL_JP = "https://www.onepiece-cardgame.com/images/cardlist/card/{card_id}.png"


def get_conn():
    return psycopg2.connect(DB_URL)


def get_missing_images(conn):
    """Get all variants with missing images"""
    with conn.cursor() as cur:
        cur.execute("""
            SELECT cv.id, cv.card_id, c.card_set_id, cv.variant_code, cv.variant_name
            FROM card_variants cv
            JOIN cards c ON cv.card_id = c.id
            WHERE cv.card_image IS NULL OR cv.card_image = ''
            ORDER BY c.card_set_id, cv.variant_code
        """)
        return cur.fetchall()


def try_official_image(card_id, variant_code):
    """Try to find image from official sources"""
    variants_to_try = [variant_code, card_id]

    for vid in variants_to_try:
        if not vid:
            continue

        # English site
        url = OFFICIAL_EN.format(card_id=vid)
        try:
            resp = requests.head(url, timeout=10, allow_redirects=True)
            if resp.status_code == 200:
                return url
        except:
            pass

        # Japanese site
        url = OFFICIAL_JP.format(card_id=vid)
        try:
            resp = requests.head(url, timeout=10, allow_redirects=True)
            if resp.status_code == 200:
                return url
        except:
            pass

        # Try with suffixes for parallel/alt art
        for suffix in ["_p1", "_p2", "_pr1", "_pr2"]:
            if vid.endswith(suffix):
                continue
            url = OFFICIAL_EN.format(card_id=vid + suffix)
            try:
                resp = requests.head(url, timeout=10, allow_redirects=True)
                if resp.status_code == 200:
                    return url
            except:
                pass

    return None


def update_variant_image(conn, variant_id, image_url):
    with conn.cursor() as cur:
        cur.execute("""
            UPDATE card_variants
            SET card_image = %s, updated_at = NOW()
            WHERE id = %s
        """, (image_url, variant_id))
    conn.commit()


def main():
    print("Image Fallback Scraper - One Piece TCG")
    print(f"DB: {DB_URL[:60]}...")
    print()

    conn = get_conn()
    print("Connected to database.")

    missing = get_missing_images(conn)
    print(f"Variants with missing images: {len(missing)}")
    print()

    found = 0
    not_found = []

    for idx, row in enumerate(missing, 1):
        variant_id, card_id, card_set_id, variant_code, variant_name = row
        name_short = (variant_name or "")[:50]
        print(f"[{idx}/{len(missing)}] {variant_code} ({card_set_id}) - {name_short}... ", end="", flush=True)

        image_url = try_official_image(card_set_id, variant_code)
        if image_url:
            update_variant_image(conn, variant_id, image_url)
            print(f"FOUND")
            found += 1
        else:
            print("NOT FOUND")
            not_found.append(variant_code)

        time.sleep(0.15)

    print()
    print("=" * 50)
    print("SUMMARY")
    print("=" * 50)
    print(f"Total missing: {len(missing)}")
    print(f"Found images: {found}")
    print(f"Still missing: {len(not_found)}")

    if not_found:
        print("\nStill missing:")
        for v in not_found:
            print(f"  {v}")

    conn.close()
    print("\nDatabase connection closed.")


if __name__ == "__main__":
    main()
