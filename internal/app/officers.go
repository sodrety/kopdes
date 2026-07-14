package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type Officer struct {
	ID                 string `json:"id"`
	FullName           string `json:"full_name"`
	Email              string `json:"email"`
	Role               string `json:"role"`
	Active             bool   `json:"active"`
	MustChangePassword bool   `json:"must_change_password"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

type OfficerAuditEvent struct {
	ID         string `json:"id"`
	ActorName  string `json:"actor_name"`
	TargetName string `json:"target_name"`
	Action     string `json:"action"`
	OldRole    string `json:"old_role"`
	NewRole    string `json:"new_role"`
	OldActive  any    `json:"old_active,omitempty"`
	NewActive  any    `json:"new_active,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type createOfficerInput struct {
	FullName string `json:"full_name" form:"full_name"`
	Email    string `json:"email" form:"email"`
	Role     string `json:"role" form:"role"`
	Password string `json:"password" form:"password"`
}

type updateOfficerInput struct {
	FullName string `json:"full_name" form:"full_name"`
	Role     string `json:"role" form:"role"`
	Active   *bool  `json:"active" form:"active"`
}

type resetOfficerPasswordInput struct {
	Password string `json:"password" form:"password"`
}

var (
	errInvalidOfficer           = errors.New("invalid officer")
	errOfficerNotFound          = errors.New("officer not found")
	errLastActiveKetuaUtama     = errors.New("last active ketua utama")
	errInvalidTemporaryPassword = errors.New("invalid temporary password")
)

func (s *Server) listOfficers(c *gin.Context) {
	officers, err := s.officers()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"officers": officers})
}

func (s *Server) adminOfficersPage(c *gin.Context) {
	officers, err := s.officers()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	audits, err := s.officerAuditEvents()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-officers", pageData(c, translate(languageFromRequest(c), "officers_page_title"), "officers", "officers", "officers_description", gin.H{"Officers": officers, "Audits": audits}))
}

func (s *Server) passwordChangePage(c *gin.Context) {
	renderPage(c, "password-change", pageData(c, translate(languageFromRequest(c), "password_change_page_title"), "", "change_password", "change_password_description", nil))
}

func (s *Server) createOfficer(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}
	var req createOfficerInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid Officer details")
		return
	}
	officer, err := s.insertOfficer(actor, req)
	if errors.Is(err, errInvalidOfficer) || errors.Is(err, errInvalidTemporaryPassword) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid Officer details")
		return
	}
	if isUniqueViolation(err) {
		respondError(c, http.StatusConflict, "DUPLICATE_DATA", "Email already exists")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	respondCreatedOrHXRedirect(c, "/admin/officers", officer)
}

func (s *Server) updateOfficer(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}
	var req updateOfficerInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid Officer details")
		return
	}
	officer, err := s.updateOfficerByID(actor, c.Param("id"), req)
	if errors.Is(err, errInvalidOfficer) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid Officer details")
		return
	}
	if errors.Is(err, errLastActiveKetuaUtama) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "At least one active Ketua Utama is required")
		return
	}
	if errors.Is(err, errOfficerNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Officer not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	respondOKOrHXRedirect(c, "/admin/officers", officer)
}

func (s *Server) resetOfficerPassword(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}
	var req resetOfficerPasswordInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Temporary password must contain at least 8 characters")
		return
	}
	if err := s.resetOfficerPasswordByID(actor, c.Param("id"), req.Password); errors.Is(err, errInvalidTemporaryPassword) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Temporary password must contain at least 8 characters")
		return
	} else if errors.Is(err, errOfficerNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Officer not found")
		return
	} else if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	respondOKOrHXRedirect(c, "/admin/officers", gin.H{"status": "ok"})
}

func (s *Server) changePassword(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}
	var req struct {
		Password string `json:"password" form:"password"`
	}
	if err := c.ShouldBind(&req); err != nil || len(req.Password) < 8 {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Password must contain at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	if _, err := s.db.Exec(`UPDATE users SET password_hash=$1, must_change_password=FALSE, updated_at=CURRENT_TIMESTAMP WHERE id=$2 AND active=TRUE`, string(hash), user.ID); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	redirect := "/admin/dashboard"
	if user.Role == "member" {
		redirect = "/member/dashboard"
	}
	respondOKOrHXRedirect(c, redirect, gin.H{"status": "ok"})
}

