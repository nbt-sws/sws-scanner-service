#!/usr/bin/env python3
"""
TCG Card API - FastAPI CRUD
One Piece TCG Card Database API
"""
import os
from typing import Optional, List
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, Query
from pydantic import BaseModel
from sqlalchemy import create_engine, text
from sqlalchemy.orm import sessionmaker

# ── Config ───────────────────────────────────────────────────────
DB_URL = os.environ.get(
    "DATABASE_URL",
    "postgresql://neondb_owner:npg_0P9uMKUYgTSZ@ep-dry-dew-amvkqf9w-pooler.c-5.us-east-1.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
)

engine = create_engine(DB_URL, pool_pre_ping=True)
SessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)

# ── Pydantic Models ────────────────────────────────────────────
class GameOut(BaseModel):
    id: int
    slug: str
    name: str
    description: Optional[str]
    class Config:
        from_attributes = True

class SetOut(BaseModel):
    id: int
    game_id: int
    set_code: str
    set_name: Optional[str]
    set_type: Optional[str]
    release_date: Optional[str]
    card_count: int
    class Config:
        from_attributes = True

class CardVariantOut(BaseModel):
    id: int
    card_id: int
    variant_code: Optional[str]
    variant_name: Optional[str]
    card_image: Optional[str]
    card_image_id: Optional[str]
    market_price: Optional[float]
    inventory_price: Optional[float]
    date_scraped: Optional[str]
    class Config:
        from_attributes = True

class CardOut(BaseModel):
    id: int
    game_id: int
    set_id: Optional[int]
    card_set_id: str
    card_name: Optional[str]
    card_text: Optional[str]
    card_type: Optional[str]
    card_color: Optional[str]
    rarity: Optional[str]
    card_cost: Optional[str]
    card_power: Optional[str]
    life: Optional[str]
    counter_amount: Optional[str]
    attribute: Optional[str]
    sub_types: Optional[str]
    card_image: Optional[str]
    card_image_id: Optional[str]
    date_scraped: Optional[str]
    source: Optional[str]
    class Config:
        from_attributes = True

class CardCreate(BaseModel):
    game_id: int
    set_id: Optional[int] = None
    card_set_id: str
    card_name: Optional[str] = None
    card_text: Optional[str] = None
    card_type: Optional[str] = None
    card_color: Optional[str] = None
    rarity: Optional[str] = None
    card_cost: Optional[str] = None
    card_power: Optional[str] = None
    life: Optional[str] = None
    counter_amount: Optional[str] = None
    attribute: Optional[str] = None
    sub_types: Optional[str] = None
    card_image: Optional[str] = None
    card_image_id: Optional[str] = None
    source: Optional[str] = "optcgapi"

class CardUpdate(BaseModel):
    set_id: Optional[int] = None
    card_name: Optional[str] = None
    card_text: Optional[str] = None
    card_type: Optional[str] = None
    card_color: Optional[str] = None
    rarity: Optional[str] = None
    card_cost: Optional[str] = None
    card_power: Optional[str] = None
    life: Optional[str] = None
    counter_amount: Optional[str] = None
    attribute: Optional[str] = None
    sub_types: Optional[str] = None
    card_image: Optional[str] = None
    card_image_id: Optional[str] = None

# ── FastAPI App ─────────────────────────────────────────────────
@asynccontextmanager
async def lifespan(app: FastAPI):
    yield

app = FastAPI(
    title="TCG Card API",
    description="One Piece TCG Card Database API",
    version="1.0.0",
    lifespan=lifespan
)

def get_db():
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()

# ── Games ──────────────────────────────────────────────────────
@app.get("/games", response_model=List[GameOut])
def list_games():
    with SessionLocal() as db:
        result = db.execute(text("SELECT id, slug, name, description FROM games ORDER BY id"))
        rows = result.mappings().all()
        return [dict(r) for r in rows]

@app.get("/games/{slug}", response_model=GameOut)
def get_game(slug: str):
    with SessionLocal() as db:
        result = db.execute(text("SELECT id, slug, name, description FROM games WHERE slug = :slug"), {"slug": slug})
        row = result.mappings().first()
        if not row:
            raise HTTPException(status_code=404, detail="Game not found")
        return dict(row)

