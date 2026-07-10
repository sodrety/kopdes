package app

import (
	"database/sql"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	cfg             Config
	db              *sql.DB
	instrumentation *instrumentation
	financialMu     sync.Mutex
	loginMu         sync.Mutex
	loginStates     map[string]loginState
}

func NewServer(cfg Config, db *sql.DB) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	server := &Server{
		cfg:             cfg,
		db:              db,
		instrumentation: newInstrumentation(cfg),
		loginStates:     make(map[string]loginState),
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(server.requestID())
	router.Use(server.observeRequests())
	router.Use(server.securityHeaders())
	router.Use(server.requireSameOriginForCookieMutations())
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/ready", server.readiness)
	if cfg.MetricsEnabled {
		router.GET("/metrics", gin.WrapH(promhttp.HandlerFor(server.instrumentation.registry, promhttp.HandlerOpts{})))
	}
	router.GET("/static/app.css", server.staticCSS)
	router.GET("/static/vendor/*file", server.staticVendorAsset)
	router.GET("/static/images/*file", server.staticImageAsset)
	router.GET("/", server.homePage)
	router.GET("/login", server.loginPage)
	router.POST("/language", server.setLanguage)
	router.POST("/api/auth/login", server.login)
	router.POST("/logout", server.logout)

	admin := router.Group("/api/admin")
	admin.Use(server.requireRole("admin"))
	admin.GET("/dashboard", server.adminDashboard)
	admin.GET("/reports", server.adminReports)
	admin.POST("/members", server.createMember)
	admin.GET("/members", server.listMembers)
	admin.GET("/members/:id", server.getMember)
	admin.POST("/members/:id/user", server.createMemberUser)
	admin.POST("/savings", server.recordSaving)
	admin.GET("/savings", server.adminSavings)
	admin.GET("/withdrawal-requests", server.adminWithdrawalRequests)
	admin.POST("/withdrawal-requests/:id/approve", server.approveWithdrawalRequest)
	admin.POST("/withdrawal-requests/:id/reject", server.rejectWithdrawalRequest)
	admin.GET("/loan-requests", server.adminLoanRequests)
	admin.POST("/loan-requests/:id/approve", server.approveLoanRequest)
	admin.POST("/loan-requests/:id/reject", server.rejectLoanRequest)
	admin.GET("/loans", server.adminLoans)
	admin.POST("/loans/:id/repayments", server.recordLoanRepayment)
	admin.GET("/exports/savings.csv", server.exportSavingsCSV)
	admin.GET("/exports/withdrawal-requests.csv", server.exportWithdrawalRequestsCSV)
	admin.GET("/exports/loans.csv", server.exportLoansCSV)
	admin.GET("/exports/repayments.csv", server.exportRepaymentsCSV)

	member := router.Group("/api/member")
	member.Use(server.requireRole("member"))
	member.GET("/profile", server.memberProfile)
	member.GET("/savings", server.memberSavings)
	member.GET("/savings/summary", server.memberSavingSummary)
	member.POST("/withdrawal-requests", server.submitWithdrawalRequest)
	member.GET("/withdrawal-requests", server.memberWithdrawalRequests)
	member.GET("/dashboard", server.memberDashboard)
	member.POST("/loan-requests", server.submitLoanRequest)
	member.GET("/loan-requests", server.memberLoanRequests)
	member.GET("/loans/active", server.memberActiveLoan)
	member.GET("/repayments", server.memberRepayments)

	router.GET("/admin/dashboard", server.requireRole("admin"), server.adminDashboardPage)
	router.GET("/admin/reports", server.requireRole("admin"), server.adminReportsPage)
	router.GET("/admin/reports/balance", server.requireRole("admin"), server.adminBalanceReportPage)
	router.GET("/admin/reports/profit-loss", server.requireRole("admin"), server.adminProfitLossReportPage)
	router.GET("/admin/members", server.requireRole("admin"), server.adminMembersPage)
	router.GET("/admin/members/new", server.requireRole("admin"), server.adminMemberNewPage)
	router.GET("/admin/members/:id", server.requireRole("admin"), server.adminMemberDetailPage)
	router.GET("/admin/savings", server.requireRole("admin"), server.adminSavingsPage)
	router.GET("/admin/savings/new", server.requireRole("admin"), server.adminSavingNewPage)
	router.GET("/admin/withdrawal-requests", server.requireRole("admin"), server.adminWithdrawalRequestsPage)
	router.GET("/admin/loan-requests", server.requireRole("admin"), server.adminLoanRequestsPage)
	router.GET("/admin/loans", server.requireRole("admin"), server.adminLoansPage)
	router.GET("/admin/repayments", server.requireRole("admin"), server.adminRepaymentsPage)
	router.GET("/member/dashboard", server.requireRole("member"), server.memberDashboardPage)
	router.GET("/member/profile", server.requireRole("member"), server.memberProfilePage)
	router.GET("/member/withdrawal-requests", server.requireRole("member"), server.memberWithdrawalRequestsPage)
	router.GET("/member/loan-requests", server.requireRole("member"), server.memberLoanRequestsPage)

	return router
}

