package payouts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"barber-booking-backend/internal/config"
	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/paystack"
	errorMap "barber-booking-backend/internal/utils/error"
)

// TestWithdrawalConcurrency proves the RESERVE phase's pessimistic lock prevents
// double-spend: with a fixed balance, many concurrent reserves must collectively
// withdraw no more than the balance. Requires a Postgres DSN in TEST_DATABASE_URL
// (run via scripts/verify-wallet-concurrency.sh); skipped otherwise.
func TestWithdrawalConcurrency(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run the wallet concurrency test")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Shop{}, &models.WalletEntry{}, &models.WithdrawalRequest{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()

	owner := models.User{
		Email:        fmt.Sprintf("owner+%s@test.local", uuid.NewString()),
		Phone:        "08000000000",
		PasswordHash: "x",
		Role:         models.RoleOwner,
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	shop := models.Shop{
		OwnerID:                owner.ID,
		Name:                   "Concurrency Test Shop",
		Slug:                   "ct-" + uuid.NewString()[:8],
		Email:                  "shop@test.local",
		Phone:                  "08000000000",
		Timezone:               "UTC",
		IsActive:               true,
		CapacityPerSlot:        1,
		BarbingDurationMinutes: 30,
	}
	if err := db.Create(&shop).Error; err != nil {
		t.Fatalf("create shop: %v", err)
	}

	// Seed a balance with one credit.
	const initial int64 = 100000
	seed := models.WalletEntry{
		ShopID: shop.ID, Type: models.WalletCredit, AmountKobo: initial,
		Reason: "seed", Reference: "seed-" + uuid.NewString(), EntryKind: "seed",
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed credit: %v", err)
	}

	svc := NewService(db, paystack.NewClient(config.PaystackConfig{}),
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Fire many concurrent reserves of 30,000 against a 100,000 balance.
	// Exactly floor(100000/30000) = 3 may succeed; the rest must be rejected.
	const (
		workers       = 24
		amount  int64 = 30000
	)
	var success, insufficient int64
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ref := fmt.Sprintf("WD-T-%d-%s", i, uuid.NewString()[:8])
			if _, err := svc.reserveWithdrawal(ctx, shop.ID, amount, ref); err == nil {
				atomic.AddInt64(&success, 1)
				return
			} else {
				var appErr *errorMap.AppError
				if errors.As(err, &appErr) && appErr.Code == errorMap.CodeInvalidInput {
					atomic.AddInt64(&insufficient, 1)
					return
				}
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	expected := initial / amount
	if success != expected {
		t.Fatalf("double-spend: expected %d successful reserves, got %d (insufficient=%d)",
			expected, success, insufficient)
	}

	final, err := balance(db, ctx, shop.ID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if want := initial - success*amount; final != want {
		t.Fatalf("balance = %d, want %d", final, want)
	}
	if final < 0 {
		t.Fatalf("balance went negative: %d", final)
	}
	t.Logf("OK: %d/%d concurrent reserves succeeded, final balance %d kobo — no double-spend",
		success, workers, final)
}