# ── Sets ────────────────────────────────────────────────────────
@app.get("/sets", response_model=List[SetOut])
def list_sets(
    game_id: Optional[int] = Query(None, description="Filter by game ID"),
    set_type: Optional[str] = Query(None, description="Filter by set type (booster, starter_deck, promo, don, extra_booster)"),
    skip: int = Query(0, ge=0),
    limit: int = Query(100, ge=1, le=1000)
):
    with SessionLocal() as db:
        sql = "SELECT id, game_id, set_code, set_name, set_type, release_date::text, card_count FROM sets WHERE 1=1"
        params = {}
        if game_id:
            sql += " AND game_id = :game_id"
            params["game_id"] = game_id
        if set_type:
            sql += " AND set_type = :set_type"
            params["set_type"] = set_type
        sql += " ORDER BY id LIMIT :limit OFFSET :skip"
        params["limit"] = limit
        params["skip"] = skip
        result = db.execute(text(sql), params)
        rows = result.mappings().all()
        return [dict(r) for r in rows]

@app.get("/sets/{set_id}", response_model=SetOut)
def get_set(set_id: int):
    with SessionLocal() as db:
        result = db.execute(text("""
            SELECT id, game_id, set_code, set_name, set_type, release_date::text, card_count
            FROM sets WHERE id = :set_id
        """), {"set_id": set_id})
        row = result.mappings().first()
        if not row:
            raise HTTPException(status_code=404, detail="Set not found")
        return dict(row)

# ── Cards ───────────────────────────────────────────────────────
@app.get("/cards", response_model=List[CardOut])
def list_cards(
    game_id: Optional[int] = Query(None, description="Filter by game ID"),
    set_id: Optional[int] = Query(None, description="Filter by set ID"),
    card_type: Optional[str] = Query(None, description="Filter by card type (Leader, Character, Event, Stage, DON!!)"),
    card_color: Optional[str] = Query(None, description="Filter by color (Red, Green, Blue, Purple, Yellow, Black)"),
    rarity: Optional[str] = Query(None, description="Filter by rarity (L, SR, R, UC, C, SEC, P, PR, etc.)"),
    search: Optional[str] = Query(None, description="Search by card name or text"),
    skip: int = Query(0, ge=0),
    limit: int = Query(100, ge=1, le=1000)
):
    with SessionLocal() as db:
        sql = """
            SELECT id, game_id, set_id, card_set_id, card_name, card_text,
                card_type, card_color, rarity, card_cost, card_power, life,
                counter_amount, attribute, sub_types, card_image, card_image_id,
                date_scraped::text, source
            FROM cards WHERE 1=1
        """
        params = {}
        if game_id:
            sql += " AND game_id = :game_id"
            params["game_id"] = game_id
        if set_id:
            sql += " AND set_id = :set_id"
            params["set_id"] = set_id
        if card_type:
            sql += " AND card_type = :card_type"
            params["card_type"] = card_type
        if card_color:
            sql += " AND card_color = :card_color"
            params["card_color"] = card_color
        if rarity:
            sql += " AND rarity = :rarity"
            params["rarity"] = rarity
        if search:
            sql += " AND (card_name ILIKE :search OR card_text ILIKE :search OR card_set_id ILIKE :search)"
            params["search"] = f"%{search}%"
        sql += " ORDER BY id LIMIT :limit OFFSET :skip"
        params["limit"] = limit
        params["skip"] = skip
        result = db.execute(text(sql), params)
        rows = result.mappings().all()
        return [dict(r) for r in rows]

@app.get("/cards/{card_id}", response_model=CardOut)
def get_card(card_id: int):
    with SessionLocal() as db:
        result = db.execute(text("""
            SELECT id, game_id, set_id, card_set_id, card_name, card_text,
                card_type, card_color, rarity, card_cost, card_power, life,
                counter_amount, attribute, sub_types, card_image, card_image_id,
                date_scraped::text, source
            FROM cards WHERE id = :card_id
        """), {"card_id": card_id})
        row = result.mappings().first()
        if not row:
            raise HTTPException(status_code=404, detail="Card not found")
        return dict(row)

