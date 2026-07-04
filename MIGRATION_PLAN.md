# Scanner Service Migration Plan

> สรุปสถานะปัจจุบัน + แผน migration จาก `sws-scanner-app` (Vercel serverless) มาเป็น `sws-scanner-service` (Go microservice) และ integration เข้ากับ `swibs` platform

---

## 1. สรุปสถานะปัจจุบัน

### 1.1 รันได้แล้ว (จากการทดสอบล่าสุด)
- `sws-scanner-service` รันบน `http://localhost:8088` (HTTP) และ `grpc://localhost:9090` (gRPC)
- `sws-scanner-app` รันบน `http://localhost:3000` ผ่าน `npm start`
- Postgres Docker container `scanner-postgres` รันบน `localhost:5433`
- Health checks `/healthz`, `/readyz` ตอบกลับปกติ
- Proxy `/v1/*` จาก dev server ไปยัง Go service ทำงานได้
- API gateway (`sws-svc-api-gateway`) ถูกตั้งค่าให้ proxy `/api/v1/scan`, `/prices`, `/quality`, ... ไปยัง scanner-service แล้ว
- `scanner.v1` proto generated code มีใน `sws-shared-protos/gen/go/scanner/v1`

### 1.2 คำตอบสั้น ๆ ตามคำถาม

| คำถาม | คำตอบ |
|-------|--------|
| **Integrate เข้า swibs ครบถ้วนแล้ว?** | **ยังไม่ครบทั้งหมด** แต่ก้าวหน้าแล้ว: gRPC server + generated proto + API gateway route เสร็จแล้ว; K8s/ArgoCD/shared events ยังไม่มี |
| **Env / API ทำงานได้เหมือนเดิม 100% แล้วหรือยัง?** | **ยังไม่ 100%** — pricing ยังต้องใช้ eBay credentials, visual-match/contribution ยังเป็น stub, auctions/transactions ควรย้ายไปบริการอื่นใน swibs |

---

## 2. Architecture Alignment กับ swibs Services

หลังจากตรวจสอบ services ที่มีอยู่ใน `swibs` แล้ว ขอแนะนำให้แบ่งขอบเขตความรับผิดชอบใหม่ดังนี้:

| Capability | เจ้าของที่เหมาะสมใน swibs | เหตุผล |
|------------|---------------------------|--------|
| Card scanning / identification / quality / watermark | **`sws-scanner-service`** | ตรงกับ `scanner.v1.ScannerService` proto |
| Verified card/sample database (community contributions) | **`sws-scanner-service`** หรือ **`sws-svc-vault-inventory`** | ข้อมูล verified card เป็นส่วนหนึ่งของ scanner domain แต่สามารถ sync เป็น `items` ใน vault inventory ได้ |
| eBay/external price discovery | **`sws-svc-swap-marketdata`** | Marketdata มี schema `trades`/`candlesticks`/`market_stats` อยู่แล้ว |
| Fixed-price listings / auctions / bids | **`sws-svc-swap-listing`** (หรือ service ใหม่ `swap-auction`) | Listing service มี `listings` table + KYC gate + events อยู่แล้ว |
| Orders / transactions / fee calculation | **`sws-svc-swap-order`** + **`sws-svc-swap-payment`** | Order service มี saga + state machine อยู่แล้ว |
| User tier / KYC status | **`sws-svc-user`** + **`sws-svc-vault-kyc`** | มี user tier, kyc_status, gRPC KYC service อยู่แล้ว |
| Vault item ownership / lock / transfer | **`sws-svc-vault-inventory`** | Inventory gRPC มี `LockItem`, `TransferOwner`, etc. |

**ผลกระทบต่อ scanner-service:**
- ควรลบ/deprecate routes `/v1/auctions`, `/v1/transactions` ออกจาก scanner-service ในระยะต่อไป
- `/v1/prices` อาจเก็บไว้เป็น compatibility bridge ชั่วคราว แต่ควร feed ข้อมูล sold ไปยัง `swap-marketdata` ผ่าน `RecordTrade`
- `/v1/visual-match` และ `/v1/contribute*` ยังคงเป็น scanner domain แต่ต้อง port ให้เสร็จ

---

## 3. Gap Analysis

### 3.1 API Routes: ต้นฉบับ vs Go service

