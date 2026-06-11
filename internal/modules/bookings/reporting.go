package bookings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"barber-booking-backend/internal/models"
	errorMap "barber-booking-backend/internal/utils/error"
)

// BookingListItem enriches a booking with its service name and payment state
// for the owner dashboard and the public status lookup.
type BookingListItem struct {
	BookingResponse
	ServiceName   string `json:"service_name"`
	AmountKobo    int64  `json:"amount_kobo"`
	PaymentStatus string `json:"payment_status"`
}

type BookingListResult struct {
	Bookings []BookingListItem `json:"bookings"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

type ListBookingsParams struct {
	Status   string
	Search   string
	Page     int
	PageSize int
}

type RevenuePoint struct {
	Date        string `json:"date"`
	RevenueKobo int64  `json:"revenue_kobo"`
	Bookings    int64  `json:"bookings"`
}

type TopService struct {
	ServiceID   uuid.UUID `json:"service_id"`
	Name        string    `json:"name"`
	Bookings    int64     `json:"bookings"`
	RevenueKobo int64     `json:"revenue_kobo"`
}

type AnalyticsSummary struct {
	TotalRevenueKobo  int64          `json:"total_revenue_kobo"`
	TotalBookings     int64          `json:"total_bookings"`
	ConfirmedBookings int64          `json:"confirmed_bookings"`
	CancelledBookings int64          `json:"cancelled_bookings"`
	ExpiredBookings   int64          `json:"expired_bookings"`
	RevenueSeries     []RevenuePoint `json:"revenue_series"`
	TopServices       []TopService   `json:"top_services"`
}

// ownedShop loads a shop and verifies it belongs to the owner.
func (s *Service) ownedShop(ctx context.Context, ownerID, shopID uuid.UUID) (models.Shop, error) {
	var shop models.Shop
	err := s.db.WithContext(ctx).First(&shop, "id = ?", shopID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Shop{}, errorMap.New(errorMap.CodeNotFound, "Bookings: Owned Shop", "shop not found")
	}
	if err != nil {
		return models.Shop{}, errorMap.Wrap(err, errorMap.CodeInternal, "Bookings: Owned Shop", "failed to load shop")
	}
	if shop.OwnerID != ownerID {
		return models.Shop{}, errorMap.New(errorMap.CodeForbidden, "Bookings: Owned Shop", "shop does not belong to owner")
	}
	return shop, nil
}

func toBookingResponse(b models.Booking) BookingResponse {
	return BookingResponse{
		ID:               b.ID,
		Code:             b.Code,
		ShopID:           b.ShopID,
		ServiceID:        b.ServiceID,
		CustomerName:     b.CustomerName,
		CustomerEmail:    b.CustomerEmail,
		Status:           string(b.Status),
		StartsAt:         b.StartsAt,
		EndsAt:           b.EndsAt,
		PaymentReference: b.PaymentReference,
	}
}

// latestPaymentByBooking returns a map of booking id -> payment for the given bookings.
func (s *Service) latestPaymentByBooking(ctx context.Context, bookingIDs []uuid.UUID) (map[uuid.UUID]models.Payment, error) {
	out := map[uuid.UUID]models.Payment{}
	if len(bookingIDs) == 0 {
		return out, nil
	}
	var payments []models.Payment
	if err := s.db.WithContext(ctx).
		Where("booking_id IN ?", bookingIDs).
		Order("created_at desc").
		Find(&payments).Error; err != nil {
		return out, errorMap.Wrap(err, errorMap.CodeInternal, "Reporting Service: Payment By Booking", "Error getting payment")
	}
	for _, p := range payments {
		if _, seen := out[p.BookingID]; !seen {
			out[p.BookingID] = p
		}
	}
	return out, nil
}

// GetByCode returns a single booking (public) by its code.
func (s *Service) GetByCode(ctx context.Context, code string) (BookingListItem, error) {
	var b models.Booking
	err := s.db.WithContext(ctx).Preload("Service").Where("code = ?", strings.TrimSpace(code)).First(&b).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return BookingListItem{}, errorMap.New(errorMap.CodeNotFound, "Bookings: GetByCode", ErrBookingNotFound.Error())
	}
	if err != nil {
		return BookingListItem{}, errorMap.Wrap(err, errorMap.CodeInternal, "Bookings: GetByCode", "failed to load booking")
	}

	item := BookingListItem{
		BookingResponse: toBookingResponse(b),
		ServiceName:     b.Service.Name,
	}
	paymentMap, err := s.latestPaymentByBooking(ctx, []uuid.UUID{b.ID})
	if err != nil {
		return BookingListItem{}, err
	}

	p, exists := paymentMap[b.ID]
	if !exists {
		msg := fmt.Sprintf("no payment found for booking %s", b.ID)
		return BookingListItem{}, errorMap.New(errorMap.CodeInvalidInput, "Reporting Service: Get Payment", msg)
	}

	item.AmountKobo = p.AmountKobo
	item.PaymentStatus = string(p.Status)

	return item, nil
}

// ListForShop returns a paginated, optionally searched/filtered list of a shop's
// bookings. Owner-scoped.
func (s *Service) ListForShop(ctx context.Context, ownerID, shopID uuid.UUID, params ListBookingsParams) (BookingListResult, error) {
	if _, err := s.ownedShop(ctx, ownerID, shopID); err != nil {
		return BookingListResult{}, err
	}

	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	q := s.db.WithContext(ctx).Model(&models.Booking{}).Where("shop_id = ?", shopID)
	if params.Status != "" && params.Status != "all" {
		q = q.Where("status = ?", params.Status)
	}
	if search := strings.TrimSpace(params.Search); search != "" {
		like := "%" + search + "%"
		q = q.Where(
			"customer_name ILIKE ? OR customer_email ILIKE ? OR code ILIKE ?",
			like, like, like,
		)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return BookingListResult{}, errorMap.Wrap(err, errorMap.CodeInternal, "Bookings: ListForShop", "failed to count bookings")
	}

	var bookings []models.Booking
	if err := q.Preload("Service").
		Order("starts_at desc").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&bookings).Error; err != nil {
		return BookingListResult{}, errorMap.Wrap(err, errorMap.CodeInternal, "Bookings: ListForShop", "failed to list bookings")
	}

	ids := make([]uuid.UUID, 0, len(bookings))
	for _, b := range bookings {
		ids = append(ids, b.ID)
	}
	payments, err := s.latestPaymentByBooking(ctx, ids)
	if err != nil {
		return BookingListResult{}, err
	}

	items := make([]BookingListItem, 0, len(bookings))
	for _, b := range bookings {
		item := BookingListItem{
			BookingResponse: toBookingResponse(b),
			ServiceName:     b.Service.Name,
		}
		if p, ok := payments[b.ID]; ok {
			item.AmountKobo = p.AmountKobo
			item.PaymentStatus = string(p.Status)
		}
		items = append(items, item)
	}

	return BookingListResult{Bookings: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func rangeToDays(r string) int {
	switch r {
	case "7d":
		return 7
	case "90d":
		return 90
	default:
		return 30
	}
}

// Analytics aggregates booking and revenue metrics for the owner dashboard.
func (s *Service) Analytics(ctx context.Context, ownerID, shopID uuid.UUID, rangeKey string) (AnalyticsSummary, error) {
	if _, err := s.ownedShop(ctx, ownerID, shopID); err != nil {
		return AnalyticsSummary{}, err
	}

	days := rangeToDays(rangeKey)
	since := time.Now().UTC().AddDate(0, 0, -days)
	var summary AnalyticsSummary

	base := s.db.WithContext(ctx).Model(&models.Booking{}).
		Where("shop_id = ? AND created_at >= ?", shopID, since)

	countWhere := func(status models.BookingStatus) int64 {
		var n int64
		s.db.WithContext(ctx).Model(&models.Booking{}).
			Where("shop_id = ? AND created_at >= ? AND status = ?", shopID, since, status).
			Count(&n)
		return n
	}

	if err := base.Count(&summary.TotalBookings).Error; err != nil {
		return AnalyticsSummary{}, errorMap.Wrap(err, errorMap.CodeInternal, "Bookings: Analytics", "failed to count bookings")
	}
	summary.ConfirmedBookings = countWhere(models.BookingConfirmed)
	summary.CancelledBookings = countWhere(models.BookingCancelled)
	summary.ExpiredBookings = countWhere(models.BookingExpired)

	// Total revenue = successful payments tied to this shop's bookings in range.
	s.db.WithContext(ctx).
		Model(&models.Payment{}).
		Joins("JOIN bookings ON bookings.id = payments.booking_id").
		Where("bookings.shop_id = ? AND payments.status = ? AND payments.paid_at >= ?",
			shopID, models.PaymentSuccess, since).
		Select("COALESCE(SUM(payments.amount_kobo), 0)").
		Scan(&summary.TotalRevenueKobo)

	// Revenue series (per day).
	var points []RevenuePoint
	s.db.WithContext(ctx).
		Model(&models.Payment{}).
		Joins("JOIN bookings ON bookings.id = payments.booking_id").
		Where("bookings.shop_id = ? AND payments.status = ? AND payments.paid_at >= ?",
			shopID, models.PaymentSuccess, since).
		Select("TO_CHAR(payments.paid_at, 'YYYY-MM-DD') AS date, " +
			"COALESCE(SUM(payments.amount_kobo),0) AS revenue_kobo, COUNT(*) AS bookings").
		Group("date").
		Order("date asc").
		Scan(&points)
	summary.RevenueSeries = points

	// Top services by revenue.
	var top []TopService
	s.db.WithContext(ctx).
		Model(&models.Payment{}).
		Joins("JOIN bookings ON bookings.id = payments.booking_id").
		Joins("JOIN services ON services.id = bookings.service_id").
		Where("bookings.shop_id = ? AND payments.status = ? AND payments.paid_at >= ?",
			shopID, models.PaymentSuccess, since).
		Select("services.id AS service_id, services.name AS name, " +
			"COUNT(*) AS bookings, COALESCE(SUM(payments.amount_kobo),0) AS revenue_kobo").
		Group("services.id, services.name").
		Order("revenue_kobo desc").
		Limit(5).
		Scan(&top)
	summary.TopServices = top

	return summary, nil
}
