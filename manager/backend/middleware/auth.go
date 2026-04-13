package middleware

import (
	"log"
	"net/http"
	"strings"
	"time"
	"strconv"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

// min 函数用于获取两个整数的最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type Claims struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

var jwtSecret = []byte("xiaozhi_admin_secret_key")

// 生成JWT Token
func GenerateToken(userID uint, username, role string) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// 解析JWT Token
func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})

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
		log.Printf("[JWTAuth] Authorization头: %s", authHeader)
		
		if authHeader == "" {
			log.Printf("[JWTAuth] ❌ 缺少认证头")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少认证头"})
			c.Abort()
			return
		}

		tokenString := strings.Replace(authHeader, "Bearer ", "", 1)
		log.Printf("[JWTAuth] 提取的token长度: %d, 前缀: %s", len(tokenString), tokenString[:min(20, len(tokenString))])
		
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


// pad2 将数字格式化为2位字符串，不足补0
func pad2(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return strconv.Itoa(n)
}

// dateStrNow 获取当前UTC日期字符串，格式：yyyy-MM-dd
func dateStrNow() string {
	now := time.Now().UTC()
	return strconv.Itoa(now.Year()) + "-" + pad2(int(now.Month())) + "-" + pad2(now.Day())
}

// simpleHash 计算字符串的简单哈希值（与app.js相同的算法）
func simpleHash(value string) string {
	const mod1 int64 = 1000000007
	const mod2 int64 = 1000000009
	var h1 int64 = 0
	var h2 int64 = 0
	for i := 0; i < len(value); i++ {
		c := int64(value[i])
		h1 = (h1*131 + c) % mod1
		h2 = (h2*137 + c) % mod2
	}
	// 转换为16进制字符串，与JavaScript的toString(16)保持一致
	return strconv.FormatInt(h1, 16) + strconv.FormatInt(h2, 16)
}

// calculateDailyToken 计算当日令牌
func calculateDailyToken(dateStr string,mqttSignatureKey string) string {
	ds := dateStr
	if ds == "" {
		ds = dateStrNow()
	}
	log.Printf("[McpDailyAuth] 计算令牌，日期: %s, 密钥: %s", ds, mqttSignatureKey)
	return simpleHash(ds + mqttSignatureKey)
}

// verifyAuthorizationHeader 验证Authorization头
func verifyAuthorizationHeader(authorization string,mqttSignatureKey string) bool {
	expected := calculateDailyToken("",mqttSignatureKey)
	log.Printf("[McpDailyAuth] 验证Authorization头: %s, 期望: %s", authorization, expected)
	return authorization == expected
}


func McpDailyAuth(mqttSignatureKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 添加调试日志
		log.Printf("[McpDailyAuth] 处理请求: %s %s, 客户端IP: %s", c.Request.Method, c.Request.URL.Path, c.ClientIP())

		authHeader := c.GetHeader("Authorization")
		log.Printf("[McpDailyAuth] Authorization头: %s", authHeader)

		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			log.Printf("[McpDailyAuth] ❌ 未提供有效的Authorization头")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未提供有效的Authorization头"})
			c.Abort()
			return
		}

		tokenString := strings.Replace(authHeader, "Bearer ", "", 1)
		log.Printf("[McpDailyAuth] 提取的token长度: %d, 前缀: %s", len(tokenString), tokenString[:min(20, len(tokenString))])

		// 验证token
		if !verifyAuthorizationHeader(tokenString,mqttSignatureKey) {
			log.Printf("[McpDailyAuth] ❌ 无效的授权令牌")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的授权令牌"})
			c.Abort()
			return
		}

		log.Printf("[McpDailyAuth] ✅ token验证成功")
		c.Next()
	}
}
