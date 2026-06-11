package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
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
	"barber-booking-backend/internal/modules/payouts"
	"barber-booking-backend/internal/modules/shops"
	"barber-booking-backend/internal/modules/slots"
	"barber-booking-backend/internal/modules/sse"
	"barber-booking-backend/internal/modules/uploads"
	"barber-booking-backend/internal/notifications"
	"barber-booking-backend/internal/paystack"
	"barber-booking-backend/internal/storage"
)

type Dependencies struct {
	Config   config.Config
	DB       *gorm.DB
	Redis    *redis.Client
	Logger   *slog.Logger
	Notifier *notifications.Notifier
	Paystack *paystack.Client
	Uploader storage.Uploader
	SSE      *sse.SSEManager
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

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	sseClient := sse.NewSSEManager(logger)
	locker := locks.NewRedisLocker(deps.Redis)
	authRepo := auth.NewGormAuthRepository(deps.DB)
	authService := auth.NewService(deps.DB, deps.Config.JWT, authRepo, logger, deps.Notifier, deps.Config.FrontendURL)
	shopService := shops.NewService(deps.DB, logger)
	slotService := slots.NewService(deps.DB, logger)
	bookingService := bookings.NewService(deps.DB, sseClient, locker, logger, deps.Paystack, deps.Notifier, deps.Config.BookingAmountKobo, deps.Config.PlatformFeePercent)
	payoutsService := payouts.NewService(deps.DB, deps.Paystack, logger)

	authHandler := auth.NewHandler(authService, logger)
	shopHandler := shops.NewHandler(shopService, logger)
	slotHandler := slots.NewHandler(slotService)
	bookingHandler := bookings.NewHandler(bookingService)
	paymentHandler := payments.NewHandler(bookingService, payoutsService)
	uploadHandler := uploads.NewHandler(deps.Uploader)
	payoutHandler := payouts.NewHandler(payoutsService)

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	authRoutes := router.Group("/auth")
	{
		authRoutes.POST("/signup", authHandler.Signup)
		authRoutes.POST("/login", authHandler.Login)
		authRoutes.POST("/refresh", authHandler.Refresh)
		authRoutes.POST("/logout", middleware.Auth(deps.Config.JWT), authHandler.Logout)
		authRoutes.POST("/forgot-password", authHandler.ForgotPassword)
		authRoutes.POST("/reset-password", authHandler.ResetPassword)
		authRoutes.POST("/verify-email", authHandler.VerifyEmail)
		authRoutes.POST("/resend-verification", authHandler.ResendVerification)
	}

	shopRoutes := router.Group("/shops")
	{
		shopRoutes.GET("", shopHandler.Discover)
		shopRoutes.POST("", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.CreateShop)
		shopRoutes.GET("/mine", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.ListMyShops)
		shopRoutes.GET("/:id", shopHandler.GetShop)
		shopRoutes.PATCH("/:id", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), shopHandler.UpdateShop)
		shopRoutes.GET("/:id/bookings", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), bookingHandler.ListForShop)
		shopRoutes.GET("/:id/analytics", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), bookingHandler.Analytics)
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

		// Payouts (owner-only)
		owner := []gin.HandlerFunc{middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner)}
		shopRoutes.POST("/:id/bank-account", append(owner, payoutHandler.SaveBankAccount)...)
		shopRoutes.GET("/:id/bank-account", append(owner, payoutHandler.GetBankAccount)...)
		shopRoutes.GET("/:id/wallet", append(owner, payoutHandler.Wallet)...)
		shopRoutes.GET("/:id/wallet/entries", append(owner, payoutHandler.WalletEntries)...)
		shopRoutes.POST("/:id/withdrawals", append(owner, payoutHandler.RequestWithdrawal)...)
		shopRoutes.GET("/:id/withdrawals", append(owner, payoutHandler.ListWithdrawals)...)
	}

	bookingRoutes := router.Group("/bookings")
	{
		bookingRoutes.POST("/initiate", bookingHandler.Initiate)
		// Public, identified by booking code — customers have no account.
		bookingRoutes.POST("/reschedule", bookingHandler.Reschedule)
		bookingRoutes.POST("/cancel", bookingHandler.Cancel)
		bookingRoutes.GET("/:code", bookingHandler.GetByCode)
		bookingRoutes.GET("/:code/status-stream", bookingHandler.GetBookingStatusStream)
	}

	paymentRoutes := router.Group("/payments")
	{
		paymentRoutes.POST("/init", paymentHandler.Init)
		paymentRoutes.POST("/webhook", paymentHandler.Webhook)
	}

	router.POST("/uploads", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), uploadHandler.Upload)
	router.GET("/payouts/banks", middleware.Auth(deps.Config.JWT), middleware.RequireRole(models.RoleOwner), payoutHandler.ListBanks)

	return router
}