| Vercel route (`/api/*`) | Go route (`/v1/*`) | สถานะ | หมายเหตุ |
|-------------------------|---------------------|-------|-----------|
| `GET/POST /auctions` | `GET/POST /auctions` | ⚠️ จะย้ายไป swap-listing/auction | ไม่ควรอยู่ใน scanner-service |
| `POST /contribute` | `POST /contribute` | ❌ stub 501 | ยังไม่ port |
| `POST /contribute-sample` | `POST /contribute-sample` | ❌ stub 501 | ยังไม่ port |
| `GET /cn-anniv-cards` | `GET /cn-anniv-cards` | ✅ มี | |
| `GET /don-cards` | `GET /don-cards` | ✅ มี | |
| `GET /fx` | `GET /fx` | ✅ มี | |
| `GET/POST /lookup-by-filename` | `GET /lookup-by-filename` | ✅ มี | |
| `GET /op-details` | `GET /op-details` | ✅ มี | |
| `GET /op-variants` | `GET /op-variants` | ✅ มี | |
| `GET /prices` | `GET /prices` | ✅ มี | port แล้ว แต่ต้องใช้ eBay credentials |
| `GET /proxy-image` | `GET /proxy-image` | ✅ มี | |
| `POST /quality` | `POST /quality` | ✅ มี | |
| `POST /scan` | `POST /scan` | ✅ มี | |
| `POST /scan-phash` | `POST /scan-phash` | ✅ มี | |
| `GET/POST /transactions` | `GET/POST /transactions` | ✅ fee preview แล้ว | ส่วน write ควรย้ายไป swap-order |
| `POST /visual-match` | `POST /visual-match` | ❌ stub 501 | ยังไม่ port |
| `POST /watermark` | `POST /watermark` | ✅ มี | |
| `GET /whoami` | `GET /whoami` | ✅ มี | |

### 3.2 Environment Variables

| ต้นฉบับ (`sws-scanner-app`) | ใน Go service | สถานะ |
|-----------------------------|---------------|-------|
| `REACT_APP_API_BASE_URL` | — (frontend only) | ✅ |
| `REACT_APP_FIREBASE_*` | — (frontend only) | ✅ |
| `FIREBASE_SERVICE_ACCOUNT_B64` | `FIREBASE_SERVICE_ACCOUNT_B64` | ✅ |
| `FIREBASE_STORAGE_BUCKET` | `FIREBASE_STORAGE_BUCKET` | ✅ |
| `ANTHROPIC_API_KEY` | `ANTHROPIC_API_KEY` | ✅ |
| `GOOGLE_VISION_API_KEY` | `GOOGLE_VISION_API_KEY` | ✅ |
| `EBAY_APP_ID` | `EBAY_APP_ID` | ✅ ใช้แล้ว |
| `EBAY_CERT_ID` | `EBAY_CERT_ID` | ✅ ใช้แล้ว |
| `ADMIN_EMAILS` | `ADMIN_EMAILS` | ⚠️ config มี แต่ code ไม่ใช้ |
| `AUCTION_REQUIRE_KYC` | `AUCTION_REQUIRE_KYC` | ⚠️ จะลบ/ย้ายไป listing service |
| `CRON_SECRET` | `CRON_SECRET` | ⚠️ จะลบ/ย้ายไป listing service |
| `CORS_ALLOWED_ORIGINS` | `CORS_ALLOWED_ORIGINS` | ✅ middleware แล้ว |
| — | `DATABASE_URL` | ✅ |
| — | `GRPC_PORT` | ✅ |
| — | `NATS_URL` | ⚠️ client สร้าง แต่ยังไม่ publish event |

### 3.3 Platform Integration (swibs)

| ส่วน | สถานะ |
|------|-------|
| Proto contract (`sws-shared-protos/proto/scanner/v1/scanner.proto`) | ✅ มี |
| Generated Go/TS code | ✅ สร้างแล้ว |
| gRPC server ใน scanner-service | ✅ รันแล้ว (`:9090`) |
| API gateway upstream/route | ✅ เพิ่ม `scanner-service` upstream + routes แล้ว |
| Shared events สำหรับ scanner | ❌ ยังไม่มี |
| K8s namespace/deployment/ArgoCD | ❌ ยังไม่มี |
| Service อื่นเรียก scanner | ❌ ยังไม่มี |

---

## 4. Migration Plan (อัปเดต)

### Phase 0 — Foundation ✅ (เสร็จแล้ว)
- [x] เพิ่ม `.env.example` ใน `sws-scanner-service`
- [x] เพิ่ม CORS middleware ให้เคารพ `CORS_ALLOWED_ORIGINS`
- [x] เพิ่ม request body size limit 12 MB
- [x] Fee engine port + `preview-fees` / `preview-chain`
- [x] Pricing engine port (eBay Finding/Browse/HTML scrape + filters + tier grouping + Mercari links)

