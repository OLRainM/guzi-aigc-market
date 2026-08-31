package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const userContextKey = "auth_user"

var errInvalidCredentials = errors.New("invalid credentials")

type Config struct {
	JWTSecret     string
	Issuer        string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
	CookieSecure  bool
	RefreshCookie string
}

type User struct {
	ID           string     `gorm:"type:char(36);primaryKey" json:"id"`
	Username     string     `gorm:"size:32;uniqueIndex;not null" json:"username"`
	Email        *string    `gorm:"size:254;uniqueIndex" json:"email,omitempty"`
	PasswordHash string     `gorm:"size:255;not null" json:"-"`
	Status       string     `gorm:"size:16;not null;default:ACTIVE" json:"status"`
	Roles        []Role     `gorm:"many2many:user_roles" json:"roles"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type Role struct {
	ID   uint   `gorm:"primaryKey" json:"-"`
	Code string `gorm:"size:32;uniqueIndex;not null" json:"code"`
	Name string `gorm:"size:64;not null" json:"name"`
}

type RefreshToken struct {
	ID         string     `gorm:"type:char(36);primaryKey"`
	UserID     string     `gorm:"type:char(36);not null;index"`
	FamilyID   string     `gorm:"type:char(36);not null;index"`
	TokenHash  string     `gorm:"type:char(64);uniqueIndex;not null"`
	ExpiresAt  time.Time  `gorm:"not null;index"`
	RevokedAt  *time.Time `gorm:"index"`
	ReplacedBy string     `gorm:"type:char(36)"`
	CreatedAt  time.Time
}

type Claims struct {
	Roles []string `json:"roles"`
	jwt.RegisteredClaims
}

type Service struct {
	db  *gorm.DB
	cfg Config
}

type Handler struct {
	service *Service
	cfg     Config
}

type credentialsRequest struct {
	Username   string `json:"username"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	Identifier string `json:"identifier"`
}

type authResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	User        User   `json:"user"`
}

func New(db *gorm.DB, cfg Config) (*Handler, error) {
	if len(cfg.JWTSecret) < 32 {
		return nil, errors.New("JWT_SECRET must contain at least 32 characters")
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "aigc-3d-platform"
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	if cfg.RefreshCookie == "" {
		cfg.RefreshCookie = "refresh_token"
	}
	if err := db.AutoMigrate(&User{}, &Role{}, &RefreshToken{}); err != nil {
		return nil, err
	}
	for _, role := range []Role{{Code: "USER", Name: "普通用户"}, {Code: "ADMIN", Name: "管理员"}} {
		if err := db.Where("code = ?", role.Code).FirstOrCreate(&role).Error; err != nil {
			return nil, err
		}
	}
	service := &Service{db: db, cfg: cfg}
	return &Handler{service: service, cfg: cfg}, nil
}

func (h *Handler) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/auth/register", h.register)
	group.POST("/auth/login", h.login)
	group.POST("/auth/refresh", h.refresh)
	group.POST("/auth/logout", h.logout)
	group.GET("/auth/me", h.Authenticate(), h.me)
	group.GET("/admin/access-check", h.Authenticate(), RequireRole("ADMIN"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}

func (h *Handler) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			abort(c, http.StatusUnauthorized, "UNAUTHORIZED", "需要登录")
			return
		}
		claims, err := h.service.parseAccessToken(strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			abort(c, http.StatusUnauthorized, "INVALID_TOKEN", "登录状态已失效")
			return
		}
		var user User
		if err := h.service.db.Preload("Roles").First(&user, "id = ? AND status = ?", claims.Subject, "ACTIVE").Error; err != nil {
			abort(c, http.StatusUnauthorized, "UNAUTHORIZED", "登录状态已失效")
			return
		}
		c.Set(userContextKey, &user)
		c.Next()
	}
}

func RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := CurrentUser(c)
		if !ok {
			abort(c, http.StatusUnauthorized, "UNAUTHORIZED", "需要登录")
			return
		}
		for _, assigned := range user.Roles {
			if assigned.Code == role {
				c.Next()
				return
			}
		}
		abort(c, http.StatusForbidden, "FORBIDDEN", "没有访问权限")
	}
}

