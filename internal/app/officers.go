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
	UserID             string `json:"user_id"`
	MemberID           string `json:"member_id"`
	MemberNo           string `json:"member_no"`
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
	MemberID string `json:"member_id" form:"member_id"`
	Email    string `json:"email" form:"email"`
	Role     string `json:"role" form:"role"`
	Password string `json:"password" form:"password"`
}

type updateOfficerInput struct {
	Role   string `json:"role" form:"role"`
	Active *bool  `json:"active" form:"active"`
}

type resetOfficerPasswordInput struct {
	Password string `json:"password" form:"password"`
}

var (
	errInvalidOfficer           = errors.New("invalid officer")
	errOfficerNotFound          = errors.New("officer not found")
	errLastActiveKetuaUtama     = errors.New("last active ketua utama")
	errInvalidTemporaryPassword = errors.New("invalid temporary password")
	errMemberAlreadyOfficer     = errors.New("member already has an Officer Appointment")
	errInactiveOfficerMember    = errors.New("Officer Member must be active")
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
	members, err := s.allMembers()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-officers", pageData(c, translate(languageFromRequest(c), "officers_page_title"), "officers", "officers", "officers_description", gin.H{"Officers": officers, "Audits": audits, "Members": members}))
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
	if errors.Is(err, errInvalidOfficer) || errors.Is(err, errInvalidTemporaryPassword) || errors.Is(err, errInactiveOfficerMember) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid Officer details")
		return
	}
	if errors.Is(err, errMemberAlreadyOfficer) {
		respondError(c, http.StatusConflict, "DUPLICATE_DATA", translate(languageFromRequest(c), "error_member_already_officer"))
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
	if errors.Is(err, errInvalidOfficer) || errors.Is(err, errInactiveOfficerMember) {
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
	respondOKOrHXRedirect(c, "/member/dashboard", gin.H{"status": "ok"})
}

func (s *Server) officers() ([]Officer, error) {
	rows, err := s.db.Query(`SELECT oa.id,u.id,m.id,m.member_no,m.full_name,u.email,oa.role,oa.active,u.must_change_password,oa.created_at,oa.updated_at
		FROM officer_appointments oa JOIN members m ON m.id=oa.member_id JOIN users u ON u.member_id=m.id AND u.historical_identity=FALSE
		ORDER BY oa.role,m.full_name,u.email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var officers []Officer
	for rows.Next() {
		var officer Officer
		if err := rows.Scan(&officer.ID, &officer.UserID, &officer.MemberID, &officer.MemberNo, &officer.FullName, &officer.Email, &officer.Role, &officer.Active, &officer.MustChangePassword, &officer.CreatedAt, &officer.UpdatedAt); err != nil {
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
	memberID := strings.TrimSpace(req.MemberID)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	role := strings.TrimSpace(req.Role)
	if memberID == "" || !validOfficerRole(role) {
		return Officer{}, errInvalidOfficer
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Officer{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var memberNo, name, status string
	if err := tx.QueryRow(`SELECT member_no,full_name,status FROM members WHERE id=$1`+rowLockClause(s.db), memberID).Scan(&memberNo, &name, &status); errors.Is(err, sql.ErrNoRows) {
		return Officer{}, errInvalidOfficer
	} else if err != nil {
		return Officer{}, err
	}
	if status != "active" {
		return Officer{}, errInactiveOfficerMember
	}
	var existingAppointment string
	if err := tx.QueryRow(`SELECT id FROM officer_appointments WHERE member_id=$1`, memberID).Scan(&existingAppointment); err == nil {
		return Officer{}, errMemberAlreadyOfficer
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Officer{}, err
	}
	var userID string
	var userEmail string
	var mustChange bool
	err = tx.QueryRow(`SELECT id,email,must_change_password FROM users WHERE member_id=$1 AND historical_identity=FALSE`, memberID).Scan(&userID, &userEmail, &mustChange)
	if errors.Is(err, sql.ErrNoRows) {
		if email == "" || len(req.Password) < 8 {
			return Officer{}, errInvalidTemporaryPassword
		}
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			return Officer{}, hashErr
		}
		userID, userEmail, mustChange = newID(), email, true
		if _, err := tx.Exec(`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active,must_change_password,historical_identity) VALUES ($1,$2,$3,'member',$4,$5,TRUE,TRUE,FALSE)`, userID, userEmail, string(hash), memberID, name); err != nil {
			return Officer{}, err
		}
	} else if err != nil {
		return Officer{}, err
	}
	officer := Officer{ID: newID(), UserID: userID, MemberID: memberID, MemberNo: memberNo, FullName: name, Email: userEmail, Role: role, Active: true, MustChangePassword: mustChange}
	if _, err := tx.Exec(`INSERT INTO officer_appointments (id,member_id,role,active) VALUES ($1,$2,$3,TRUE)`, officer.ID, memberID, role); err != nil {
		return Officer{}, err
	}
	if err := syncOfficerNotifications(tx, officer.UserID, officer.Role, true); err != nil {
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
	role := strings.TrimSpace(req.Role)
	if !validOfficerRole(role) || req.Active == nil {
		return Officer{}, errInvalidOfficer
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Officer{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var current Officer
	err = scanOfficer(tx.QueryRow(`SELECT oa.id,u.id,m.id,m.member_no,m.full_name,u.email,oa.role,oa.active,u.must_change_password,oa.created_at,oa.updated_at
		FROM officer_appointments oa JOIN members m ON m.id=oa.member_id JOIN users u ON u.member_id=m.id AND u.historical_identity=FALSE
		WHERE oa.id=$1`+rowLockClause(s.db), id), &current)
	if errors.Is(err, sql.ErrNoRows) {
		return Officer{}, errOfficerNotFound
	}
	if err != nil {
		return Officer{}, err
	}
	var memberStatus string
	if err := tx.QueryRow(`SELECT status FROM members WHERE id=$1`, current.MemberID).Scan(&memberStatus); err != nil {
		return Officer{}, err
	}
	if *req.Active && memberStatus != "active" {
		return Officer{}, errInactiveOfficerMember
	}
	if current.Role == "ketua_utama" && current.Active && (role != "ketua_utama" || !*req.Active) {
		var activeCount int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id=oa.member_id WHERE oa.role='ketua_utama' AND oa.active=TRUE AND m.status='active'`).Scan(&activeCount); err != nil {
			return Officer{}, err
		}
		if activeCount <= 1 {
			return Officer{}, errLastActiveKetuaUtama
		}
	}
	if _, err := tx.Exec(`UPDATE officer_appointments SET role=$1,active=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$3`, role, *req.Active, id); err != nil {
		return Officer{}, err
	}
	updated := current
	updated.Role, updated.Active = role, *req.Active
	if current.Role != updated.Role || current.Active != updated.Active {
		if err := syncOfficerNotifications(tx, updated.UserID, updated.Role, updated.Active); err != nil {
			return Officer{}, err
		}
	}
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
	err = scanOfficer(tx.QueryRow(`SELECT oa.id,u.id,m.id,m.member_no,m.full_name,u.email,oa.role,oa.active,u.must_change_password,oa.created_at,oa.updated_at
		FROM officer_appointments oa JOIN members m ON m.id=oa.member_id JOIN users u ON u.member_id=m.id AND u.historical_identity=FALSE
		WHERE oa.id=$1`+rowLockClause(s.db), id), &target)
	if errors.Is(err, sql.ErrNoRows) {
		return errOfficerNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET password_hash=$1,must_change_password=TRUE,updated_at=CURRENT_TIMESTAMP WHERE id=$2`, string(hash), target.UserID); err != nil {
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
	_, err := tx.Exec(`INSERT INTO officer_audit_events (id,actor_id,actor_member_id,actor_member_no,actor_name,target_id,target_member_id,target_member_no,target_name,target_appointment_id,action,old_role,new_role,old_active,new_active) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, newID(), actor.ID, actor.MemberID.String, actor.MemberNo, actorName, target.UserID, target.MemberID, target.MemberNo, target.FullName, target.ID, action, oldRole, newRole, oldActive, newActive)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOfficer(row rowScanner, officer *Officer) error {
	return row.Scan(&officer.ID, &officer.UserID, &officer.MemberID, &officer.MemberNo, &officer.FullName, &officer.Email, &officer.Role, &officer.Active, &officer.MustChangePassword, &officer.CreatedAt, &officer.UpdatedAt)
}

func boolPointer(value bool) *bool { return &value }
