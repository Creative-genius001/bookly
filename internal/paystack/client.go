package paystack

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"barber-booking-backend/internal/config"
)

type Client struct {
	cfg        config.PaystackConfig
	httpClient *http.Client
}

type Channel string

const (
	ChannelCard         Channel = "card"
	ChannelBankTransfer Channel = "bank_transfer"
)

type InitializeRequest struct {
	Email     string         `json:"email"`
	Amount    int64          `json:"amount"`
	Channels  []Channel      `json:"channels,omitempty"`
	Reference string         `json:"reference"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type InitializeResponse struct {
	AuthorizationURL string `json:"authorization_url"`
	AccessCode       string `json:"access_code"`
	Reference        string `json:"reference"`
}

type WebhookPayload struct {
	Event string `json:"event"`
	Data  struct {
		Reference string         `json:"reference"`
		Status    string         `json:"status"`
		Amount    int64          `json:"amount"`
		Metadata  map[string]any `json:"metadata"`
	} `json:"data"`
}

type TransferAccount struct {
	AccountName   string `json:"account_name"`
	AccountNumber string `json:"account_number"`
	BankName      string `json:"bank_name"`
}

// WebhookEvent is the full payload Paystack POSTs to your webhook endpoint.
type WebhookEvent struct {
	Event string      `json:"event"`
	Data  WebhookData `json:"data"`
}

type WebhookData struct {
	Reference  string          `json:"reference"`
	Status     string          `json:"status"` // success | failed | abandoned
	AmountKobo int64           `json:"amount"`
	Channel    string          `json:"channel"` // card | bank_transfer
	PaidAt     time.Time       `json:"paid_at"`
	Customer   WebhookCustomer `json:"customer"`
	// Only populated when channel=bank_transfer
	Authorization Authorization `json:"authorization"`
}

type Authorization struct {
	AccountName   string `json:"account_name"`
	AccountNumber string `json:"account_number"`
	BankName      string `json:"bank"`
	Channel       string `json:"channel"`
}

type WebhookCustomer struct {
	Email string `json:"email"`
}

type VerifyResult struct {
	Reference  string
	Status     string // success | failed | abandoned
	AmountKobo int64
	Channel    string
	PaidAt     time.Time
}

func NewClient(cfg config.PaystackConfig) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Client) InitializeTransaction(ctx context.Context, req InitializeRequest) (InitializeResponse, error) {
	if c.cfg.SecretKey == "" {
		return InitializeResponse{}, fmt.Errorf("PAYSTACK_SECRET_KEY is required")
	}

	body := map[string]interface{}{
		"email":        req.Email,
		"amount":       req.Amount,
		"reference":    req.Reference,
		"callback_url": c.cfg.CallbackURL,
		"channels":     req.Channels,
	}
	if len(req.Metadata) > 0 {
		body["metadata"] = req.Metadata
	}

	var out struct {
		Status  bool               `json:"status"`
		Message string             `json:"message"`
		Data    InitializeResponse `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/transaction/initialize", body, &out); err != nil {
		return InitializeResponse{}, err
	}
	if !out.Status {
		return InitializeResponse{}, fmt.Errorf("paystack initialize failed: %s", out.Message)
	}
	return out.Data, nil
}

func (c *Client) Refund(ctx context.Context, reference string) error {
	if c.cfg.SecretKey == "" {
		return fmt.Errorf("PAYSTACK_SECRET_KEY is required")
	}

	var out struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
	}
	body := map[string]string{"transaction": reference}
	if err := c.do(ctx, http.MethodPost, "/refund", body, &out); err != nil {
		return err
	}
	if !out.Status {
		return fmt.Errorf("paystack refund failed: %s", out.Message)
	}
	return nil
}

func (c *Client) Verify(ctx context.Context, reference string) (VerifyResult, error) {
	var resp struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
		Data    struct {
			Reference string    `json:"reference"`
			Status    string    `json:"status"`
			Amount    int64     `json:"amount"`
			Channel   string    `json:"channel"`
			PaidAt    time.Time `json:"paid_at"`
		} `json:"data"`
	}

	if err := c.do(ctx, http.MethodGet, "/transaction/verify/"+reference, nil, &resp); err != nil {
		return VerifyResult{}, fmt.Errorf("paystack verify: %w", err)
	}
	if !resp.Status {
		return VerifyResult{}, fmt.Errorf("paystack verify: %s", resp.Message)
	}

	return VerifyResult{
		Reference:  resp.Data.Reference,
		Status:     resp.Data.Status,
		AmountKobo: resp.Data.Amount,
		Channel:    resp.Data.Channel,
		PaidAt:     resp.Data.PaidAt,
	}, nil
}

func (c *Client) ParseWebhook(body []byte, signature string) (WebhookPayload, error) {
	if c.cfg.SecretKey == "" {
		return WebhookPayload{}, fmt.Errorf("PAYSTACK_SECRET_KEY is required")
	}
	if !c.ValidSignature(body, signature) {
		return WebhookPayload{}, fmt.Errorf("invalid paystack signature")
	}

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return WebhookPayload{}, err
	}
	return payload, nil
}

func (c *Client) ValidSignature(body []byte, signature string) bool {
	mac := hmac.New(sha512.New, []byte(c.cfg.SecretKey))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(signature)), []byte(expected))
}

func (c *Client) do(ctx context.Context, method, path string, input interface{}, output interface{}) error {
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}

	baseURL := strings.TrimRight(c.cfg.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.SecretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("paystack returned %d: %s", resp.StatusCode, string(respBody))
	}

	if output != nil {
		if err := json.Unmarshal(respBody, output); err != nil {
			return err
		}
	}
	return nil
}
