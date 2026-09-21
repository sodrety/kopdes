package app

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

// auditPrivilegedMutations records every unsafe request made by the platform
// identity. The event is intentionally append-only and is independent from
// the Officer approval tables, so an override can never look like an Officer
// decision.
func (s *Server) auditPrivilegedMutations() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if !isUnsafeMethod(c.Request.Method) {
			return
		}
		user, ok := currentUser(c)
		if !ok || user.Role != "super_admin" {
			return
		}
		if _, err := s.db.Exec(`INSERT INTO admin_audit_events (id,actor_id,method,path,status,request_id) VALUES ($1,$2,$3,$4,$5,$6)`, newID(), user.ID, c.Request.Method, c.Request.URL.Path, c.Writer.Status(), requestIDFromContext(c)); err != nil {
			slog.Error("record super admin audit event", "request_id", requestIDFromContext(c), "actor_id", user.ID, "error", err)
		}
	}
}
