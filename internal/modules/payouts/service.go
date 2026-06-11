package payouts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/paystack"
	"barber-booking-backend/internal/utils"
	errorMap "barber-booking-backend/internal/utils/error"
)

var (
	ErrShopNotFound    = errors.New("shop not found")
	ErrNoBankAccount   = errors.New("add a payout bank account first")
	ErrInvalidAmount   = errors.New("enter a valid amount")
	ErrInsufficient    = errors.New("amount exceeds your available balance")
	ErrPayoutsDisabled = errors.New("payouts are not configured")
)

type Service struct {
	db       *gorm.DB
	paystack *paystack.Client
	logger   *slog.Logger
}

func NewService(db *gorm.DB, paystackClient *paystack.Client, logger *slog.Logger) *Service {
	return &Service{db: db, paystack: paystackClient, logger: logger}
}

func (s *Service) ownedShop(ctx context.Context, ownerID, shopID uuid.UUID) (models.Shop, error) {
	var shop models.Shop
	err := s.db.WithContext(ctx).Where("id = ? AND owner_id = ?", shopID, ownerID).First(&shop).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Shop{}, errorMap.New(errorMap.CodeNotFound, "Payouts", ErrShopNotFound.Error())
	}
	if err != nil {
		return models.Shop{}, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts", "could not load shop")
	}
	return shop, nil
}

func (s *Service) ListBanks(ctx context.Context) ([]paystack.Bank, error) {
	banks, err := s.paystack.ListBanks(ctx)
	if err != nil {
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts: ListBanks", "could not load banks")
	}
	return banks, nil
}

// SaveBankAccount resolves the account, registers a Paystack transfer recipient
// and stores it as the shop's single payout destination.
func (s *Service) SaveBankAccount(ctx context.Context, ownerID, shopID uuid.UUID, bankCode, accountNumber string) (models.ShopBankAccount, error) {
	shop, err := s.ownedShop(ctx, ownerID, shopID)
	if err != nil {
		return models.ShopBankAccount{}, err
	}
	bankCode = strings.TrimSpace(bankCode)
	accountNumber = strings.TrimSpace(accountNumber)
	if bankCode == "" || accountNumber == "" {
		return models.ShopBankAccount{}, errorMap.New(errorMap.CodeInvalidInput, "Payouts", "bank and account number are required")
	}

	resolved, err := s.paystack.ResolveAccount(ctx, accountNumber, bankCode)
	if err != nil {
		return models.ShopBankAccount{}, errorMap.New(errorMap.CodeInvalidInput, "Payouts", "could not verify those bank details")
	}

	recipientCode, err := s.paystack.CreateTransferRecipient(ctx, resolved.AccountName, accountNumber, bankCode)
	if err != nil {
		return models.ShopBankAccount{}, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts", "could not register payout account")
	}

	bankName := s.bankNameFor(ctx, bankCode)
	account := models.ShopBankAccount{
		ShopID:        shop.ID,
		BankCode:      bankCode,
		BankName:      bankName,
		AccountNumber: accountNumber,
		AccountName:   resolved.AccountName,
		RecipientCode: recipientCode,
	}

	// One account per shop — replace any existing.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("shop_id = ?", shop.ID).Delete(&models.ShopBankAccount{}).Error; err != nil {
			return err
		}
		return tx.Create(&account).Error
	})
	if err != nil {
		return models.ShopBankAccount{}, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts", "could not save payout account")
	}
	return account, nil
}

func (s *Service) bankNameFor(ctx context.Context, code string) string {
	banks, err := s.paystack.ListBanks(ctx)
	if err != nil {
		return ""
	}
	for _, b := range banks {
		if b.Code == code {
			return b.Name
		}
	}
	return ""
}

func (s *Service) GetBankAccount(ctx context.Context, ownerID, shopID uuid.UUID) (*models.ShopBankAccount, error) {
	if _, err := s.ownedShop(ctx, ownerID, shopID); err != nil {
		return nil, err
	}
	var account models.ShopBankAccount
	err := s.db.WithContext(ctx).Where("shop_id = ?", shopID).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts", "could not load payout account")
	}
	return &account, nil
}