@app.get("/cards/{card_id}/variants", response_model=List[CardVariantOut])
def get_card_variants(card_id: int):
    with SessionLocal() as db:
        result = db.execute(text("""
            SELECT id, card_id, variant_code, variant_name, card_image,
                card_image_id, market_price, inventory_price, date_scraped::text
            FROM card_variants WHERE card_id = :card_id
            ORDER BY id
        """), {"card_id": card_id})
        rows = result.mappings().all()
        return [dict(r) for r in rows]

@app.post("/cards", response_model=CardOut)
def create_card(card: CardCreate):
    with SessionLocal() as db:
        result = db.execute(text("""
            INSERT INTO cards (game_id, set_id, card_set_id, card_name, card_text,
                card_type, card_color, rarity, card_cost, card_power, life,
                counter_amount, attribute, sub_types, card_image, card_image_id, source)
            VALUES (:game_id, :set_id, :card_set_id, :card_name, :card_text,
                :card_type, :card_color, :rarity, :card_cost, :card_power, :life,
                :counter_amount, :attribute, :sub_types, :card_image, :card_image_id, :source)
            RETURNING id, game_id, set_id, card_set_id, card_name, card_text,
                card_type, card_color, rarity, card_cost, card_power, life,
                counter_amount, attribute, sub_types, card_image, card_image_id,
                date_scraped::text, source
        """), card.model_dump())
        db.commit()
        row = result.mappings().first()
        return dict(row)

@app.put("/cards/{card_id}", response_model=CardOut)
def update_card(card_id: int, card: CardUpdate):
    with SessionLocal() as db:
        # Check exists
        check = db.execute(text("SELECT id FROM cards WHERE id = :card_id"), {"card_id": card_id})
        if not check.fetchone():
            raise HTTPException(status_code=404, detail="Card not found")

        # Build dynamic update
        fields = []
        params = {"card_id": card_id}
        for key, value in card.model_dump(exclude_unset=True).items():
            if value is not None:
                fields.append(f"{key} = :{key}")
                params[key] = value
        if not fields:
            raise HTTPException(status_code=400, detail="No fields to update")

        sql = "UPDATE cards SET " + ", ".join(fields) + ", updated_at = NOW() WHERE id = :card_id"
        db.execute(text(sql), params)
        db.commit()

        result = db.execute(text("""
            SELECT id, game_id, set_id, card_set_id, card_name, card_text,
                card_type, card_color, rarity, card_cost, card_power, life,
                counter_amount, attribute, sub_types, card_image, card_image_id,
                date_scraped::text, source
            FROM cards WHERE id = :card_id
        """), {"card_id": card_id})
        row = result.mappings().first()
        return dict(row)

@app.delete("/cards/{card_id}")
def delete_card(card_id: int):
    with SessionLocal() as db:
        check = db.execute(text("SELECT id FROM cards WHERE id = :card_id"), {"card_id": card_id})
        if not check.fetchone():
            raise HTTPException(status_code=404, detail="Card not found")
        db.execute(text("DELETE FROM cards WHERE id = :card_id"), {"card_id": card_id})
        db.commit()
        return {"message": "Card deleted successfully", "card_id": card_id}

# ── Variants ────────────────────────────────────────────────────
@app.get("/variants/{variant_id}", response_model=CardVariantOut)
def get_variant(variant_id: int):
    with SessionLocal() as db:
        result = db.execute(text("""
            SELECT id, card_id, variant_code, variant_name, card_image,
                card_image_id, market_price, inventory_price, date_scraped::text
            FROM card_variants WHERE id = :variant_id
        """), {"variant_id": variant_id})
        row = result.mappings().first()
        if not row:
            raise HTTPException(status_code=404, detail="Variant not found")
        return dict(row)

# ── Stats ───────────────────────────────────────────────────────
@app.get("/stats")
def get_stats():
    with SessionLocal() as db:
        games = db.execute(text("SELECT count(*) FROM games")).scalar()
        sets = db.execute(text("SELECT count(*) FROM sets")).scalar()
        cards = db.execute(text("SELECT count(*) FROM cards")).scalar()
        variants = db.execute(text("SELECT count(*) FROM card_variants")).scalar()
        return {
            "games": games,
            "sets": sets,
            "cards": cards,
            "variants": variants
        }

# ── Run ─────────────────────────────────────────────────────────
if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
