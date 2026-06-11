package shops

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"barber-booking-backend/internal/httpx"
)

// Discover is the public shop-discovery endpoint:
// GET /shops?lat=&lng=&radius=&search=&page=&page_size=
func (h *Handler) Discover(c *gin.Context) {
	params := DiscoverParams{
		Search: c.Query("search"),
	}
	params.Page, _ = strconv.Atoi(c.Query("page"))
	params.PageSize, _ = strconv.Atoi(c.Query("page_size"))

	if v := c.Query("lat"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			params.Lat = &f
		}
	}
	if v := c.Query("lng"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			params.Lng = &f
		}
	}
	if v := c.Query("radius"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			params.RadiusKm = f
		}
	}

	result, err := h.service.Discover(c.Request.Context(), params)
	if err != nil {
		writeShopError(c, err)
		return
	}

	httpx.CachePublic(c, 30)
	httpx.OK(c, result)
}
