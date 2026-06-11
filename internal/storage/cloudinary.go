package storage

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"barber-booking-backend/internal/config"
	errorMap "barber-booking-backend/internal/utils/error"
)

type CloudinaryUploader struct {
	cfg    config.CloudinaryConfig
	client *http.Client
}

func NewCloudinaryUploader(cfg config.CloudinaryConfig) *CloudinaryUploader {
	return &CloudinaryUploader{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}
}

func (u *CloudinaryUploader) Enabled() bool { return u.cfg.Enabled() }

func (u *CloudinaryUploader) Upload(ctx context.Context, filename string, data []byte, contentType string) (string, error) {
	if !u.cfg.Enabled() {
		return "", fmt.Errorf("cloudinary is not configured")
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	folder := u.cfg.Folder
	if folder == "" {
		folder = "bookly"
	}

	toSign := fmt.Sprintf("folder=%s&timestamp=%s", folder, timestamp)
	sum := sha1.Sum([]byte(toSign + u.cfg.APISecret))
	signature := hex.EncodeToString(sum[:])

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	for k, v := range map[string]string{
		"api_key":   u.cfg.APIKey,
		"timestamp": timestamp,
		"folder":    folder,
		"signature": signature,
	} {
		if err := w.WriteField(k, v); err != nil {
			return "", err
		}
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/image/upload", u.cfg.CloudName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := u.client.Do(req)
	if err != nil {
		return "", errorMap.Wrap(err, errorMap.CodeInternal, "Cloudinary Service: Upload image", "could not upload image")
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("cloudinary upload failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var out struct {
		SecureURL string `json:"secure_url"`
		URL       string `json:"url"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", err
	}
	if out.SecureURL != "" {
		return out.SecureURL, nil
	}
	return out.URL, nil
}
