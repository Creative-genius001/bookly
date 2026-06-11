# Bookly — Epics & Design Doc

Design notes for the next wave of work, to be reviewed **before** implementation.
Each epic lists scope, schema/API changes, frontend impact, risks, and a rough
sequencing. Status legend: 🔴 not started · 🟡 in progress · 🟢 done.

Already shipped (for context): payment webhook confirmation, `GET /bookings/:code`,
`GET /shops/mine`, owner bookings list (search + pagination), analytics, password
reset, SMTP email, perf pass (DB pool, caching, bundle), PWA + icons.

---

## Epic 1 — Multi-shop per owner + Maps / "nearest shop" 🔴

**Goal:** one owner can run several shops in different locations; customers
discover and pick the closest/preferred one.

### Backend
- **Schema:** drop the `owner_id` **unique** index on `shops` (currently one
  shop per owner). Add `latitude DOUBLE PRECISION`, `longitude DOUBLE PRECISION`,
  `address VARCHAR`, and (for images, Epic 4) `logo_url`, `cover_image_url`.
- Remove the one-shop guard in `CreateShop`; keep slug uniqueness.
- `GET /shops/mine` already returns a **list** — no change needed (forward-compatible).
- **New public discovery endpoint:** `GET /shops?lat=&lng=&radius=&search=&page=&page_size=`
  - Distance via Postgres. Option A (no extension): haversine in raw SQL,
    `ORDER BY distance`. Option B: enable `cube` + `earthdistance` and use
    `earth_distance`. Option C: PostGIS `geography` + `ST_DWithin` (most robust,
    heaviest dependency). **Recommendation:** start with haversine raw SQL (zero
    infra), migrate to PostGIS only if geo queries grow.
  - Returns shop cards (name, slug, address, lat/lng, logo, distance_km).
- Add index on `shops(latitude, longitude)` (or a PostGIS GiST index).

### Frontend
- Dashboard **shop switcher** (the active-shop store already exists; make it a
  list + selector in the topbar).
- Onboarding: allow creating additional shops from the dashboard.
- **Customer discovery page** `/shops` (or `/discover`): map + list, "use my
  location" (browser Geolocation API), distance sort, search. Pins link to
  `/book/[slug]`.
- Maps provider (see below).

### Maps provider decision
| Option | Cost | Notes |
| --- | --- | --- |
| **Leaflet + OpenStreetMap** (+ Nominatim geocode, OSRM routing) | Free | No key; community tiles; self-host for scale. |
| **Mapbox** (recommended) | Generous free tier | Great React SDK + autocomplete. |
| **Google Maps Platform** | Paid (billing) | Best data/autocomplete. |

Needs: address **autocomplete + geocode** on shop setup (store lat/lng), a map
component on discovery, and a "directions" deep-link.

### Risks
- Dropping the unique index is a one-way migration — back up first.
- Geo queries without an index get slow fast; add the index in the same migration.

---

## Epic 2 — Payouts / Withdrawals 🟢 DONE

Implemented with a **platform-balance + transfer** model (no subaccounts, so the
charge flow is untouched):
- **Models**: `shop_bank_accounts` (one per shop, with Paystack `recipient_code`),
  `wallet_entries` (append-only ledger; balance = Σcredit − Σdebit; idempotent on
  `reference+entry_kind`), `withdrawal_requests`.
- **Ledger**: a **credit** (amount − `PLATFORM_FEE_PERCENT`) is written inside the
  payment-success transaction; a **debit** reverses it when a paid booking is
  cancelled/refunded.
- **Paystack**: added `ListBanks`, `ResolveAccount`, `CreateTransferRecipient`,
  `InitiateTransfer`; the webhook now also dispatches `transfer.*` events.
- **Withdrawals — reserve → external → settle saga**: RESERVE locks the shop's
  wallet row `FOR UPDATE` (`clause.Locking{Strength:"UPDATE"}`), checks the
  balance and writes the debit in one short tx (lock released on commit) →
  EXTERNAL calls the Paystack Transfer with **no DB lock held** across the
  network → SETTLE: `transfer.success` marks it paid; `transfer.failed/reversed`
  runs a compensating credit and marks it failed. The same shop-row lock guards
  the refund debit (the other balance-decreasing op), with a consistent
  "shop row first" lock order to avoid deadlocks. Credits (which only add funds)
  stay lock-free.
- **Endpoints** (owner): `GET /payouts/banks`, `POST/GET /shops/:id/bank-account`,
  `GET /shops/:id/wallet`, `GET /shops/:id/wallet/entries`,
  `POST/GET /shops/:id/withdrawals`.
- **Frontend**: Settings → **Payouts** (balance card + withdraw dialog, bank
  connect/verify form, withdrawal history) + typed api/hooks.
- *Verified: BE build+vet; FE typecheck + 55 unit + build + 1 new e2e.*
- ⚠️ **Runtime needs Paystack test keys** (banks/resolve/transfer) + **Transfers
  enabled** on the Paystack account; the ledger credit/debit + balance + the
  "reject withdrawal without a bank account / over balance" paths are DB-only.

