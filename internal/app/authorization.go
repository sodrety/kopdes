package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Permission string

const (
	PermissionDashboardView               Permission = "dashboard.view"
	PermissionReportsView                 Permission = "reports.view"
	PermissionMembersView                 Permission = "members.view"
	PermissionMembersManage               Permission = "members.manage"
	PermissionMemberAccountsManage        Permission = "member_accounts.manage"
	PermissionSavingsView                 Permission = "savings.view"
	PermissionSavingsRecord               Permission = "savings.record"
	PermissionRequestsView                Permission = "requests.view"
	PermissionRequestsManage              Permission = "requests.manage"
	PermissionRequestsCreateOnBehalf      Permission = "requests.create_on_behalf"
	PermissionWithdrawalRequestsManage    Permission = "withdrawal_requests.manage"
	PermissionRequestsDecide              Permission = "requests.decide"
	PermissionRequestsOverride            Permission = "requests.override"
	PermissionLoansView                   Permission = "loans.view"
	PermissionLoansManage                 Permission = "loans.manage"
	PermissionLoanReportsGenerate         Permission = "loan_reports.generate"
	PermissionRepaymentsView              Permission = "repayments.view"
	PermissionRepaymentsRecord            Permission = "repayments.record"
	PermissionTransactionsView            Permission = "transactions.view"
	PermissionTransactionsRecord          Permission = "transactions.record"
	PermissionTransactionsApprove         Permission = "transactions.approve"
	PermissionJournalsView                Permission = "journals.view"
	PermissionJournalsManage              Permission = "journals.manage"
	PermissionJournalsApprove             Permission = "journals.approve"
	PermissionTransactionSourceEdit       Permission = "transaction_source.edit"
	PermissionTransactionCategoriesManage Permission = "transaction_categories.manage"
	PermissionCOAView                     Permission = "coa.view"
	PermissionCOAManage                   Permission = "coa.manage"
	PermissionCOAApprove                  Permission = "coa.approve"
	PermissionAccountingMappingsManage    Permission = "accounting_mappings.manage"
	PermissionAccountingCOAOverride       Permission = "accounting_coa.override"
	PermissionOfficersManage              Permission = "officers.manage"
	PermissionTagihanView                 Permission = "tagihan.view"
	PermissionTagihanManage               Permission = "tagihan.manage"
	PermissionSettingsManage              Permission = "settings.manage"
	PermissionNotificationsView           Permission = "notifications.view"
)

var officerPermissions = map[string]map[Permission]bool{
	"admin": {
		PermissionDashboardView: true, PermissionReportsView: true,
		PermissionMembersView: true, PermissionMembersManage: true, PermissionMemberAccountsManage: true,
		PermissionSavingsView: true, PermissionSavingsRecord: true,
		PermissionRequestsView: true, PermissionRequestsManage: true, PermissionWithdrawalRequestsManage: true,
		PermissionRequestsDecide: true, PermissionRequestsCreateOnBehalf: true,
		PermissionLoansView: true, PermissionLoansManage: true, PermissionLoanReportsGenerate: true,
		PermissionRepaymentsView: true, PermissionRepaymentsRecord: true, PermissionOfficersManage: true,
		PermissionTransactionsView:         true,
		PermissionAccountingMappingsManage: true, PermissionAccountingCOAOverride: true,
		PermissionTagihanView: true, PermissionTagihanManage: true, PermissionSettingsManage: true,
		PermissionNotificationsView: true,
	},
	"manager": {
		PermissionDashboardView: true, PermissionReportsView: true,
		PermissionMembersView: true, PermissionMembersManage: true,
		PermissionSavingsView: true, PermissionSavingsRecord: true,
		PermissionRequestsView: true, PermissionRequestsManage: true, PermissionWithdrawalRequestsManage: true, PermissionRequestsDecide: true,
		PermissionLoansView: true, PermissionLoansManage: true, PermissionLoanReportsGenerate: true, PermissionRepaymentsView: true,
		PermissionRepaymentsRecord: true, PermissionNotificationsView: true,
		PermissionTransactionsView: true, PermissionTransactionsRecord: true,
		PermissionJournalsView: true, PermissionCOAView: true, PermissionTagihanView: true, PermissionTagihanManage: true,
		PermissionAccountingCOAOverride: true, PermissionTransactionCategoriesManage: true,
	},
	"bendahara": {
		PermissionRequestsView: true, PermissionWithdrawalRequestsManage: true,
		PermissionLoansView: true, PermissionLoansManage: true, PermissionLoanReportsGenerate: true,
		PermissionRepaymentsView: true, PermissionRepaymentsRecord: true,
		PermissionTransactionsView: true, PermissionTransactionsRecord: true,
		PermissionTransactionSourceEdit: true, PermissionJournalsView: true, PermissionJournalsManage: true,
		PermissionCOAView: true, PermissionCOAManage: true,
	},
	"ketua_i": func() map[Permission]bool {
		permissions := officerOversightPermissions()
		permissions[PermissionTransactionsRecord] = true
		permissions[PermissionTransactionsApprove] = true
		permissions[PermissionJournalsManage] = true
		permissions[PermissionJournalsApprove] = true
		permissions[PermissionCOAManage] = true
		permissions[PermissionCOAApprove] = true
		return permissions
	}(),
	"ketua_ii": officerOversightPermissions(),
	"ketua_utama": func() map[Permission]bool {
		permissions := officerOversightPermissions()
		permissions[PermissionMemberAccountsManage] = true
		permissions[PermissionOfficersManage] = true
		return permissions
	}(),
}

