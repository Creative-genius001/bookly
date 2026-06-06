package server

import (
	"log/slog"
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
	Logger   *slog.Logger
	Notifier *notifications.Notifier
	Paystack *paystack.Client
}

func NewRouter(deps Dependencies) http.Handler {
	if deps.Config.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	router := gin.New()
	router.Use(
		middleware.RequestID(),
		middleware.ErrorHandler(logger),
		middleware.Recovery(logger),
		middleware.RequestLogger(logger),
		middleware.Timeout(20*time.Second),
		middleware.RateLimit(deps.Redis, deps.Config.RateLimitPerMinute),
	)

	locker := locks.NewRedisLocker(deps.Redis)
	authRepo := auth.NewGormAuthRepository(deps.DB)
	authService := auth.NewService(deps.DB, deps.Config.JWT, authRepo, logger)
	shopService := shops.NewService(deps.DB, logger)
	slotService := slots.NewService(deps.DB, logger)
	bookingService := bookings.NewService(deps.DB, locker, logger, deps.Paystack, deps.Notifier, deps.Config.BookingAmountKobo)

	authHandler := auth.NewHandler(authService, logger)
	shopHandler := shops.NewHandler(shopService, logger)
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
		shopRoutes.POST("/:id/services", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.AddService)
		shopRoutes.GET("/:id/services", shopHandler.ListServices)
		shopRoutes.GET("/services/:id", shopHandler.GetService)
		shopRoutes.PATCH("/:id/services/:serviceId", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.UpdateService)
		shopRoutes.DELETE("/services/:id", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.DeleteService)
		shopRoutes.GET("/:id/availability", slotHandler.GetSlots)
	}

	bookingRoutes := router.Group("/bookings")
	{
		bookingRoutes.POST("/initiate", bookingHandler.Initiate)
		// bookingRoutes.POST("/reschedule", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleCustomer), bookingHandler.Reschedule)
		// bookingRoutes.POST("/cancel", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleCustomer), bookingHandler.Cancel)
		// bookingRoutes.GET("/:code", bookingHandler.GetByCode)
	}

	paymentRoutes := router.Group("/payments")
	{
		paymentRoutes.POST("/init", paymentHandler.Init)
		paymentRoutes.POST("/webhook", paymentHandler.Webhook)
	}

	return router
}
