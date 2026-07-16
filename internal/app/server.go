package app

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func rowLockClause(db *sql.DB) string {
	if strings.Contains(strings.ToLower(fmt.Sprintf("%T", db.Driver())), "sqlite") {
		return ""
	}
	return " FOR UPDATE"
}

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
	router.HandleMethodNotAllowed = true
	router.Use(gin.Recovery())
	router.Use(server.requestID())
	if cfg.TracingEnabled {
		router.Use(otelgin.Middleware(cfg.observabilityServiceName()))
	}
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
	router.NoRoute(func(c *gin.Context) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Page not found")
	})
	router.NoMethod(func(c *gin.Context) {
		respondError(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	})

	admin := router.Group("/api/admin")
	admin.GET("/dashboard", server.requirePermission(PermissionDashboardView), server.adminDashboard)
	admin.GET("/reports", server.requirePermission(PermissionReportsView), server.adminReports)
	admin.POST("/members", server.requirePermission(PermissionMembersManage), server.createMember)
	admin.GET("/members", server.requirePermission(PermissionMembersView), server.listMembers)
	admin.GET("/members/:id", server.requirePermission(PermissionMembersView), server.getMember)
	admin.POST("/members/:id/type", server.requirePermission(PermissionMembersManage), server.updateMemberType)
	admin.POST("/members/:id/user", server.requirePermission(PermissionMembersManage), server.createMemberUser)
	admin.POST("/members/:id/user/update", server.requirePermission(PermissionMembersManage), server.updateMemberUser)
	admin.POST("/members/:id/user/reset-password", server.requirePermission(PermissionMembersManage), server.resetMemberPassword)
	admin.POST("/savings", server.requirePermission(PermissionSavingsRecord), server.recordSaving)
	admin.GET("/savings", server.requirePermission(PermissionSavingsView), server.adminSavings)
	admin.GET("/withdrawal-requests", server.requirePermission(PermissionRequestsView), server.adminWithdrawalRequests)
	admin.POST("/withdrawal-requests/:id/approve", server.requirePermission(PermissionRequestsDecide), server.approveWithdrawalRequest)
	admin.POST("/withdrawal-requests/:id/reject", server.requirePermission(PermissionRequestsDecide), server.rejectWithdrawalRequest)
	admin.GET("/loan-requests", server.requirePermission(PermissionRequestsView), server.adminLoanRequests)
	admin.POST("/loan-requests/:id/approve", server.requirePermission(PermissionRequestsDecide), server.approveLoanRequest)
	admin.POST("/loan-requests/:id/reject", server.requirePermission(PermissionRequestsDecide), server.rejectLoanRequest)
	admin.GET("/loans", server.requirePermission(PermissionLoansView), server.adminLoans)
	admin.GET("/loans/:id", server.requirePermission(PermissionLoansView), server.adminLoanDetail)
	admin.POST("/loans/:id/repayments", server.requirePermission(PermissionRepaymentsRecord), server.recordLoanRepayment)
	admin.GET("/exports/savings.csv", server.requirePermission(PermissionReportsView), server.exportSavingsCSV)
	admin.GET("/exports/withdrawal-requests.csv", server.requirePermission(PermissionReportsView), server.exportWithdrawalRequestsCSV)
	admin.GET("/exports/loans.csv", server.requirePermission(PermissionReportsView), server.exportLoansCSV)
	admin.GET("/loans/:id/export.pdf", server.requirePermission(PermissionReportsView), server.exportLoanPDF)
	admin.GET("/exports/repayments.csv", server.requirePermission(PermissionReportsView), server.exportRepaymentsCSV)
	admin.GET("/tagihan", server.requireTagihanManage(), server.adminTagihan)
	admin.GET("/tagihan/export.xlsx", server.requireTagihanManage(), server.exportTagihanXLSX)
	admin.POST("/tagihan/import", server.requireTagihanManage(), server.importTagihanXLSX)
	admin.GET("/officers", server.requirePermission(PermissionOfficersManage), server.listOfficers)
	admin.POST("/officers", server.requirePermission(PermissionOfficersManage), server.createOfficer)
	admin.POST("/officers/:id/update", server.requirePermission(PermissionOfficersManage), server.updateOfficer)
	admin.POST("/officers/:id/reset-password", server.requirePermission(PermissionOfficersManage), server.resetOfficerPassword)

	member := router.Group("/api/member")
	member.Use(server.requireRole("member"))
	member.GET("/profile", server.memberProfile)
	member.GET("/savings", server.memberSavings)
	member.GET("/savings/summary", server.memberSavingSummary)
	member.POST("/withdrawal-requests", server.submitWithdrawalRequest)
	member.GET("/withdrawal-requests", server.memberWithdrawalRequests)
	member.POST("/withdrawal-requests/:id/cancel", server.cancelWithdrawalRequest)
	member.GET("/dashboard", server.memberDashboard)
	member.POST("/loan-requests", server.submitLoanRequest)
	member.GET("/loan-requests", server.memberLoanRequests)
	member.POST("/loan-requests/:id/cancel", server.cancelLoanRequest)
	member.GET("/loans/active", server.memberActiveLoan)
	member.GET("/loans/outstanding", server.memberOutstandingLoans)
	member.GET("/repayments", server.memberRepayments)
	router.POST("/api/account/password", server.requireAuthenticated(), server.changePassword)
	router.GET("/api/notifications", server.requireAuthenticated(), server.listNotifications)
	router.POST("/api/notifications/:id/read", server.requireAuthenticated(), server.markNotificationRead)
	router.GET("/password/change", server.requireAuthenticated(), server.passwordChangePage)
	router.GET("/notifications", server.requireAuthenticated(), server.notificationsPage)
	router.GET("/member/notifications", server.requireRole("member"), server.notificationsPage)
	router.GET("/admin/notifications", server.requirePermission(PermissionDashboardView), server.notificationsPage)

	router.GET("/admin/dashboard", server.requirePermission(PermissionDashboardView), server.adminDashboardPage)
	router.GET("/admin/reports", server.requirePermission(PermissionReportsView), server.adminReportsPage)
	router.GET("/admin/reports/balance", server.requirePermission(PermissionReportsView), server.adminBalanceReportPage)
	router.GET("/admin/reports/profit-loss", server.requirePermission(PermissionReportsView), server.adminProfitLossReportPage)
	router.GET("/admin/members", server.requirePermission(PermissionMembersView), server.adminMembersPage)
	router.GET("/admin/members/new", server.requirePermission(PermissionMembersManage), server.adminMemberNewPage)
	router.GET("/admin/members/:id", server.requirePermission(PermissionMembersView), server.adminMemberDetailPage)
	router.GET("/admin/savings", server.requirePermission(PermissionSavingsView), server.adminSavingsPage)
	router.GET("/admin/savings/new", server.requirePermission(PermissionSavingsRecord), server.adminSavingNewPage)
	router.GET("/admin/withdrawal-requests", server.requirePermission(PermissionRequestsView), server.adminWithdrawalRequestsPage)
	router.GET("/admin/loan-requests", server.requirePermission(PermissionRequestsView), server.adminLoanRequestsPage)
	router.GET("/admin/loans", server.requirePermission(PermissionLoansView), server.adminLoansPage)
	router.GET("/admin/loans/:id", server.requirePermission(PermissionLoansView), server.adminLoanDetailPage)
	router.GET("/admin/repayments", server.requirePermission(PermissionRepaymentsView), server.adminRepaymentsPage)
	router.GET("/admin/transactions", server.requirePermission(PermissionReportsView), server.adminTransactionsPage)
	router.GET("/admin/tagihan", server.requireTagihanManage(), server.adminTagihanPage)
	router.GET("/admin/officers", server.requirePermission(PermissionOfficersManage), func(c *gin.Context) { c.Redirect(http.StatusSeeOther, "/admin/members") })
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
		respondJSONError(c, http.StatusForbidden, "FORBIDDEN", "Same-origin browser request is required")
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

	redirectPath := "/member/dashboard"
	if user.MustChangePassword {
		redirectPath = "/password/change"
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
		"id":                   user.ID,
		"email":                user.Email,
		"role":                 user.Role,
		"full_name":            user.FullName,
		"must_change_password": user.MustChangePassword,
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
		user, ok := s.authenticateRequest(c)
		if !ok {
			return
		}
		if role == "member" && !user.MemberID.Valid {
			respondError(c, http.StatusForbidden, "FORBIDDEN", "Insufficient role")
			c.Abort()
			return
		}
		if role != "member" && user.Role != role {
			respondError(c, http.StatusForbidden, "FORBIDDEN", "Insufficient role")
			c.Abort()
			return
		}
		c.Set("user", user)
		s.decorateAuthenticatedContext(c, user)
		c.Next()
	}
}

