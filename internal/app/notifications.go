package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Notification struct {
	ID         string `json:"id"`
	EventType  string `json:"event_type"`
	TitleKey   string `json:"title_key"`
	BodyKey    string `json:"body_key"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	Link       string `json:"link"`
	IsRead     bool   `json:"is_read"`
	ResolvedAt string `json:"resolved_at,omitempty"`
	CreatedAt  string `json:"created_at"`
}

func (s *Server) listNotifications(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}
	notifications, err := s.notificationsForUser(user.ID, languageFromRequest(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"notifications": notifications})
}

func (s *Server) notificationsPage(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}
	notifications, err := s.notificationsForUser(user.ID, languageFromRequest(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "notifications", pageData(c, translate(languageFromRequest(c), "notifications_page_title"), "notifications", "notifications", "notifications_description", gin.H{"Notifications": notifications}))
}

func (s *Server) markNotificationRead(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}
	result, err := s.db.Exec(`UPDATE notifications SET is_read=TRUE WHERE id=$1 AND user_id=$2`, c.Param("id"), user.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Notification not found")
		return
	}
	respondOKOrHXRedirect(c, "/notifications", gin.H{"status": "ok"})
}

func (s *Server) notificationsForUser(userID, lang string) ([]Notification, error) {
	rows, err := s.db.Query(`SELECT n.id,e.event_type,n.title_key,n.body_key,n.link,n.is_read,COALESCE(CAST(n.resolved_at AS TEXT),''),n.created_at FROM notifications n INNER JOIN notification_events e ON e.id=n.event_id WHERE n.user_id=$1 ORDER BY n.created_at DESC,n.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notifications []Notification
	for rows.Next() {
		var notification Notification
		if err := rows.Scan(&notification.ID, &notification.EventType, &notification.TitleKey, &notification.BodyKey, &notification.Link, &notification.IsRead, &notification.ResolvedAt, &notification.CreatedAt); err != nil {
			return nil, err
		}
		notification.Title = translate(lang, notification.TitleKey)
		notification.Body = translate(lang, notification.BodyKey)
		notifications = append(notifications, notification)
	}
	return notifications, rows.Err()
}

func (s *Server) unreadNotificationCount(userID string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND is_read=FALSE AND resolved_at IS NULL`, userID).Scan(&count)
	return count, err
}
