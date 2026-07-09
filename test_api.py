from fastapi.testclient import TestClient
import api

client = TestClient(api.app)

def test_all():
    # Test games
    print("=== GET /games ===")
    resp = client.get("/games")
    print(resp.status_code, resp.json()[:2])

    # Test sets
    print("\n=== GET /sets ===")
    resp = client.get("/sets")
    print(resp.status_code, len(resp.json()))

    # Test sets with filter
    print("\n=== GET /sets?game_id=1 ===")
    resp = client.get("/sets?game_id=1")
    print(resp.status_code, len(resp.json()))

    # Test cards
    print("\n=== GET /cards ===")
    resp = client.get("/cards")
    print(resp.status_code, len(resp.json()))

    # Test cards with filter
    print("\n=== GET /cards?card_type=Leader ===")
    resp = client.get("/cards?card_type=Leader")
    print(resp.status_code, len(resp.json()))

    # Test cards with search
    print("\n=== GET /cards?search=Zoro ===")
    resp = client.get("/cards?search=Zoro")
    print(resp.status_code, len(resp.json()))

    # Test card detail
    print("\n=== GET /cards/1 ===")
    resp = client.get("/cards/1")
    print(resp.status_code, resp.json() and resp.json().get("card_name"))

    # Test variants
    print("\n=== GET /cards/1/variants ===")
    resp = client.get("/cards/1/variants")
    print(resp.status_code, len(resp.json()))

    # Test stats
    print("\n=== GET /stats ===")
    resp = client.get("/stats")
    print(resp.status_code, resp.json())

    # Test create card
    print("\n=== POST /cards ===")
    new_card = {
        "game_id": 1,
        "set_id": 1,
        "card_set_id": "TEST-001",
        "card_name": "Test Card",
        "card_type": "Character",
        "card_color": "Red",
        "rarity": "C"
    }
    resp = client.post("/cards", json=new_card)
    print(resp.status_code, resp.json())
    test_card_id = resp.json().get("id") if resp.status_code == 200 else None

    # Test update card
    if test_card_id:
        print("\n=== PUT /cards/{test_card_id} ===")
        resp = client.put(f"/cards/{test_card_id}", json={"card_name": "Test Card Updated"})
        print(resp.status_code, resp.json().get("card_name"))

    # Test delete card
    if test_card_id:
        print("\n=== DELETE /cards/{test_card_id} ===")
        resp = client.delete(f"/cards/{test_card_id}")
        print(resp.status_code, resp.json())

    print("\n=== ALL TESTS PASSED ===")

if __name__ == "__main__":
    test_all()