func CurrentUser(c *gin.Context) (*User, bool) {
	value, ok := c.Get(userContextKey)
	if !ok {
		return nil, false
	}
	user, ok := value.(*User)
	return user, ok
}

func (h *Handler) ResolveUser(c *gin.Context) (*User, bool) {
	if user, ok := CurrentUser(c); ok {
		return user, true
	}
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, false
	}
	claims, err := h.service.parseAccessToken(strings.TrimPrefix(header, "Bearer "))
	if err != nil {
		return nil, false
	}
	var user User
	if err := h.service.db.Preload("Roles").First(&user, "id = ? AND status = ?", claims.Subject, "ACTIVE").Error; err != nil {
		return nil, false
	}
	c.Set(userContextKey, &user)
	return &user, true
}

func (h *Handler) register(c *gin.Context) {
	var req credentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "请求格式无效")
		return
	}
	user, access, refresh, err := h.service.Register(c.Request.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			abort(c, http.StatusConflict, "ACCOUNT_EXISTS", "用户名或邮箱已被使用")
			return
		}
		if errors.Is(err, errInvalidCredentials) {
			abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "用户名、邮箱或密码不符合要求")
			return
		}
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "注册失败")
		return
	}
	h.setRefreshCookie(c, refresh)
	c.JSON(http.StatusCreated, authResponse{AccessToken: access, ExpiresIn: int64(h.cfg.AccessTTL.Seconds()), User: *user})
}

func (h *Handler) login(c *gin.Context) {
	var req credentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "请求格式无效")
		return
	}
	user, access, refresh, err := h.service.Login(c.Request.Context(), req.Identifier, req.Password)
	if err != nil {
		abort(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "账号或密码错误")
		return
	}
	h.setRefreshCookie(c, refresh)
	c.JSON(http.StatusOK, authResponse{AccessToken: access, ExpiresIn: int64(h.cfg.AccessTTL.Seconds()), User: *user})
}

func (h *Handler) refresh(c *gin.Context) {
	token, err := c.Cookie(h.cfg.RefreshCookie)
	if err != nil {
		abort(c, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "刷新凭证无效")
		return
	}
	user, access, refresh, err := h.service.Refresh(c.Request.Context(), token)
	if err != nil {
		h.clearRefreshCookie(c)
		abort(c, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "刷新凭证无效")
		return
	}
	h.setRefreshCookie(c, refresh)
	c.JSON(http.StatusOK, authResponse{AccessToken: access, ExpiresIn: int64(h.cfg.AccessTTL.Seconds()), User: *user})
}

func (h *Handler) logout(c *gin.Context) {
	if token, err := c.Cookie(h.cfg.RefreshCookie); err == nil {
		_ = h.service.Revoke(c.Request.Context(), token)
	}
	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}

func (h *Handler) me(c *gin.Context) {
	user, _ := CurrentUser(c)
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *Handler) setRefreshCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.cfg.RefreshCookie, token, int(h.cfg.RefreshTTL.Seconds()), "/api/v1/auth", "", h.cfg.CookieSecure, true)
}
func (h *Handler) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.cfg.RefreshCookie, "", -1, "/api/v1/auth", "", h.cfg.CookieSecure, true)
}

func (s *Service) Register(_ context.Context, username, email, password string) (*User, string, string, error) {
	username, email = strings.TrimSpace(username), strings.ToLower(strings.TrimSpace(email))
	if len(username) < 3 || len(username) > 32 || len(password) < 8 || (email != "" && !strings.Contains(email, "@")) {
		return nil, "", "", errInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", "", err
	}
	var user User
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var role Role
		if err := tx.Where("code = ?", "USER").First(&role).Error; err != nil {
			return err
		}
		user = User{ID: uuid.NewString(), Username: username, PasswordHash: string(hash), Status: "ACTIVE", Roles: []Role{role}}
		if email != "" {
			user.Email = &email
		}
		return tx.Create(&user).Error
	})
	if err != nil {
		return nil, "", "", err
	}
	return s.issueSession(&user, uuid.NewString())
}

