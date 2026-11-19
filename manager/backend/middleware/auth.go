package middleware

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

type Claims struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type signingConfig struct {
	secret []byte
	expiry time.Duration
}

var currentSigningConfig atomic.Pointer[signingConfig]

const legacyJWTSecret = "xiaozhi_admin_secret_key"

func ConfigureJWT(secret string, expireHour int) error {
	secret = strings.TrimSpace(secret)
	key := []byte(secret)
	switch {
	case secret == "":
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return fmt.Errorf("generate JWT signing key: %w", err)
		}
		log.Print("JWT_SECRET and jwt.secret are unset; using a temporary signing key. Existing sessions will not survive a restart.")
	case secret == legacyJWTSecret:
		return errors.New("the previous default JWT secret is not allowed; clear jwt.secret or configure a new random secret")
	case len(key) < 32:
		return errors.New("JWT_SECRET or jwt.secret must be a random value of at least 32 bytes")
	}
	if expireHour <= 0 {
		expireHour = 24
	}
	currentSigningConfig.Store(&signingConfig{
		secret: key,
		expiry: time.Duration(expireHour) * time.Hour,
	})
	return nil
}

// 生成JWT Token
func GenerateToken(userID uint, username, role string) (string, error) {
	config := currentSigningConfig.Load()
	if config == nil {
		return "", errors.New("JWT signing is not configured")
	}
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(config.expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(config.secret)
}

// 解析JWT Token
func ParseToken(tokenString string) (*Claims, error) {
	config := currentSigningConfig.Load()
	if config == nil {
		return nil, errors.New("JWT signing is not configured")
	}
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return config.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrInvalidKey
}

// JWT认证中间件
func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 添加调试日志
		log.Printf("[JWTAuth] 处理请求: %s %s, 客户端IP: %s", c.Request.Method, c.Request.URL.Path, c.ClientIP())

		authHeader := c.GetHeader("Authorization")

		if authHeader == "" {
			log.Printf("[JWTAuth] ❌ 缺少认证头")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少认证头"})
			c.Abort()
			return
		}

		tokenString := strings.Replace(authHeader, "Bearer ", "", 1)

		claims, err := ParseToken(tokenString)
		if err != nil {
			log.Printf("[JWTAuth] ❌ token解析失败: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的token"})
			c.Abort()
			return
		}

		log.Printf("[JWTAuth] ✅ token验证成功 - 用户ID: %d, 用户名: %s, 角色: %s", claims.UserID, claims.Username, claims.Role)
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Next()
	}
}

// 管理员权限中间件
func AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists || role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
			c.Abort()
			return
		}
		c.Next()
	}
}