### Original design notes (Epic 2) 🟢

**Goal:** each shop receives its own earnings and can withdraw them.

### Backend
- **Paystack model:** create a **subaccount** (or transfer recipient) per shop;
  capture bank details + run Paystack account resolution (a light KYC).
- **Split at charge time:** pass `subaccount` (+ `bearer`, `transaction_charge`)
  to `transaction/initialize` so the platform takes a fee and the shop gets the rest.
- **Ledger:** `wallet_entries` (shop_id, type credit|debit, amount_kobo, ref,
  booking_id, created_at) + a derived/maintained `balance`. Credit on confirmed
  payment (in the webhook), debit on withdrawal/refund.
- **Withdrawals:** `withdrawal_requests` (shop_id, amount_kobo, status
  pending|processing|paid|failed, bank ref). Use Paystack **Transfers** to pay
  out; reconcile via transfer webhooks.
- **New endpoints:** `POST /shops/:id/bank-account`, `GET /shops/:id/wallet`,
  `GET /shops/:id/wallet/entries`, `POST /shops/:id/withdrawals`,
  `GET /shops/:id/withdrawals`. Extend the Paystack webhook for `transfer.*`.

### Frontend
- Settings → **Payouts**: connect bank account, show balance + ledger, request a
  withdrawal, withdrawal history.

### Risks
- Real money movement — needs idempotency, reconciliation, and careful testing in
  Paystack test mode. Bank-detail capture has compliance implications.
- Decide the **fee model** (flat vs %) up front.

---

## Epic 3 — Cancel & Reschedule 🟢 DONE

Implemented against the current overlap-based model (no Slot table):
- **Public** `POST /bookings/cancel` (`{code}`) and `POST /bookings/reschedule`
  (`{code, start_time}`) — identified by booking code, no account (the stale
  commented versions guarded by `RoleCustomer` were replaced).
- **Cancel**: 1-hour lead-time guard; sets status `cancelled` (which frees the
  slot, since availability counts only confirmed/pending); refunds a successful
  payment via Paystack + marks it `refunded`; emails the customer.
- **Reschedule**: confirmed-only, 1-hour lead-time guard; validates the new slot
  via the Redis lock + overlap capacity check (excluding the booking itself);
  moves `starts_at/ends_at`; emails the customer.
- **Frontend**: cancel/reschedule API + hooks; owner booking drawer gains Cancel
  (confirm) + Reschedule (calendar + live availability) actions; new **customer
  self-service** page `/book/[slug]/manage?code=` (linked from the payment
  callback) for cancel + reschedule.
- *Verified: BE build+vet; FE typecheck + 55 unit + build + 2 new e2e.*

### Original design notes (Epic 3) 🟢

**Goal:** customers/owners can cancel (with refund) and reschedule bookings.

### Backend
- **Schema:** add `cancelled_at TIMESTAMP NULL` to `bookings` (and optionally a
  `slot_id` link if slot-level booked-count accounting is introduced; today
  availability is computed from overlapping bookings, not a counter).
- **Cancel:** guard window (e.g. ≥1h before `starts_at`), set status `cancelled`,
  refund via Paystack (`POST /refund`), mark payment `refunded`, credit/debit the
  wallet (Epic 2), notify. Endpoint: `POST /bookings/cancel` (`{ code }`, public
  or owner).
- **Reschedule:** validate the new slot is available + inside the 14-day window,
  move `starts_at`/`ends_at`, keep the same payment. Endpoint:
  `POST /bookings/reschedule` (`{ code, start_time }`).
- The commented `Cancel`/`Reschedule` in `bookings/service.go` reference an older
  model (slot_id, customer_id) — rewrite against the current schema.

### Frontend
- Booking detail drawer: enable **Cancel** / **Mark no-show** / **Reschedule**
  (the drawer already notes these are pending).
- Customer self-service: a "manage booking" view keyed by code on `/payment/callback`
  or a `/booking/[code]` page.

### Risks
- Refund + availability race conditions — reuse the existing Redis slot lock.

---

## Epic 4 — Images & storage (logos, covers, service photos) 🔴

**Goal:** shops have logos/cover images; services can have photos.

### Backend
- **Schema:** `shops.logo_url`, `shops.cover_image_url`, optional
  `services.image_url`.
- **Storage decision:**
  | Option | Notes |
  | --- | --- |
  | **External URL field** | Simplest — owner pastes a hosted URL. No upload infra. |
  | **Object storage (S3/Cloudinary/Supabase)** (recommended) | `POST /uploads` returns a signed URL or proxies the upload; store the public URL. Scales, CDN-backed. |
  | **Local disk + static serve** | Easy for dev, not for multi-instance prod. |
- **New endpoint (if uploading):** `POST /uploads` (multipart) → `{ url }`, with
  size/type validation.

### Frontend
- Re-enable `next/image` for shop logo/cover + service photos (it was reverted
  during the perf pass precisely because no image URLs existed yet).
- Upload widgets on shop setup / settings / service form.
- Discovery cards (Epic 1) and `ShopHero` render the cover + logo.

