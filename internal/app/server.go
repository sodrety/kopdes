package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Server struct {
	cfg Config
	db  *sql.DB
}

func NewServer(cfg Config, db *sql.DB) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	server := &Server{cfg: cfg, db: db}

	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/static/app.css", server.staticCSS)
	router.GET("/login", server.loginPage)
	router.POST("/api/auth/login", server.login)
	router.POST("/logout", server.logout)

	admin := router.Group("/api/admin")
	admin.Use(server.requireRole("admin"))
	admin.GET("/dashboard", server.adminDashboard)
	admin.POST("/members", server.createMember)
	admin.GET("/members", server.listMembers)
	admin.GET("/members/:id", server.getMember)
	admin.POST("/members/:id/user", server.createMemberUser)
	admin.POST("/savings", server.recordSaving)
	admin.GET("/loan-requests", server.adminLoanRequests)
	admin.POST("/loan-requests/:id/approve", server.approveLoanRequest)
	admin.POST("/loan-requests/:id/reject", server.rejectLoanRequest)
	admin.GET("/loans", server.adminLoans)
	admin.POST("/loans/:id/repayments", server.recordLoanRepayment)

	member := router.Group("/api/member")
	member.Use(server.requireRole("member"))
	member.GET("/profile", server.memberProfile)
	member.GET("/savings", server.memberSavings)
	member.GET("/savings/summary", server.memberSavingSummary)
	member.GET("/dashboard", server.memberDashboard)
	member.POST("/loan-requests", server.submitLoanRequest)
	member.GET("/loan-requests", server.memberLoanRequests)
	member.GET("/loans/active", server.memberActiveLoan)
	member.GET("/repayments", server.memberRepayments)

	router.GET("/admin/dashboard", server.requireRole("admin"), server.adminDashboardPage)
	router.GET("/admin/members", server.requireRole("admin"), server.adminMembersPage)
	router.GET("/admin/members/:id", server.requireRole("admin"), server.adminMemberDetailPage)
	router.GET("/admin/savings", server.requireRole("admin"), server.adminSavingsPage)
	router.GET("/admin/loan-requests", server.requireRole("admin"), server.adminLoanRequestsPage)
	router.GET("/admin/loans", server.requireRole("admin"), server.adminLoansPage)
	router.GET("/admin/repayments", server.requireRole("admin"), server.adminRepaymentsPage)
	router.GET("/member/profile", server.requireRole("member"), server.memberProfilePage)
	router.GET("/member/loan-requests", server.requireRole("member"), server.memberLoanRequestsPage)

	return router
}

func (s *Server) logout(c *gin.Context) {
	s.clearAuthCookie(c)
	c.Redirect(http.StatusSeeOther, "/login")
}

func (s *Server) login(c *gin.Context) {
	var req struct {
		Email    string `json:"email" form:"email"`
		Password string `json:"password" form:"password"`
	}
	if err := c.ShouldBind(&req); err != nil || req.Email == "" || req.Password == "" {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Email and password are required")
		return
	}

	user, err := AuthenticateUser(s.db, req.Email, req.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid email or password")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	token, err := SignToken(s.cfg.JWTSecret, user)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	redirectPath := "/admin/dashboard"
	if user.Role == "member" {
		redirectPath = "/member/profile"
	}

	if c.GetHeader("HX-Request") == "true" {
		s.setAuthCookie(c, token)
		c.Header("HX-Redirect", redirectPath)
		c.Status(http.StatusNoContent)
		return
	}

	if strings.Contains(c.GetHeader("Content-Type"), "application/x-www-form-urlencoded") {
		s.setAuthCookie(c, token)
		c.Redirect(http.StatusSeeOther, redirectPath)
		return
	}

	userBody := gin.H{
		"id":    user.ID,
		"email": user.Email,
		"role":  user.Role,
	}
	if user.MemberID.Valid {
		userBody["member_id"] = user.MemberID.String
	}
	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  userBody,
	})
}

func (s *Server) setAuthCookie(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearAuthCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) adminDashboard(c *gin.Context) {
	summary, err := s.adminDashboardSummary()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	c.JSON(http.StatusOK, summary)
}

func (s *Server) requireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenValue := bearerToken(c.GetHeader("Authorization"))
		if tokenValue == "" {
			if cookie, err := c.Cookie("auth_token"); err == nil {
				tokenValue = cookie
			}
		}
		if tokenValue == "" {
			respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
			c.Abort()
			return
		}

		user, err := ParseToken(s.cfg.JWTSecret, tokenValue)
		if err != nil {
			respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid authentication token")
			c.Abort()
			return
		}
		if user.Role != role {
			respondError(c, http.StatusForbidden, "FORBIDDEN", "Insufficient role")
			c.Abort()
			return
		}
		c.Set("user", user)
		c.Next()
	}
}

func currentUser(c *gin.Context) (User, bool) {
	value, ok := c.Get("user")
	if !ok {
		return User{}, false
	}
	user, ok := value.(User)
	return user, ok
}

func bearerToken(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func respondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, errorBody{Error: errorDetail{Code: code, Message: message}})
}
