package paystack

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type Bank struct {
	Name string `json:"name"`
	Code string `json:"code"`
	Slug string `json:"slug"`
}

type ResolvedAccount struct {
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
}

type TransferInput struct {
	AmountKobo    int64
	RecipientCode string
	Reason        string
	Reference     string
}

type TransferResult struct {
	TransferCode string
	Status       string // otp | pending | success | failed | reversed
	Reference    string
}

// ListBanks returns the supported Nigerian banks (for the payout bank picker).
func (c *Client) ListBanks(ctx context.Context) ([]Bank, error) {
	var out struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
		Data    []Bank `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/bank?currency=NGN", nil, &out); err != nil {
		return nil, err
	}
	if !out.Status {
		return nil, fmt.Errorf("paystack list banks failed: %s", out.Message)
	}
	return out.Data, nil
}

// ResolveAccount verifies bank details and returns the account holder's name.
func (c *Client) ResolveAccount(ctx context.Context, accountNumber, bankCode string) (ResolvedAccount, error) {
	q := url.Values{}
	q.Set("account_number", accountNumber)
	q.Set("bank_code", bankCode)

	var out struct {
		Status  bool            `json:"status"`
		Message string          `json:"message"`
		Data    ResolvedAccount `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/bank/resolve?"+q.Encode(), nil, &out); err != nil {
		return ResolvedAccount{}, err
	}
	if !out.Status {
		return ResolvedAccount{}, fmt.Errorf("could not resolve account: %s", out.Message)
	}
	return out.Data, nil
}

// CreateTransferRecipient registers a payout destination and returns its code.
func (c *Client) CreateTransferRecipient(ctx context.Context, name, accountNumber, bankCode string) (string, error) {
	body := map[string]any{
		"type":           "nuban",
		"name":           name,
		"account_number": accountNumber,
		"bank_code":      bankCode,
		"currency":       "NGN",
	}
	var out struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
		Data    struct {
			RecipientCode string `json:"recipient_code"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/transferrecipient", body, &out); err != nil {
		return "", err
	}
	if !out.Status {
		return "", fmt.Errorf("could not create transfer recipient: %s", out.Message)
	}
	return out.Data.RecipientCode, nil
}

// InitiateTransfer sends money from the Paystack balance to a recipient.
func (c *Client) InitiateTransfer(ctx context.Context, in TransferInput) (TransferResult, error) {
	body := map[string]any{
		"source":    "balance",
		"amount":    in.AmountKobo,
		"recipient": in.RecipientCode,
		"reason":    in.Reason,
		"reference": in.Reference,
	}
	var out struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
		Data    struct {
			TransferCode string `json:"transfer_code"`
			Status       string `json:"status"`
			Reference    string `json:"reference"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/transfer", body, &out); err != nil {
		return TransferResult{}, err
	}
	if !out.Status {
		return TransferResult{}, fmt.Errorf("transfer failed: %s", out.Message)
	}
	return TransferResult{
		TransferCode: out.Data.TransferCode,
		Status:       out.Data.Status,
		Reference:    out.Data.Reference,
	}, nil
}
