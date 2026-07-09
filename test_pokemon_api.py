from fastapi.testclient import TestClient
import api

client = TestClient(api.app)

def test_pokemon():
    # Test Pokemon game
    print("=== GET /games ===")
    resp = client.get("/games")
    print(resp.status_code, [g['name'] for g in resp.json()])

    # Test Pokemon sets
    print("\n=== GET /sets?game_id=2 ===")
    resp = client.get("/sets?game_id=2")
    pokemon_sets = resp.json()
    print(f"Status: {resp.status_code}, Sets: {len(pokemon_sets)}")
    for s in pokemon_sets[:3]:
        print(f"  - {s['set_code']}: {s['set_name']} ({s['card_count']} cards)")

    # Test Pokemon cards
    print("\n=== GET /cards?game_id=2 ===")
    resp = client.get("/cards?game_id=2&limit=5")
    pokemon_cards = resp.json()
    print(f"Status: {resp.status_code}, Cards: {len(pokemon_cards)}")
    for c in pokemon_cards[:3]:
        print(f"  - {c['card_set_id']}: {c['card_name']} ({c['card_type']}, {c['rarity']})")

    # Test Pokemon card search
    print("\n=== GET /cards?game_id=2&search=Charizard ===")
    resp = client.get("/cards?game_id=2&search=Charizard")
    print(f"Status: {resp.status_code}, Found: {len(resp.json())} cards")
    for c in resp.json()[:3]:
        print(f"  - {c['card_set_id']}: {c['card_name']}")

    # Test Pokemon card types
    print("\n=== GET /cards?game_id=2&card_type=Pokémon ===")
    resp = client.get("/cards?game_id=2&card_type=Pokémon")
    print(f"Status: {resp.status_code}, Pokémon cards: {len(resp.json())}")

    # Test card detail with variants
    print("\n=== GET /cards with variants ===")
    # Find a Pokemon card with variants
    resp = client.get("/cards?game_id=2&card_type=Pokémon&limit=1")
    card_id = resp.json()[0]['id']
    card_name = resp.json()[0]['card_name']
    print(f"Card ID: {card_id}, Name: {card_name}")
    
    resp = client.get(f"/cards/{card_id}")
    print(f"Detail: {resp.json()['card_name']}, Type: {resp.json()['card_type']}")
    
    resp = client.get(f"/cards/{card_id}/variants")
    variants = resp.json()
    print(f"Variants: {len(variants)}")
    for v in variants[:3]:
        print(f"  - {v['variant_code']}: {v['variant_name']} (market: ${v['market_price']})")

    # Test stats
    print("\n=== GET /stats ===")
    resp = client.get("/stats")
    print(resp.json())

    print("\n=== ALL POKEMON TESTS PASSED ===")

if __name__ == "__main__":
    test_pokemon()
