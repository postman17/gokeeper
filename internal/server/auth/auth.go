// Package auth provides password hashing and JWT token management.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// TokenTTL is the lifetime of issued JWT tokens.
const TokenTTL = 24 * time.Hour

// ErrInvalidToken is returned when a token cannot be verified.
var ErrInvalidToken = errors.New("invalid token")

// HashPassword hashes a plain-text password with bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword compares a plain-text password with a bcrypt hash.
func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// JWTManager issues and verifies JWT tokens.
type JWTManager struct {
	secret []byte
	ttl    time.Duration
}

// NewJWTManager creates a JWTManager with the given signing secret and token TTL.
func NewJWTManager(secret string, ttl time.Duration) *JWTManager {
	return &JWTManager{secret: []byte(secret), ttl: ttl}
}

// Claims holds the JWT payload for GophKeeper tokens.
type Claims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

// NewToken issues a signed JWT for the given user ID.
func (m *JWTManager) NewToken(userID string) (string, error) {
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// ParseToken verifies a signed JWT and returns the user ID from its claims.
func (m *JWTManager) ParseToken(tokenString string) (string, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil || !token.Valid {
		return "", ErrInvalidToken
	}
	if claims.UserID == "" {
		return "", ErrInvalidToken
	}
	return claims.UserID, nil
}