### Status note
PWA app icons, favicon and the brand mark are **already done** (`src/app/icon.svg`,
generated `icon-192/512`, maskable, apple-icon, `manifest.webmanifest`, service
worker + offline page). This epic is specifically about **user-supplied** images.

---

## Cross-cutting / smaller follow-ups
- **Email verification** 🟢 DONE. `users.email_verified` + an
  `email_verification_tokens` table (24h TTL); a token is issued + emailed on
  signup (best-effort, off the signup tx). Endpoints: `POST /auth/verify-email`
  (`{token}`) and `POST /auth/resend-verification` (`{email}`, no enumeration).
  Login is **not** hard-gated (non-breaking); the auth response now carries
  `email_verified`. FE: signup → `/verify-email` (check-inbox + resend), the email
  link auto-verifies, and an unverified owner sees a dismissible dashboard banner.
  *Verified: BE build+vet; FE typecheck + 55 unit + build + 2 new e2e.*
- **"Booking for someone else"** — optional separate booker email so payer + attendee
  both get notified (functionally already possible; make it first-class).
- **Search + pagination** on services/blocked-dates lists (bookings already done).
- **Redis-cache analytics** (currently browser-cached 60s via `Cache-Control`).
- **Real email templates** (HTML) once SMTP is in use.

## Locked decisions (2026-06-08)
- **Maps provider:** Mapbox (public token via `NEXT_PUBLIC_MAPBOX_TOKEN`).
- **Image storage:** **Cloudinary** (prioritised) via a backend `POST /uploads`;
  store the returned secure URL on the shop. (Interface kept pluggable so
  S3/Supabase can be added later.)
- **Geo queries:** PostGIS (`CREATE EXTENSION postgis`; `ST_DistanceSphere` on
  lat/lng; optional `geography` column + GiST index as a later optimisation).

> Runtime: PostGIS + the Mapbox token + Cloudinary credentials must be present in
> the deploy/dev environment. **The DB-backed pieces (multi-shop migration +
> PostGIS discovery) are now verifiable with `./scripts/verify-geo.sh`**, which
> spins up PostGIS + Redis, runs the API, and asserts multi-shop + distance
> sorting against a real database. Map/upload behaviour still needs the
> respective secrets.

### Build order (this batch)
1. **Shop schema + multi-shop** — ✅ done. Added `address`, `latitude`,
   `longitude`, `logo_url`, `cover_image_url`; `owner_id` index is now non-unique;
   one-shop guard removed; migration drops the legacy unique index + enables
   PostGIS. *Verified: `go build` + `go vet`.*
2. **Discovery endpoint** `GET /shops?lat=&lng=&radius=&search=&page=&page_size=`
   — ✅ done (PostGIS `ST_DistanceSphere`, distance-sorted, paginated). *Verified:
   compile/vet only — needs a PostGIS-enabled DB to exercise at runtime.*
3. **FE contract sync** — ✅ done. Shop type + payloads + `shopsApi.discover` +
   `useDiscovery` + MSW. *Verified: typecheck + 55 unit tests.*
4. **FE shop switcher** + create-another-shop — ✅ done (topbar dropdown).
5. **FE discovery page (Mapbox)** + address geocoding — ✅ done. `/discover`
   (map + list + "use my location" + search), `AddressAutocomplete` on the shop
   setup form (Mapbox geocoding → address + lat/lng). Degrades to list-only
   without `NEXT_PUBLIC_MAPBOX_TOKEN`. *Verified: typecheck + build + 2 new e2e.*
6. **Image uploads (Cloudinary)** — ✅ done. `POST /uploads` (owner, multipart,
   5MB/image-only) signs and forwards to Cloudinary (no SDK) → returns the
   secure URL; pluggable `storage.Uploader` interface. FE: `ImageUpload` widget
   on Settings (logo + cover), `next/image` re-enabled on the shop hero, logos on
   discovery cards. Degrades gracefully when `CLOUDINARY_*` is unset.
   *Verified: BE build+vet, FE typecheck+build+tests.*

**Batch 1 (multi-shop + maps + images) is complete.** Remaining epics: cancel/
reschedule (Epic 3), payouts (Epic 2), plus follow-ups (email verification,
"booking for someone else", broader search/pagination, Redis-cached analytics).

> Migration note: switching `owner_id` from unique→non-unique won't auto-drop the
> old index, so `AutoMigrate` now runs `DROP INDEX IF EXISTS idx_shops_owner_id`
> first (no-op on fresh DBs). PostGIS is enabled best-effort via
> `CREATE EXTENSION IF NOT EXISTS postgis` (needs a privileged DB role).

## Suggested sequence
1. **Epic 4 (images)** + **Epic 1 (multi-shop + maps)** together — they share the
   shop schema migration (lat/lng + image columns) and the discovery UI.
2. **Epic 3 (cancel/reschedule)** — small schema change, high customer value.
3. **Epic 2 (payouts)** — largest; do last, in Paystack test mode, with a clear fee model.