func (s *Server) respondUnauthorized(c *gin.Context, usesBearerToken bool, message string, clearCookie bool) {
	if shouldRedirectAuthFailure(c, usesBearerToken) {
		if clearCookie {
			s.clearAuthCookie(c)
		}
		if isHTMXRequest(c) {
			respondHXRedirect(c, "/login")
		} else {
			c.Redirect(http.StatusSeeOther, "/login")
		}
		c.Abort()
		return
	}
	respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", message)
	c.Abort()
}

func shouldRedirectAuthFailure(c *gin.Context, usesBearerToken bool) bool {
	return !usesBearerToken && wantsBrowserResponse(c)
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
	if !current.Active || current.MemberStatus != "active" || current.Role != tokenUser.Role || !strings.EqualFold(current.Email, tokenUser.Email) {
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
	localizedMessage := localizedErrorMessage(c, message)
	if isHTMXRequest(c) {
		body := `<span class="form-error-message">` + template.HTMLEscapeString(localizedMessage) + `</span>`
		c.Data(status, "text/html; charset=utf-8", []byte(body))
		return
	}
	if wantsBrowserDocument(c) {
		respondBrowserError(c, status, code, localizedMessage)
		return
	}
	// Preserve the established JSON contract for canonical/raw messages. New
	// callers can pass an explicit catalog key and receive one translation.
	if catalog, ok := translations[languageFromRequest(c)]; ok {
		if _, explicitKey := catalog[message]; explicitKey {
			message = localizedMessage
		}
	}
	respondJSONError(c, status, code, message)
}

func respondJSONError(c *gin.Context, status int, code, message string) {
	c.JSON(status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func isHTMXRequest(c *gin.Context) bool {
	return c.GetHeader("HX-Request") == "true"
}

func isBrowserFormRequest(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Content-Type"), "application/x-www-form-urlencoded")
}

func wantsBrowserResponse(c *gin.Context) bool {
	return isHTMXRequest(c) || wantsBrowserDocument(c)
}

func wantsBrowserDocument(c *gin.Context) bool {
	if isBrowserFormRequest(c) || !strings.HasPrefix(c.Request.URL.Path, "/api/") {
		return true
	}
	if strings.EqualFold(c.GetHeader("Sec-Fetch-Mode"), "navigate") || strings.EqualFold(c.GetHeader("Sec-Fetch-Dest"), "document") {
		return true
	}
	return strings.Contains(strings.ToLower(c.GetHeader("Accept")), "text/html")
}

func respondBrowserError(c *gin.Context, status int, code, message string) {
	lang := languageFromRequest(c)
	backPath := browserErrorBackPath(c)
	data := gin.H{
		"Lang":        lang,
		"Title":       translate(lang, "error_page_title"),
		"Status":      status,
		"Code":        code,
		"Message":     localizedErrorMessage(c, message),
		"BackPath":    backPath,
		"CurrentPath": backPath,
	}
	var body strings.Builder
	if err := pageTemplates.ExecuteTemplate(&body, "error-page", data); err != nil {
		c.Data(status, "text/plain; charset=utf-8", []byte(localizedErrorMessage(c, message)))
		return
	}
	c.Data(status, "text/html; charset=utf-8", []byte(body.String()))
}

func browserErrorBackPath(c *gin.Context) string {
	if c.Request.URL.Path == "/api/auth/login" {
		return "/login"
	}
	referer := c.GetHeader("Referer")
	if referer == "" {
		return "/"
	}
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Host, c.Request.Host) || parsed.Path == "" {
		return "/"
	}
	return parsed.RequestURI()
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
	if isBrowserFormRequest(c) {
		c.Redirect(http.StatusSeeOther, redirectPath)
		return
	}
	c.JSON(http.StatusCreated, body)
}

func respondOKOrHXRedirect(c *gin.Context, redirectPath string, body any) {
	if isHTMXRequest(c) {
		respondHXRedirect(c, redirectPath)
		return
	}
	if isBrowserFormRequest(c) {
		c.Redirect(http.StatusSeeOther, redirectPath)
		return
	}
	c.JSON(http.StatusOK, body)
}
