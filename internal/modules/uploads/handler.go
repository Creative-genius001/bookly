package uploads

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"barber-booking-backend/internal/httpx"
	"barber-booking-backend/internal/storage"
	errorMap "barber-booking-backend/internal/utils/error"
)

const maxUploadBytes = 5 << 20 // 5 MiB

type Handler struct {
	uploader storage.Uploader
}

func NewHandler(uploader storage.Uploader) *Handler {
	return &Handler{uploader: uploader}
}

// Upload accepts a multipart "file" image and returns its public URL.
func (h *Handler) Upload(c *gin.Context) {
	if h.uploader == nil || !h.uploader.Enabled() {
		httpx.Error(c, http.StatusServiceUnavailable, "uploads are not configured")
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Upload Handler", "a file is required"))
		return
	}
	if fileHeader.Size > maxUploadBytes {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Upload Handler", "file too large (max 5MB)"))
		return
	}
	contentType := fileHeader.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Upload Handler", "only image uploads are allowed"))
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		httpx.InternalServerError(c, errorMap.New(errorMap.CodeInternal, "Upload Handler", "could not read file"))
		return
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxUploadBytes+1))
	if err != nil {
		httpx.InternalServerError(c, errorMap.New(errorMap.CodeInternal, "Upload Handler", "could not read file"))
		return
	}

	url, err := h.uploader.Upload(c.Request.Context(), fileHeader.Filename, data, contentType)
	if err != nil {
		httpx.InternalServerError(c, errorMap.New(errorMap.CodeInternal, "Upload Handler", "upload failed"))
		return
	}

	httpx.Created(c, gin.H{"url": url})
}
