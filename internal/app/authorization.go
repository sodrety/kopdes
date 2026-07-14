package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Permission string

const (
	PermissionDashboardView     Permission = "dashboard.view"
	PermissionReportsView       Permission = "reports.view"
	PermissionMembersView       Permission = "members.view"
	PermissionMembersManage     Permission = "members.manage"
	PermissionSavingsView       Permission = "savings.view"
	PermissionSavingsRecord     Permission = "savings.record"
	PermissionRequestsView      Permission = "requests.view"
	PermissionRequestsDecide    Permission = "requests.decide"
	PermissionLoansView         Permission = "loans.view"
	PermissionRepaymentsView    Permission = "repayments.view"
	PermissionRepaymentsRecord  Permission = "repayments.record"
	PermissionOfficersManage    Permission = "officers.manage"
	PermissionNotificationsView Permission = "notifications.view"
)

var officerPermissions = map[string]map[Permission]bool{
	"manager": {
		PermissionDashboardView: true, PermissionReportsView: true,
		PermissionMembersView: true, PermissionMembersManage: true,
		PermissionSavingsView: true, PermissionSavingsRecord: true,
		PermissionRequestsView: true, PermissionRequestsDecide: true,
		PermissionLoansView: true, PermissionRepaymentsView: true,
		PermissionRepaymentsRecord: true, PermissionNotificationsView: true,
	},
	"ketua_i":  officerOversightPermissions(),
	"ketua_ii": officerOversightPermissions(),
	"ketua_utama": func() map[Permission]bool {
		permissions := officerOversightPermissions()
		permissions[PermissionOfficersManage] = true
		return permissions
	}(),
}

func officerOversightPermissions() map[Permission]bool {
	return map[Permission]bool{
		PermissionDashboardView: true, PermissionReportsView: true,
		PermissionMembersView: true, PermissionSavingsView: true,
		PermissionRequestsView: true, PermissionRequestsDecide: true,
		PermissionLoansView: true, PermissionRepaymentsView: true,
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
	return officerPermissions[role][permission]
}

func permissionSet(role string) map[string]bool {
	result := map[string]bool{}
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
	count, err := s.unreadNotificationCount(user.ID)
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
	if user.Role == "member" {
		if err := s.validateMemberSession(tokenUser, user); err != nil {
			s.respondUnauthorized(c, usesBearerToken, "Invalid authentication token", true)
			return User{}, false
		}
	}
	return user, true
}