type WalletSummary struct {
	BalanceKobo int64  `json:"balance_kobo"`
	Currency    string `json:"currency"`
}

func balance(db *gorm.DB, ctx context.Context, shopID uuid.UUID) (int64, error) {
	var credit, debit int64
	if err := db.WithContext(ctx).Model(&models.WalletEntry{}).
		Where("shop_id = ? AND type = ?", shopID, models.WalletCredit).
		Select("COALESCE(SUM(amount_kobo), 0)").Scan(&credit).Error; err != nil {
		return 0, err
	}
	if err := db.WithContext(ctx).Model(&models.WalletEntry{}).
		Where("shop_id = ? AND type = ?", shopID, models.WalletDebit).
		Select("COALESCE(SUM(amount_kobo), 0)").Scan(&debit).Error; err != nil {
		return 0, err
	}
	return credit - debit, nil
}

func (s *Service) Wallet(ctx context.Context, ownerID, shopID uuid.UUID) (WalletSummary, error) {
	if _, err := s.ownedShop(ctx, ownerID, shopID); err != nil {
		return WalletSummary{}, err
	}
	bal, err := balance(s.db, ctx, shopID)
	if err != nil {
		return WalletSummary{}, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts", "could not load wallet")
	}
	return WalletSummary{BalanceKobo: bal, Currency: "NGN"}, nil
}