var allPermissions = []Permission{
	PermissionDashboardView,
	PermissionReportsView,
	PermissionMembersView,
	PermissionMembersManage,
	PermissionMemberAccountsManage,
	PermissionSavingsView,
	PermissionSavingsRecord,
	PermissionRequestsView,
	PermissionRequestsManage,
	PermissionRequestsCreateOnBehalf,
	PermissionWithdrawalRequestsManage,
	PermissionRequestsDecide,
	PermissionRequestsOverride,
	PermissionLoansView,
	PermissionLoansManage,
	PermissionLoanReportsGenerate,
	PermissionRepaymentsView,
	PermissionRepaymentsRecord,
	PermissionTransactionsView,
	PermissionTransactionsRecord,
	PermissionTransactionsApprove,
	PermissionJournalsView,
	PermissionJournalsManage,
	PermissionJournalsApprove,
	PermissionTransactionSourceEdit,
	PermissionTransactionCategoriesManage,
	PermissionCOAView,
	PermissionCOAManage,
	PermissionCOAApprove,
	PermissionAccountingMappingsManage,
	PermissionAccountingCOAOverride,
	PermissionOfficersManage,
	PermissionTagihanView,
	PermissionTagihanManage,
	PermissionSettingsManage,
	PermissionNotificationsView,
}

func officerOversightPermissions() map[Permission]bool {
	return map[Permission]bool{
		PermissionDashboardView: true, PermissionReportsView: true,
		PermissionMembersView: true, PermissionSavingsView: true, PermissionTagihanView: true,
		PermissionRequestsView: true, PermissionRequestsDecide: true,
		PermissionLoansView: true, PermissionRepaymentsView: true,
		PermissionTransactionsView: true, PermissionJournalsView: true, PermissionCOAView: true,
		PermissionNotificationsView: true,
	}
}

func isOfficerRole(role string) bool {
	_, ok := officerPermissions[role]
	return ok
}

func validOfficerRole(role string) bool {
	return isOfficerRole(role)
}

func hasPermission(role string, permission Permission) bool {
	if role == "super_admin" {
		return permission != PermissionSettingsManage
	}
	return officerPermissions[role][permission]
}

func permissionSet(role string) map[string]bool {
	result := map[string]bool{}
	if role == "super_admin" {
		for _, permission := range allPermissions {
			if hasPermission(role, permission) {
				result[string(permission)] = true
			}
		}
		return result
	}
	for permission, allowed := range officerPermissions[role] {
		if allowed {
			result[string(permission)] = true
		}
	}
	return result
}

func (s *Server) requirePermission(permission Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := s.authenticateRequest(c)
		if !ok {
			return
		}
		if user.MustChangePassword {
			if wantsBrowserResponse(c) {
				if isHTMXRequest(c) {
					respondHXRedirect(c, "/password/change")
				} else {
					c.Redirect(http.StatusSeeOther, "/password/change")
				}
				c.Abort()
				return
			}
			respondError(c, http.StatusForbidden, "PASSWORD_CHANGE_REQUIRED", "Password change is required")
			c.Abort()
			return
		}
		if !hasPermission(user.Role, permission) {
			respondError(c, http.StatusForbidden, "FORBIDDEN", "Insufficient permission")
			c.Abort()
			return
		}
		c.Set("user", user)
		s.decorateAuthenticatedContext(c, user)
		c.Next()
	}
}

func (s *Server) requireAuthenticated() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := s.authenticateRequest(c)
		if !ok {
			return
		}
		c.Set("user", user)
		s.decorateAuthenticatedContext(c, user)
		c.Next()
	}
}

func (s *Server) decorateAuthenticatedContext(c *gin.Context, user User) {
	c.Set("permissions", permissionSet(user.Role))
	audience := notificationAudienceFromPath(c.Request.URL.Path)
	c.Set("notification_audience", audience)
	count, err := s.unreadNotificationCount(user.ID, audience)
	if err == nil {
		c.Set("unread_notifications", count)
	}
}

func (s *Server) authenticateRequest(c *gin.Context) (User, bool) {
	tokenValue := bearerToken(c.GetHeader("Authorization"))
	usesBearerToken := tokenValue != ""
	if tokenValue == "" {
		if cookie, err := c.Cookie("auth_token"); err == nil {
			tokenValue = cookie
		}
	}
	if tokenValue == "" {
		s.respondUnauthorized(c, usesBearerToken, "Authentication token is required", false)
		return User{}, false
	}
	tokenUser, err := ParseToken(s.cfg.JWTSecret, tokenValue)
	if err != nil {
		s.respondUnauthorized(c, usesBearerToken, "Invalid authentication token", true)
		return User{}, false
	}
	user, err := s.validateSessionUser(tokenUser)
	if err != nil {
		s.respondUnauthorized(c, usesBearerToken, "Invalid authentication token", true)
		return User{}, false
	}
	if err := s.validateMemberSession(tokenUser, user); err != nil {
		s.respondUnauthorized(c, usesBearerToken, "Invalid authentication token", true)
		return User{}, false
	}
	return user, true
}