func (s *Service) Login(_ context.Context, identifier, password string) (*User, string, string, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	var user User
	if err := s.db.Preload("Roles").Where("LOWER(username) = ? OR LOWER(email) = ?", identifier, identifier).First(&user).Error; err != nil {
		return nil, "", "", errInvalidCredentials
	}
	if user.Status != "ACTIVE" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, "", "", errInvalidCredentials
	}
	now := time.Now()
	s.db.Model(&user).Update("last_login_at", now)
	user.LastLoginAt = &now
	return s.issueSession(&user, uuid.NewString())
}

func (s *Service) Refresh(_ context.Context, raw string) (*User, string, string, error) {
	hash := tokenHash(raw)
	var user User
	var newRaw string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var stored RefreshToken
		if err := tx.Where("token_hash = ?", hash).First(&stored).Error; err != nil {
			return errInvalidCredentials
		}
		if stored.RevokedAt != nil || time.Now().After(stored.ExpiresAt) {
			now := time.Now()
			tx.Model(&RefreshToken{}).Where("family_id = ? AND revoked_at IS NULL", stored.FamilyID).Update("revoked_at", now)
			return errInvalidCredentials
		}
		if err := tx.Preload("Roles").First(&user, "id = ? AND status = ?", stored.UserID, "ACTIVE").Error; err != nil {
			return errInvalidCredentials
		}
		var err error
		newRaw, err = randomToken()
		if err != nil {
			return err
		}
		replacement := RefreshToken{ID: uuid.NewString(), UserID: user.ID, FamilyID: stored.FamilyID, TokenHash: tokenHash(newRaw), ExpiresAt: time.Now().Add(s.cfg.RefreshTTL)}
		now := time.Now()
		if result := tx.Model(&RefreshToken{}).Where("id = ? AND revoked_at IS NULL", stored.ID).Updates(map[string]any{"revoked_at": now, "replaced_by": replacement.ID}); result.Error != nil || result.RowsAffected != 1 {
			return errInvalidCredentials
		}
		return tx.Create(&replacement).Error
	})
	if err != nil {
		return nil, "", "", err
	}
	access, err := s.createAccessToken(&user)
	if err != nil {
		return nil, "", "", err
	}
	return &user, access, newRaw, nil
}

func (s *Service) Revoke(_ context.Context, raw string) error {
	now := time.Now()
	return s.db.Model(&RefreshToken{}).Where("token_hash = ? AND revoked_at IS NULL", tokenHash(raw)).Update("revoked_at", now).Error
}

func (s *Service) issueSession(user *User, familyID string) (*User, string, string, error) {
	access, err := s.createAccessToken(user)
	if err != nil {
		return nil, "", "", err
	}
	raw, err := randomToken()
	if err != nil {
		return nil, "", "", err
	}
	stored := RefreshToken{ID: uuid.NewString(), UserID: user.ID, FamilyID: familyID, TokenHash: tokenHash(raw), ExpiresAt: time.Now().Add(s.cfg.RefreshTTL)}
	if err := s.db.Create(&stored).Error; err != nil {
		return nil, "", "", err
	}
	return user, access, raw, nil
}

func (s *Service) createAccessToken(user *User) (string, error) {
	now := time.Now()
	roles := make([]string, 0, len(user.Roles))
	for _, role := range user.Roles {
		roles = append(roles, role.Code)
	}
	claims := Claims{Roles: roles, RegisteredClaims: jwt.RegisteredClaims{Issuer: s.cfg.Issuer, Subject: user.ID, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.AccessTTL)), ID: uuid.NewString()}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}

func (s *Service) parseAccessToken(raw string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(raw, &Claims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.cfg.JWTSecret), nil
	}, jwt.WithIssuer(s.cfg.Issuer), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return nil, errInvalidCredentials
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, errInvalidCredentials
	}
	return claims, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func tokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func abort(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message, "request_id": c.GetString("request_id")}})
}