type EntriesResult struct {
	Entries  []models.WalletEntry `json:"entries"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

func (s *Service) WalletEntries(ctx context.Context, ownerID, shopID uuid.UUID, page, pageSize int) (EntriesResult, error) {
	if _, err := s.ownedShop(ctx, ownerID, shopID); err != nil {
		return EntriesResult{}, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	q := s.db.WithContext(ctx).Model(&models.WalletEntry{}).Where("shop_id = ?", shopID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return EntriesResult{}, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts", "could not load ledger")
	}
	var entries []models.WalletEntry
	if err := q.Order("created_at DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&entries).Error; err != nil {
		return EntriesResult{}, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts", "could not load ledger")
	}
	return EntriesResult{Entries: entries, Total: total, Page: page, PageSize: pageSize}, nil
}

// RequestWithdrawal reserves funds (a debit), records the request and initiates a
// Paystack transfer to the shop's bank account.
func (s *Service) RequestWithdrawal(ctx context.Context, ownerID, shopID uuid.UUID, amountKobo int64) (models.WithdrawalRequest, error) {
	if _, err := s.ownedShop(ctx, ownerID, shopID); err != nil {
		return models.WithdrawalRequest{}, err
	}
	if amountKobo <= 0 {
		return models.WithdrawalRequest{}, errorMap.New(errorMap.CodeInvalidInput, "Payouts", ErrInvalidAmount.Error())
	}

	var account models.ShopBankAccount
	if err := s.db.WithContext(ctx).Where("shop_id = ?", shopID).First(&account).Error; err != nil {
		return models.WithdrawalRequest{}, errorMap.New(errorMap.CodeInvalidInput, "Payouts", ErrNoBankAccount.Error())
	}

	reference, err := withdrawalReference()
	if err != nil {
		return models.WithdrawalRequest{}, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts", "could not start withdrawal")
	}

	request, err := s.reserveWithdrawal(ctx, shopID, amountKobo, reference)
	if err != nil {
		return models.WithdrawalRequest{}, err
	}

	// Initiate the transfer. On failure, reverse the reservation.
	transfer, err := s.paystack.InitiateTransfer(ctx, paystack.TransferInput{
		AmountKobo:    amountKobo,
		RecipientCode: account.RecipientCode,
		Reason:        "Bookly payout " + reference,
		Reference:     reference,
	})
	if err != nil {
		s.reverseWithdrawal(ctx, request, "transfer could not be started")
		return models.WithdrawalRequest{}, errorMap.New(errorMap.CodeInternal, "Payouts", "could not start the transfer")
	}

	updates := map[string]interface{}{"transfer_code": transfer.TransferCode}
	if transfer.Status == "success" {
		now := time.Now().UTC()
		updates["status"] = models.WithdrawalPaid
		updates["processed_at"] = &now
		request.Status = models.WithdrawalPaid
	}
	_ = s.db.WithContext(ctx).Model(&models.WithdrawalRequest{}).Where("id = ?", request.ID).Updates(updates).Error
	request.TransferCode = transfer.TransferCode
	return request, nil
}

// reserveWithdrawal is the RESERVE phase of the reserve → external → settle saga.
// It pessimistically locks the shop's wallet row (FOR UPDATE) so concurrent
// balance-decreasing ops serialize, checks the balance and writes the debit in a
// single short transaction. The lock is released on commit — it is never held
// across the external transfer call. Exported-for-tests via the service.
func (s *Service) reserveWithdrawal(ctx context.Context, shopID uuid.UUID, amountKobo int64, reference string) (models.WithdrawalRequest, error) {
	var request models.WithdrawalRequest
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockRow models.Shop
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").First(&lockRow, "id = ?", shopID).Error; err != nil {
			return err
		}
		bal, err := balance(tx, ctx, shopID)
		if err != nil {
			return err
		}
		if amountKobo > bal {
			return errorMap.New(errorMap.CodeInvalidInput, "Payouts", ErrInsufficient.Error())
		}
		request = models.WithdrawalRequest{
			ShopID:     shopID,
			AmountKobo: amountKobo,
			Status:     models.WithdrawalProcessing,
			Reference:  reference,
		}
		if err := tx.Create(&request).Error; err != nil {
			return err
		}
		debit := models.WalletEntry{
			ShopID:       shopID,
			Type:         models.WalletDebit,
			AmountKobo:   amountKobo,
			Reason:       "Withdrawal " + reference,
			WithdrawalID: &request.ID,
			Reference:    reference,
			EntryKind:    "withdrawal",
		}
		return tx.Create(&debit).Error
	})
	if err != nil {
		var appErr *errorMap.AppError
		if errors.As(err, &appErr) {
			return models.WithdrawalRequest{}, appErr
		}
		return models.WithdrawalRequest{}, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts", "could not reserve funds")
	}
	return request, nil
}

func (s *Service) ListWithdrawals(ctx context.Context, ownerID, shopID uuid.UUID, page, pageSize int) ([]models.WithdrawalRequest, int64, error) {
	if _, err := s.ownedShop(ctx, ownerID, shopID); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	q := s.db.WithContext(ctx).Model(&models.WithdrawalRequest{}).Where("shop_id = ?", shopID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts", "could not load withdrawals")
	}
	var rows []models.WithdrawalRequest
	if err := q.Order("created_at DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		return nil, 0, errorMap.Wrap(err, errorMap.CodeInternal, "Payouts", "could not load withdrawals")
	}
	return rows, total, nil
}

// reverseWithdrawal marks a withdrawal failed and credits the reserved funds
// back to the wallet (idempotent on reference+kind).
func (s *Service) reverseWithdrawal(ctx context.Context, request models.WithdrawalRequest, reason string) {
	now := time.Now().UTC()
	_ = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.WithdrawalRequest{}).Where("id = ?", request.ID).
			Updates(map[string]interface{}{
				"status":         models.WithdrawalFailed,
				"failure_reason": reason,
				"processed_at":   &now,
			}).Error; err != nil {
			return err
		}
		reversal := models.WalletEntry{
			ShopID:       request.ShopID,
			Type:         models.WalletCredit,
			AmountKobo:   request.AmountKobo,
			Reason:       "Reversed withdrawal " + request.Reference,
			WithdrawalID: &request.ID,
			Reference:    request.Reference,
			EntryKind:    "withdrawal_reversal",
		}
		// Ignore duplicate reversal (already credited back).
		return tx.Where("reference = ? AND entry_kind = ?", reversal.Reference, reversal.EntryKind).
			FirstOrCreate(&reversal).Error
	})
}

func withdrawalReference() (string, error) {
	r, err := utils.RandomString(10, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("WD-%s", r), nil
}
