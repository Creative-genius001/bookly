package models

import (
	"time"

	"github.com/google/uuid"
)

// ShopBankAccount is the payout destination for a shop (one per shop).
type ShopBankAccount struct {
	BaseModel
	ShopID        uuid.UUID `json:"shop_id" gorm:"type:uuid;uniqueIndex;not null"`
	BankCode      string    `json:"bank_code" gorm:"type:varchar(20);not null"`
	BankName      string    `json:"bank_name" gorm:"type:varchar(120);not null"`
	AccountNumber string    `json:"account_number" gorm:"type:varchar(20);not null"`
	AccountName   string    `json:"account_name" gorm:"type:varchar(150);not null"`
	RecipientCode string    `json:"-" gorm:"type:varchar(120);not null"`
}

type WalletEntryType string

const (
	WalletCredit WalletEntryType = "credit"
	WalletDebit  WalletEntryType = "debit"
)

// WalletEntry is an append-only ledger line. Balance = Σcredit − Σdebit.
type WalletEntry struct {
	BaseModel
	ShopID       uuid.UUID       `json:"shop_id" gorm:"type:uuid;index;not null"`
	Type         WalletEntryType `json:"type" gorm:"type:varchar(10);not null"`
	AmountKobo   int64           `json:"amount_kobo" gorm:"not null"` // always positive
	Reason       string          `json:"reason" gorm:"type:varchar(160);not null"`
	BookingID    *uuid.UUID      `json:"booking_id" gorm:"type:uuid;index"`
	WithdrawalID *uuid.UUID      `json:"withdrawal_id" gorm:"type:uuid;index"`
	// Reference makes ledger writes idempotent (uniqueIndex with type).
	Reference string `json:"reference" gorm:"type:varchar(160);index:idx_wallet_ref_type,unique"`
	EntryKind string `json:"entry_kind" gorm:"type:varchar(40);index:idx_wallet_ref_type,unique"`
}

type WithdrawalStatus string

const (
	WithdrawalPending    WithdrawalStatus = "pending"
	WithdrawalProcessing WithdrawalStatus = "processing"
	WithdrawalPaid       WithdrawalStatus = "paid"
	WithdrawalFailed     WithdrawalStatus = "failed"
)

type WithdrawalRequest struct {
	BaseModel
	ShopID        uuid.UUID        `json:"shop_id" gorm:"type:uuid;index;not null"`
	AmountKobo    int64            `json:"amount_kobo" gorm:"not null"`
	Status        WithdrawalStatus `json:"status" gorm:"type:varchar(20);not null;index"`
	Reference     string           `json:"reference" gorm:"type:varchar(120);uniqueIndex;not null"`
	TransferCode  string           `json:"-" gorm:"type:varchar(120);index"`
	FailureReason string           `json:"failure_reason" gorm:"type:varchar(200)"`
	ProcessedAt   *time.Time       `json:"processed_at"`
}
