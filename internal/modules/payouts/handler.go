package payouts

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"barber-booking-backend/internal/httpx"
	"barber-booking-backend/internal/middleware"
	errorMap "barber-booking-backend/internal/utils/error"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type saveBankAccountRequest struct {
	BankCode      string `json:"bank_code" binding:"required"`
	AccountNumber string `json:"account_number" binding:"required"`
}

type withdrawalRequestBody struct {
	AmountKobo int64 `json:"amount_kobo" binding:"required"`
}

func (h *Handler) ListBanks(c *gin.Context) {
	banks, err := h.service.ListBanks(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	httpx.CachePrivate(c, 3600)
	httpx.OK(c, banks)
}

func (h *Handler) SaveBankAccount(c *gin.Context) {
	ownerID, shopID, ok := ownerAndShop(c)
	if !ok {
		return
	}
	var req saveBankAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Payouts Handler", "bank and account number are required"))
		return
	}
	account, err := h.service.SaveBankAccount(c.Request.Context(), ownerID, shopID, req.BankCode, req.AccountNumber)
	if err != nil {
		writeError(c, err)
		return
	}
	httpx.OK(c, account)
}

func (h *Handler) GetBankAccount(c *gin.Context) {
	ownerID, shopID, ok := ownerAndShop(c)
	if !ok {
		return
	}
	account, err := h.service.GetBankAccount(c.Request.Context(), ownerID, shopID)
	if err != nil {
		writeError(c, err)
		return
	}
	httpx.OK(c, account)
}

func (h *Handler) Wallet(c *gin.Context) {
	ownerID, shopID, ok := ownerAndShop(c)
	if !ok {
		return
	}
	wallet, err := h.service.Wallet(c.Request.Context(), ownerID, shopID)
	if err != nil {
		writeError(c, err)
		return
	}
	httpx.OK(c, wallet)
}

func (h *Handler) WalletEntries(c *gin.Context) {
	ownerID, shopID, ok := ownerAndShop(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	result, err := h.service.WalletEntries(c.Request.Context(), ownerID, shopID, page, pageSize)
	if err != nil {
		writeError(c, err)
		return
	}
	httpx.OK(c, result)
}

func (h *Handler) RequestWithdrawal(c *gin.Context) {
	ownerID, shopID, ok := ownerAndShop(c)
	if !ok {
		return
	}
	var req withdrawalRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Payouts Handler", "a valid amount is required"))
		return
	}
	request, err := h.service.RequestWithdrawal(c.Request.Context(), ownerID, shopID, req.AmountKobo)
	if err != nil {
		writeError(c, err)
		return
	}
	httpx.Created(c, request)
}

func (h *Handler) ListWithdrawals(c *gin.Context) {
	ownerID, shopID, ok := ownerAndShop(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	rows, total, err := h.service.ListWithdrawals(c.Request.Context(), ownerID, shopID, page, pageSize)
	if err != nil {
		writeError(c, err)
		return
	}
	httpx.OK(c, gin.H{"withdrawals": rows, "total": total})
}

func ownerAndShop(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Payouts Handler", "unauthorized user"))
		return uuid.Nil, uuid.Nil, false
	}
	shopID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Payouts Handler", "invalid shop ID"))
		return uuid.Nil, uuid.Nil, false
	}
	return ownerID, shopID, true
}

func writeError(c *gin.Context, err error) {
	var appErr *errorMap.AppError
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case errorMap.CodeNotFound:
			httpx.NotFound(c, appErr)
		case errorMap.CodeForbidden:
			httpx.Forbidden(c, appErr)
		case errorMap.CodeInvalidInput:
			httpx.BadRequest(c, appErr)
		case errorMap.CodeUnauthorized:
			httpx.Unauthorized(c, appErr)
		default:
			httpx.InternalServerError(c, appErr)
		}
		return
	}
	httpx.InternalServerError(c, errorMap.New(errorMap.CodeInternal, "Payouts Handler", "an unexpected error occurred"))
}
