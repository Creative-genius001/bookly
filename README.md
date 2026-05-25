# Barber Booking Backend

Go + Gin backend for a payment-first, slot-based barbershop booking platform.

## What is included

- JWT auth with DB-backed refresh tokens and logout deletion.
- One shop per owner.
- Dynamic slot generation from business days, `barbing_duration`, open/close time, active days, and capacity per interval.
- Rolling 14-day booking window.
- Redis rate limiting and slot locks.
- Paystack initialization, webhook signature validation, and idempotent payment confirmation.
- Pluggable notification interfaces with a log email sender by default.

## Quick start

```powershell
cp .env.example .env
docker-compose up --build
```

Or run only the dependencies in Docker and start the API on your host:

```powershell
docker-compose up -d postgres redis
go run ./cmd/api
```

The API listens on `http://localhost:8080` by default.

## Important endpoints

- `POST /auth/signup`
- `POST /auth/login`
- `POST /auth/refresh`
- `POST /auth/logout`
- `POST /shops`
- `PATCH /shops/:id`
- `POST /shops/:id/business-days`
- `GET /shops/:slug/slots?date=YYYY-MM-DD`
- `POST /bookings/initiate`
- `POST /bookings/reschedule`
- `POST /bookings/cancel`
- `GET /bookings/:code`
- `POST /payments/init`
- `POST /payments/webhook`
