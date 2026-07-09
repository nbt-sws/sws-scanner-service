# TCG Card Database - Project Summary

## Overview
ระบบฐานข้อมูลการ์ด TCG (Trading Card Game) รองรับหลายเกม โดยใช้ PostgreSQL + FastAPI + Python Scraper

## Database State (Current)

| Metric | Count |
|--------|-------|
| Games | 3 |
| Sets | 121 |
| Cards | 7,207 |
| Variants | 11,011 |

### By Game

| Game | Sets | Cards | Variants |
|------|------|-------|----------|
| One Piece TCG | 55 | 3,349 | 4,890 |
| Pokemon TCG | 66 | 3,858 | 6,121 |
| Digimon Card Game | 0 | 0 | 0 |

## Files

| File | Description |
|------|-------------|
| `schema.sql` | PostgreSQL schema (games, sets, cards, variants, prices, translations) |
| `scraper.py` | One Piece TCG scraper (optcgapi.com) |
| `scraper_pokemon.py` | Pokemon TCG scraper (pokemontcg.io) |
| `fetch_missing_pokemon.py` | Fetch missing Pokemon sets that timed out |
| `api.py` | FastAPI CRUD API |
| `test_api.py` | API tests for One Piece |
| `test_pokemon_api.py` | API tests for Pokemon |
| `design.md` | Design documentation |

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/games` | List all games |
| GET | `/games/{slug}` | Get game by slug |
| GET | `/sets` | List sets (filter: game_id, set_type) |
| GET | `/sets/{id}` | Get set by ID |
| GET | `/cards` | List cards (filter, search, paginate) |
| GET | `/cards/{id}` | Get card by ID |
| GET | `/cards/{id}/variants` | List card variants |
| POST | `/cards` | Create new card |
| PUT | `/cards/{id}` | Update card |
| DELETE | `/cards/{id}` | Delete card |
| GET | `/variants/{id}` | Get variant by ID |
| GET | `/stats` | Database statistics |

## How to Run

### Run API Server
```bash
python api.py
# or
uvicorn api:app --host 0.0.0.0 --port 8000
```

### Scrape One Piece TCG
```bash
python scraper.py
```

### Scrape Pokemon TCG
```bash
python scraper_pokemon.py
```

### Fetch Missing Pokemon Sets
```bash
python fetch_missing_pokemon.py
```

### Run Tests
```bash
python test_api.py
python test_pokemon_api.py
```

## Data Sources

- **One Piece TCG**: https://optcgapi.com/documentation
- **Pokemon TCG**: https://pokemontcg.io/

## Next Steps
- [ ] Add Digimon Card Game data
- [ ] Add multi-language translations (JP, TH, ZH)
- [ ] Add price history tracking (card_prices table)
- [ ] Add deck builder functionality
- [ ] Add card collection/user inventory tracking
- [ ] Add advanced search with filters
- [ ] Add card image caching
- [ ] Add authentication (JWT)