func (s *Server) securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.Writer.Header()
		header.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "same-origin")
		c.Next()
	}
}

func (s *Server) requireSameOriginForCookieMutations() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isUnsafeMethod(c.Request.Method) || bearerToken(c.GetHeader("Authorization")) != "" {
			c.Next()
			return
		}
		if _, err := c.Cookie("auth_token"); err != nil {
			c.Next()
			return
		}
		if sameOriginRequest(c.Request) {
			c.Next()
			return
		}
		respondError(c, http.StatusForbidden, "FORBIDDEN", "Same-origin browser request is required")
		c.Abort()
	}
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func sameOriginRequest(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		return originMatchesHost(origin, r.Host)
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		return originMatchesHost(referer, r.Host)
	}
	return false
}

func originMatchesHost(raw, host string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, host)
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

	email := strings.ToLower(strings.TrimSpace(req.Email))
	loginKey := loginThrottleKey(c, email)
	if s.loginBlocked(loginKey) {
		respondError(c, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "Too many failed login attempts. Try again later")
		return
	}

	user, err := AuthenticateUser(s.db, email, req.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		s.recordFailedLogin(loginKey)
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid email or password")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	s.clearFailedLogin(loginKey)

	token, err := SignToken(s.cfg.JWTSecret, user)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	redirectPath := "/admin/dashboard"
	if user.Role == "member" {
		redirectPath = "/member/dashboard"
	}

	if isHTMXRequest(c) {
		s.setAuthCookie(c, token)
		respondHXRedirect(c, redirectPath)
		return
	}

	if isBrowserFormRequest(c) {
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
	// #nosec G124 -- Cookie Secure is controlled by deployment config so local HTTP tests can exercise browser flows.
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
	// #nosec G124 -- Cookie Secure is controlled by deployment config so local HTTP tests can exercise browser flows.
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

		tokenUser, err := ParseToken(s.cfg.JWTSecret, tokenValue)
		if err != nil {
			respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid authentication token")
			c.Abort()
			return
		}
		user, err := s.validateSessionUser(tokenUser)
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
		if role == "member" {
			if err := s.validateMemberSession(tokenUser, user); err != nil {
				respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid authentication token")
				c.Abort()
				return
			}
		}
		c.Set("user", user)
		c.Next()
	}
}

const (
	maxFailedLoginAttempts = 5
	loginLockDuration      = 15 * time.Minute
)

type loginState struct {
	Count       int
	LockedUntil time.Time
}

func loginThrottleKey(c *gin.Context, email string) string {
	return email + "|" + c.ClientIP()
}

func (s *Server) loginBlocked(key string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	state, ok := s.loginStates[key]
	if !ok {
		return false
	}
	if !state.LockedUntil.IsZero() && time.Now().Before(state.LockedUntil) {
		return true
	}
	if !state.LockedUntil.IsZero() {
		delete(s.loginStates, key)
	}
	return false
}

func (s *Server) recordFailedLogin(key string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	state := s.loginStates[key]
	state.Count++
	if state.Count >= maxFailedLoginAttempts {
		state.LockedUntil = time.Now().Add(loginLockDuration)
	}
	s.loginStates[key] = state
}

func (s *Server) clearFailedLogin(key string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	delete(s.loginStates, key)
}

func (s *Server) validateSessionUser(tokenUser User) (User, error) {
	current, err := UserByID(s.db, tokenUser.ID)
	if err != nil {
		return User{}, err
	}
	if current.Role != tokenUser.Role || !strings.EqualFold(current.Email, tokenUser.Email) {
		return User{}, ErrUnauthorized
	}
	return current, nil
}

func (s *Server) validateMemberSession(tokenUser, current User) error {
	if !current.MemberID.Valid || !tokenUser.MemberID.Valid || current.MemberID.String != tokenUser.MemberID.String {
		return ErrUnauthorized
	}
	_, err := s.memberByID(current.MemberID.String)
	return err
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
	if isHTMXRequest(c) {
		body := `<span class="form-error-message">` + template.HTMLEscapeString(localizedErrorMessage(c, message)) + `</span>`
		c.Data(status, "text/html; charset=utf-8", []byte(body))
		return
	}
	c.JSON(status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func isHTMXRequest(c *gin.Context) bool {
	return c.GetHeader("HX-Request") == "true"
}

func isBrowserFormRequest(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Content-Type"), "application/x-www-form-urlencoded")
}

func respondHXRedirect(c *gin.Context, path string) {
	c.Header("HX-Redirect", path)
	c.Status(http.StatusNoContent)
}

func respondCreatedOrHXRedirect(c *gin.Context, redirectPath string, body any) {
	if isHTMXRequest(c) {
		respondHXRedirect(c, redirectPath)
		return
	}
	c.JSON(http.StatusCreated, body)
}

func respondOKOrHXRedirect(c *gin.Context, redirectPath string, body any) {
	if isHTMXRequest(c) {
		respondHXRedirect(c, redirectPath)
		return
	}
	c.JSON(http.StatusOK, body)
}