func (s *Server) officers() ([]Officer, error) {
	rows, err := s.db.Query(`SELECT id, full_name, email, role, active, must_change_password, created_at, updated_at FROM users WHERE role <> 'member' ORDER BY role, full_name, email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var officers []Officer
	for rows.Next() {
		var officer Officer
		if err := rows.Scan(&officer.ID, &officer.FullName, &officer.Email, &officer.Role, &officer.Active, &officer.MustChangePassword, &officer.CreatedAt, &officer.UpdatedAt); err != nil {
			return nil, err
		}
		officers = append(officers, officer)
	}
	return officers, rows.Err()
}

func (s *Server) officerAuditEvents() ([]OfficerAuditEvent, error) {
	rows, err := s.db.Query(`SELECT id,actor_name,target_name,action,old_role,new_role,old_active,new_active,created_at FROM officer_audit_events ORDER BY created_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []OfficerAuditEvent
	for rows.Next() {
		var event OfficerAuditEvent
		var oldActive, newActive sql.NullBool
		if err := rows.Scan(&event.ID, &event.ActorName, &event.TargetName, &event.Action, &event.OldRole, &event.NewRole, &oldActive, &newActive, &event.CreatedAt); err != nil {
			return nil, err
		}
		if oldActive.Valid {
			event.OldActive = oldActive.Bool
		}
		if newActive.Valid {
			event.NewActive = newActive.Bool
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Server) insertOfficer(actor User, req createOfficerInput) (Officer, error) {
	name := strings.TrimSpace(req.FullName)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	role := strings.TrimSpace(req.Role)
	if name == "" || email == "" || !validOfficerRole(role) {
		return Officer{}, errInvalidOfficer
	}
	if len(req.Password) < 8 {
		return Officer{}, errInvalidTemporaryPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return Officer{}, err
	}
	officer := Officer{ID: newID(), FullName: name, Email: email, Role: role, Active: true, MustChangePassword: true}
	tx, err := s.db.Begin()
	if err != nil {
		return Officer{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO users (id,email,password_hash,role,full_name,active,must_change_password) VALUES ($1,$2,$3,$4,$5,TRUE,TRUE)`, officer.ID, officer.Email, string(hash), officer.Role, officer.FullName); err != nil {
		return Officer{}, err
	}
	if err := insertOfficerAudit(tx, actor, officer, "created", "", officer.Role, nil, boolPointer(true)); err != nil {
		return Officer{}, err
	}
	if err := tx.Commit(); err != nil {
		return Officer{}, err
	}
	return officer, nil
}

func (s *Server) updateOfficerByID(actor User, id string, req updateOfficerInput) (Officer, error) {
	name := strings.TrimSpace(req.FullName)
	role := strings.TrimSpace(req.Role)
	if name == "" || !validOfficerRole(role) || req.Active == nil {
		return Officer{}, errInvalidOfficer
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Officer{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var current Officer
	err = tx.QueryRow(`SELECT id,full_name,email,role,active,must_change_password,created_at,updated_at FROM users WHERE id=$1 AND role <> 'member'`+rowLockClause(s.db), id).Scan(&current.ID, &current.FullName, &current.Email, &current.Role, &current.Active, &current.MustChangePassword, &current.CreatedAt, &current.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Officer{}, errOfficerNotFound
	}
	if err != nil {
		return Officer{}, err
	}
	if current.Role == "ketua_utama" && current.Active && (role != "ketua_utama" || !*req.Active) {
		var activeCount int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE role='ketua_utama' AND active=TRUE`).Scan(&activeCount); err != nil {
			return Officer{}, err
		}
		if activeCount <= 1 {
			return Officer{}, errLastActiveKetuaUtama
		}
	}
	if _, err := tx.Exec(`UPDATE users SET full_name=$1,role=$2,active=$3,updated_at=CURRENT_TIMESTAMP WHERE id=$4`, name, role, *req.Active, id); err != nil {
		return Officer{}, err
	}
	updated := current
	updated.FullName, updated.Role, updated.Active = name, role, *req.Active
	if current.Role != role {
		if err := insertOfficerAudit(tx, actor, updated, "role_changed", current.Role, role, nil, nil); err != nil {
			return Officer{}, err
		}
	}
	if current.Active != *req.Active {
		action := "deactivated"
		if *req.Active {
			action = "activated"
		}
		if err := insertOfficerAudit(tx, actor, updated, action, role, role, boolPointer(current.Active), req.Active); err != nil {
			return Officer{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Officer{}, err
	}
	return updated, nil
}

func (s *Server) resetOfficerPasswordByID(actor User, id, password string) error {
	if len(password) < 8 {
		return errInvalidTemporaryPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var target Officer
	err = tx.QueryRow(`SELECT id,full_name,email,role,active,must_change_password,created_at,updated_at FROM users WHERE id=$1 AND role <> 'member'`+rowLockClause(s.db), id).Scan(&target.ID, &target.FullName, &target.Email, &target.Role, &target.Active, &target.MustChangePassword, &target.CreatedAt, &target.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return errOfficerNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET password_hash=$1,must_change_password=TRUE,updated_at=CURRENT_TIMESTAMP WHERE id=$2`, string(hash), id); err != nil {
		return err
	}
	if err := insertOfficerAudit(tx, actor, target, "password_reset", target.Role, target.Role, nil, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func insertOfficerAudit(tx *sql.Tx, actor User, target Officer, action, oldRole, newRole string, oldActive, newActive *bool) error {
	actorName := actor.FullName
	if actorName == "" {
		actorName = actor.Email
	}
	_, err := tx.Exec(`INSERT INTO officer_audit_events (id,actor_id,actor_name,target_id,target_name,action,old_role,new_role,old_active,new_active) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, newID(), actor.ID, actorName, target.ID, target.FullName, action, oldRole, newRole, oldActive, newActive)
	return err
}

func boolPointer(value bool) *bool { return &value }