### Phase 1 — Scanner Core Parity
- [x] **Visual match**: port `api/visual-match.js` → `internal/usecase/visualmatch/` + `/v1/visual-match`
- [x] **Contributions**: port `api/contribute.js` + `api/contribute-sample.js` → `internal/usecase/contributions/`
- [ ] **Scan skill parity**: ย้าย prompt logic จาก `skills/op-scan-skill.js` / `ygo-scan-skill.js` ให้ใกล้เคียงต้นฉบับ
- [x] **Admin / KYC env ใน scanner**: wire `ADMIN_EMAILS` สำหรับ contribution admin replace (KYC หลักให้ swap-listing/user จัดการ)

### Phase 2 — Deprecate Marketplace Logic ออกจาก Scanner
- [ ] ย้าย `/v1/auctions` ไป `sws-svc-swap-listing` (หรือ service ใหม่)
- [ ] ย้าย `/v1/transactions` write path ไป `sws-svc-swap-order`
- [ ] เก็บ fee preview ไว้ใน order service แทน scanner
- [ ] สร้าง POST `/v1/market/:sku/trades` ใน `swap-marketdata` และให้ scanner/pricing feed sold records ไปที่นั่น

### Phase 3 — Platform Integration
- [x] Generate `scanner.v1` Go/TS code (`buf generate`)
- [x] Implement gRPC server ใน scanner-service (`Scan`, `GetVerifiedCard`, `LookupByPHash`)
- [x] เพิ่ม `SCANNER_SERVICE_URL` และ routes ใน `sws-svc-api-gateway`
- [ ] สร้าง scanner event schemas ใน `sws-shared-events/schemas/` (เช่น `scan.completed`, `card.verified`)
- [ ] Publish events ผ่าน NATS จาก scanner-service
- [ ] สร้าง Helm values / ArgoCD Application / K8s namespace สำหรับ scanner-service
- [ ] ให้ `sws-svc-swap-listing` และ `sws-svc-vault-inventory` เรียก scanner gRPC เมื่อต้องการ verify card image

### Phase 4 — Frontend Cutover
- [ ] อัปเดต `REACT_APP_API_BASE_URL` ให้ชี้ gateway (`/api/v1`) แทน scanner service ตรง ๆ
- [ ] แยก calls: scan → scanner-service, listing/auction → swap-listing, order/fee → swap-order, price history → swap-marketdata
- [ ] ทดสอบ feature สำคัญบน staging
- [ ] ลบ Vercel API functions ที่ย้ายเสร็จแล้ว

---

## 5. ความเสี่ยงและข้อควรระวัง

| ความเสี่ยง | แนวทางรับมือ |
|------------|--------------|
| eBay API rate limit / HTML scraping เปลี่ยน | ใช้ tiered cache + circuit breaker; feed ข้อมูลไป marketdata |
| Marketplace logic ซ้ำซ้อนใน scanner-service | Deprecate ทีละ route และ redirect ไป service ที่เหมาะสม |
| Firebase Auth token ข้าม service | ใช้ gateway เป็น token verifier, ส่ง `X-User-ID` ต่อ |
| Firestore vs Postgres data drift | เลือก canonical store ชัดเจน และทำ sync job |
| ไม่มี automated tests | เพิ่ม tests ก่อน Phase 1 ให้เสร็จ |
| gRPC server ไม่มี TLS/mTLS ใน production | เพิ่ม credentials/interceptors ก่อน deploy |

---

## 6. Definition of Done

- [ ] ทุก scanner-core route (scan, quality, watermark, visual-match, contribute) ทำงานได้เหมือน Vercel ≥ 95%
- [ ] `/v1/prices` คืนข้อมูล sold/active จริงเมื่อมี eBay credentials
- [ ] Auctions/transactions ถูกย้ายออกจาก scanner-service ไปยัง swap services
- [ ] API gateway ส่งต่อ scanner routes ได้
- [ ] gRPC server ตอบสนอง `Scan`, `GetVerifiedCard`, `LookupByPHash`
- [ ] มี K8s deployment + ArgoCD app สำหรับ scanner-service
- [ ] Frontend ใช้งาน scanner ผ่าน platform ได้โดยไม่มี regression สำคัญ
- [ ] Rollback procedure ทดสอบแล้ว
