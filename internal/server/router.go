package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"

	"barber-booking-backend/internal/config"
	"barber-booking-backend/internal/locks"
	"barber-booking-backend/internal/middleware"
	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/modules/auth"
	"barber-booking-backend/internal/modules/bookings"
	"barber-booking-backend/internal/modules/payments"
	"barber-booking-backend/internal/modules/shops"
	"barber-booking-backend/internal/modules/slots"
	"barber-booking-backend/internal/notifications"
	"barber-booking-backend/internal/paystack"
)

type Dependencies struct {
	Config   config.Config
	DB       *gorm.DB
	Redis    *redis.Client
	Notifier *notifications.Notifier
	Paystack *paystack.Client
}

func NewRouter(deps Dependencies) http.Handler {
	if deps.Config.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(
		middleware.RequestID(),
		gin.Logger(),
		gin.Recovery(),
		middleware.Timeout(20*time.Second),
		middleware.RateLimit(deps.Redis, deps.Config.RateLimitPerMinute),
	)

	locker := locks.NewRedisLocker(deps.Redis)
	authService := auth.NewService(deps.DB, deps.Config.JWT)
	shopService := shops.NewService(deps.DB)
	slotService := slots.NewService(deps.DB)
	bookingService := bookings.NewService(deps.DB, locker, deps.Paystack, deps.Notifier, deps.Config.BookingAmountKobo)

	authHandler := auth.NewHandler(authService)
	shopHandler := shops.NewHandler(shopService)
	slotHandler := slots.NewHandler(slotService)
	bookingHandler := bookings.NewHandler(bookingService)
	paymentHandler := payments.NewHandler(bookingService)

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	authRoutes := router.Group("/auth")
	{
		authRoutes.POST("/signup", authHandler.Signup)
		authRoutes.POST("/login", authHandler.Login)
		authRoutes.POST("/refresh", authHandler.Refresh)
		authRoutes.POST("/logout", middleware.Auth(deps.Config.JWT), authHandler.Logout)
	}

	shopRoutes := router.Group("/shops")
	{
		shopRoutes.POST("", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.CreateShop)
		shopRoutes.GET("/:id", shopHandler.GetShop)
		shopRoutes.PATCH("/:id", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.UpdateShop)
		shopRoutes.POST("/:id/business-days", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.UpsertBusinessDays)
		shopRoutes.GET("/:id/business-days", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.ListBusinessDays)
		shopRoutes.PATCH("/:id/business-days/:dayId", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.PatchBusinessDay)
		shopRoutes.POST("/:id/blocked-dates", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.AddBlockedDate)
		shopRoutes.GET("/:id/blocked-dates", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.ListBlockedDates)
		shopRoutes.DELETE("/:id/blocked-dates/:blockedDateId", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.DeleteBlockedDate)
		shopRoutes.GET("/:id/slots", slotHandler.GetSlots)
	}

	bookingRoutes := router.Group("/bookings")
	{
		bookingRoutes.POST("/initiate", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleCustomer), bookingHandler.Initiate)
		bookingRoutes.POST("/reschedule", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleCustomer), bookingHandler.Reschedule)
		bookingRoutes.POST("/cancel", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleCustomer), bookingHandler.Cancel)
		bookingRoutes.GET("/:code", bookingHandler.GetByCode)
	}

	paymentRoutes := router.Group("/payments")
	{
		paymentRoutes.POST("/init", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleCustomer), paymentHandler.Init)
		paymentRoutes.POST("/webhook", paymentHandler.Webhook)
	}

	return router
}
