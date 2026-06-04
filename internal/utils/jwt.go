package utils

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"barber-booking-backend/internal/config"
	"barber-booking-backend/internal/models"
	errorMap "barber-booking-backend/internal/utils/error"
)

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

type Claims struct {
	UserID    string          `json:"user_id"`
	Role      models.UserRole `json:"role"`
	TokenType string          `json:"token_type"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

func GenerateTokenPair(user models.User, cfg config.JWTConfig) (TokenPair, error) {
	now := time.Now().UTC()
	access, err := signJWT(Claims{
		UserID:    user.ID.String(),
		Role:      user.Role,
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(cfg.AccessTokenTTL)),
		},
	}, cfg.Secret)
	if err != nil {
		return TokenPair{}, err
	}

	refreshExpiresAt := now.Add(cfg.RefreshTokenTTL)
	refresh, err := signJWT(Claims{
		UserID:    user.ID.String(),
		Role:      user.Role,
		TokenType: TokenTypeRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(refreshExpiresAt),
		},
	}, cfg.Secret)
	if err != nil {
		return TokenPair{}, err
	}

	return TokenPair{
		AccessToken:      access,
		RefreshToken:     refresh,
		RefreshExpiresAt: refreshExpiresAt,
	}, nil
}

func ParseJWT(tokenString, secret, expectedType string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errorMap.New(errorMap.CodeInvalidInput, "JWT Parsing", "unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "JWT Parsing", "failed to parse token")
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errorMap.New(errorMap.CodeInvalidInput, "JWT Parsing", "invalid token")
	}
	if claims.TokenType != expectedType {
		return nil, errorMap.New(errorMap.CodeInvalidInput, "JWT Parsing", "invalid token")
	}
	if _, err := uuid.Parse(claims.UserID); err != nil {
		return nil, errorMap.New(errorMap.CodeInvalidInput, "JWT Parsing", "invalid token")
	}

	return claims, nil
}

func signJWT(claims Claims, secret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}
