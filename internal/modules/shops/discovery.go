package shops

import (
	"context"
	"strings"

	"github.com/google/uuid"

	errorMap "barber-booking-backend/internal/utils/error"
)

// ShopCard is the public, customer-facing shape returned by discovery.
type ShopCard struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	Address       string    `json:"address"`
	Phone         string    `json:"phone"`
	LogoURL       string    `json:"logo_url"`
	CoverImageURL string    `json:"cover_image_url"`
	Latitude      *float64  `json:"latitude"`
	Longitude     *float64  `json:"longitude"`
	DistanceKm    *float64  `json:"distance_km,omitempty"`
}

type DiscoverParams struct {
	Lat      *float64
	Lng      *float64
	RadiusKm float64
	Search   string
	Page     int
	PageSize int
}

type DiscoverResult struct {
	Shops    []ShopCard `json:"shops"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

// Discover lists active shops, optionally sorted by distance from a point
// (PostGIS ST_DistanceSphere) and filtered by a radius and/or text search.
func (s *Service) Discover(ctx context.Context, p DiscoverParams) (DiscoverResult, error) {
	page := p.Page
	if page < 1 {
		page = 1
	}
	pageSize := p.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	hasGeo := p.Lat != nil && p.Lng != nil

	where := []string{"is_active = true"}
	args := []interface{}{}
	if hasGeo {
		where = append(where, "latitude IS NOT NULL", "longitude IS NOT NULL")
	}
	if search := strings.TrimSpace(p.Search); search != "" {
		where = append(where, "(name ILIKE ? OR address ILIKE ?)")
		like := "%" + search + "%"
		args = append(args, like, like)
	}

	// Distance expression in km (only meaningful when geo is provided).
	distanceExpr := "NULL::float8"
	radiusArgs := []interface{}{}
	if hasGeo {
		distanceExpr = "ST_DistanceSphere(ST_MakePoint(longitude, latitude), ST_MakePoint(?, ?)) / 1000.0"
		radiusArgs = append(radiusArgs, *p.Lng, *p.Lat)
		if p.RadiusKm > 0 {
			where = append(where,
				"ST_DistanceSphere(ST_MakePoint(longitude, latitude), ST_MakePoint(?, ?)) <= ?")
			args = append(args, *p.Lng, *p.Lat, p.RadiusKm*1000.0)
		}
	}

	whereSQL := strings.Join(where, " AND ")

	// Count (without the distance column).
	var total int64
	countSQL := "SELECT COUNT(*) FROM shops WHERE " + whereSQL
	if err := s.db.WithContext(ctx).Raw(countSQL, args...).Scan(&total).Error; err != nil {
		return DiscoverResult{}, errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Discover", "failed to count shops")
	}

	orderBy := "created_at DESC"
	if hasGeo {
		orderBy = "distance_km ASC NULLS LAST"
	}

	selectSQL := "SELECT id, name, slug, address, phone, logo_url, cover_image_url, latitude, longitude, " +
		distanceExpr + " AS distance_km FROM shops WHERE " + whereSQL +
		" ORDER BY " + orderBy + " LIMIT ? OFFSET ?"

	// Arg order must match placeholder order: distance SELECT args, then WHERE
	// args, then LIMIT/OFFSET.
	queryArgs := append([]interface{}{}, radiusArgs...)
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)

	var cards []ShopCard
	if err := s.db.WithContext(ctx).Raw(selectSQL, queryArgs...).Scan(&cards).Error; err != nil {
		return DiscoverResult{}, errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Discover", "failed to list shops")
	}

	return DiscoverResult{Shops: cards, Total: total, Page: page, PageSize: pageSize}, nil
}
