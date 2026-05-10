package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

type InternalClaims struct {
	Kind  string `json:"kind"`
	Role  string `json:"role,omitempty"`
	Email string `json:"email,omitempty"`
	jwt.RegisteredClaims
}

func HashPassword(password string) (string, error) {
	encoded, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func CheckPassword(hash string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func GenerateSessionToken() (string, error) {
	return randomString(32)
}

func GenerateCSRFToken() (string, error) {
	return randomString(24)
}

func GenerateAPIKey() (raw string, prefix string, hash string, err error) {
	token, err := randomString(32)
	if err != nil {
		return "", "", "", err
	}
	raw = "sk-inf-" + token
	if len(raw) < 14 {
		return "", "", "", errors.New("generated api key too short")
	}
	prefix = raw[:14]
	hashBytes := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(hashBytes[:])
	return raw, prefix, hash, nil
}

func HashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func IssueInternalJWT(secret string, subject string, kind string, role string, email string, ttl time.Duration) (string, error) {
	claims := InternalClaims{
		Kind:  kind,
		Role:  role,
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.NewString(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ParseInternalJWT(secret string, value string) (*InternalClaims, error) {
	token, err := jwt.ParseWithClaims(value, &InternalClaims{}, func(token *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*InternalClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid internal token")
	}
	return claims, nil
}

func ValidateTOTP(secret string, code string) bool {
	if secret == "" || code == "" {
		return false
	}
	return totp.Validate(code, secret)
}

func ProvisioningURL(email string, issuer string, secret string) string {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: email,
		Secret:      []byte(secret),
	})
	if err != nil {
		return ""
	}
	return key.URL()
}

func GenerateTOTPSetup(email string, issuer string) (secret string, provisioningURL string, qrCodeDataURL string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: email,
	})
	if err != nil {
		return "", "", "", err
	}
	imageValue, err := key.Image(240, 240)
	if err != nil {
		return "", "", "", err
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, imageValue); err != nil {
		return "", "", "", err
	}
	return key.Secret(), key.URL(), "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()), nil
}

func randomString(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func BearerToken(value string) string {
	return fmt.Sprintf("Bearer %s", value)
}
