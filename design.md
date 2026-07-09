# TCG Card Database Design Document

## Overview
ระบบฐานข้อมูลการ์ด TCG (Trading Card Game) รองรับหลายเกม เช่น One Piece TCG, Pokemon TCG, Digimon Card Game โดยเริ่มต้นจาก One Piece TCG

## Tech Stack
- **Database**: PostgreSQL (Neon)
- **Backend**: Python 3.12 + FastAPI + SQLAlchemy
- **Scraper**: Python requests + psycopg2
- **Data Source**: [optcgapi.com](https://optcgapi.com/documentation)

## Database Schema

### 1. games
ตารางเกม
| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | SERIAL | PK | |
| slug | VARCHAR(50) | UNIQUE, NOT NULL | ชื่อย่อ (onepiece, pokemon) |
| name | VARCHAR(100) | NOT NULL | ชื่อเต็ม |
| description | TEXT | | รายละเอียด |
| created_at | TIMESTAMP | DEFAULT NOW() | |

**CRUD**
- `GET /games` - list all games
- `GET /games/{slug}` - get game by slug

### 2. sets
ตารางชุดการ์ด (Booster, Starter Deck, Promo, Extra Booster, DON)
| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | SERIAL | PK | |
| game_id | INTEGER | FK(games), NOT NULL | |
| set_code | VARCHAR(50) | NOT NULL | รหัสชุด (OP-01, ST-01, P) |
| set_name | VARCHAR(300) | | ชื่อชุด |
| set_type | VARCHAR(50) | | booster, starter_deck, promo, extra_booster, don |
| release_date | DATE | | |
| card_count | INTEGER | DEFAULT 0 | |
| created_at | TIMESTAMP | DEFAULT NOW() | |
| updated_at | TIMESTAMP | DEFAULT NOW() | |
| UNIQUE(game_id, set_code) | | | |

**CRUD**
- `GET /sets` - list sets (filter: game_id, set_type)
- `GET /sets/{id}` - get set by ID

### 3. cards
ตารางการ์ด
| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | SERIAL | PK | |
| game_id | INTEGER | FK(games), NOT NULL | |
| set_id | INTEGER | FK(sets) | |
| card_set_id | VARCHAR(200) | NOT NULL | รหัสการ์ด (OP01-001) |
| card_name | VARCHAR(500) | | |
| card_text | TEXT | | |
| card_type | VARCHAR(50) | | Leader, Character, Event, Stage, DON!! |
| card_color | VARCHAR(50) | | Red, Green, Blue, Purple, Yellow, Black |
| rarity | VARCHAR(20) | | L, SR, R, UC, C, SEC, P, PR |
| card_cost | VARCHAR(20) | | |
| card_power | VARCHAR(20) | | |
| life | VARCHAR(20) | | |
| counter_amount | VARCHAR(20) | | |
| attribute | VARCHAR(100) | | Slash, Strike, Special, Wisdom |
| sub_types | VARCHAR(300) | | Straw Hat Crew, Supernovas |
| card_image | TEXT | | URL รูปการ์ด |
| card_image_id | VARCHAR(100) | | |
| date_scraped | DATE | | |
| source | VARCHAR(50) | DEFAULT 'optcgapi' | |
| created_at | TIMESTAMP | DEFAULT NOW() | |
| updated_at | TIMESTAMP | DEFAULT NOW() | |
| UNIQUE(game_id, card_set_id, set_id) | | | |

**CRUD**
- `GET /cards` - list cards (filter: game_id, set_id, card_type, card_color, rarity, search)
- `GET /cards/{id}` - get card by ID
- `POST /cards` - create new card
- `PUT /cards/{id}` - update card
- `DELETE /cards/{id}` - delete card

### 4. card_variants
ตารางรูปแบบการ์ด (Parallel, Alternate Art, Promo versions)
| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | SERIAL | PK | |
| card_id | INTEGER | FK(cards), NOT NULL | |
| variant_code | VARCHAR(200) | | รหัส variant (OP01-001_p1) |
| variant_name | VARCHAR(500) | | ชื่อ variant |
| card_image | TEXT | | URL รูป |
| card_image_id | VARCHAR(100) | | |
| market_price | DECIMAL(12,2) | | ราคาตลาด |
| inventory_price | DECIMAL(12,2) | | ราคาสต็อก |
| date_scraped | DATE | | |
| created_at | TIMESTAMP | DEFAULT NOW() | |
| updated_at | TIMESTAMP | DEFAULT NOW() | |
| UNIQUE(card_id, variant_code) | | | |

**CRUD**
- `GET /cards/{card_id}/variants` - list variants of a card
- `GET /variants/{id}` - get variant by ID

### 5. card_prices
ตารางประวัติราคา
| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | SERIAL | PK | |
| card_variant_id | INTEGER | FK(card_variants), NOT NULL | |
| market_price | DECIMAL(12,2) | | |
| inventory_price | DECIMAL(12,2) | | |
| date_scraped | DATE | | |
| created_at | TIMESTAMP | DEFAULT NOW() | |

### 6. card_translations
ตารางการแปลภาษา (รองรับ multi-language)
| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | SERIAL | PK | |
| card_id | INTEGER | FK(cards), NOT NULL | |
| language | VARCHAR(10) | NOT NULL | en, jp, th, zh, ko |
| card_name | VARCHAR(500) | | |
| card_text | TEXT | | |
| card_image | TEXT | | |
| created_at | TIMESTAMP | DEFAULT NOW() | |
| updated_at | TIMESTAMP | DEFAULT NOW() | |
| UNIQUE(card_id, language) | | | |

## Data Sources
- **One Piece TCG**: [optcgapi.com](https://optcgapi.com/documentation)
- **Pokemon TCG**: [pokemontcg.io](https://pokemontcg.io/)
- **Digimon Card Game**: TBD

## Current Database State
| Game | Sets | Cards | Variants |
|------|------|-------|----------|
| One Piece TCG | 55 | 3,349 | 4,890 |
| Pokemon TCG | 66 | 3,858 | 6,121 |
| Digimon Card Game | 0 | 0 | 0 |
| **Total** | **121** | **7,207** | **11,011** |

### API Flow
```
GET /games
  → GET /sets?game_id=1
    → GET /cards?set_id=1
      → GET /cards/{id}/variants
```

## Endpoints Summary

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | /games | List all games |
| GET | /games/{slug} | Get game details |
| GET | /sets | List sets (filterable) |
| GET | /sets/{id} | Get set details |
| GET | /cards | List cards (filterable, paginated) |
| GET | /cards/{id} | Get card details |
| GET | /cards/{id}/variants | List card variants |
| POST | /cards | Create card |
| PUT | /cards/{id} | Update card |
| DELETE | /cards/{id} | Delete card |
| GET | /variants/{id} | Get variant details |
| GET | /stats | Database stats |

## Multi-language Support
- ตาราง `card_translations` รองรับการแปล card_name และ card_text เป็นหลายภาษา
- เริ่มต้นข้อมูลจาก optcgapi.com เป็นภาษาอังกฤษ (en)
- สามารถเพิ่มภาษาอื่น (jp, th, zh) ผ่าน API หรือ scraping จากแหล่งอื่น

## Card Type Classification
- **Leader**: การ์ดผู้นำ
- **Character**: การ์ดตัวละคร
- **Event**: การ์ดเหตุการณ์
- **Stage**: การ์ดเวที
- **DON!!**: การ์ด DON!!

## Set Type Classification
- **booster**: Booster Pack (OP-01, OP-02, ...)
- **starter_deck**: Starter Deck (ST-01, ST-02, ...)
- **promo**: Promotion Cards (P, PR)
- **extra_booster**: Extra Booster (EB-01, EB-02)
- **don**: DON!! Cards
- **event_pack**: Event Pack (PRB-01, PRB-02)

## Indexes
- GIN trigram index บน card_name และ sub_types สำหรับ fuzzy search
- B-tree index บน card_set_id, card_type, card_color, rarity
- Foreign key index บน game_id, set_id

## Security Notes
- Database connection string เก็บใน environment variable
- API ไม่มี authentication ในตอนนี้ (สำหรับ development)
- สามารถเพิ่ม JWT/OAuth สำหรับ production
