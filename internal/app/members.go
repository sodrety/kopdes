package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Member struct {
	ID              string `json:"id"`
	MemberNo        string `json:"member_no"`
	FullName        string `json:"full_name"`
	Phone           string `json:"phone"`
	Address         string `json:"address"`
	JoinDate        string `json:"join_date"`
	Status          string `json:"status"`
	MemberType      string `json:"member_type"`
	MemberTypeLabel string `json:"member_type_label"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type memberRequest struct {
	MemberNo   string `json:"member_no" form:"member_no"`
	FullName   string `json:"full_name" form:"full_name"`
	Phone      string `json:"phone" form:"phone"`
	Address    string `json:"address" form:"address"`
	JoinDate   string `json:"join_date" form:"join_date"`
	Status     string `json:"status" form:"status"`
	MemberType string `json:"member_type" form:"member_type"`
	Email      string `json:"email" form:"email"`
	Password   string `json:"password" form:"password"`
}

func (s *Server) createMember(c *gin.Context) {
	var req memberRequest
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid member request")
		return
	}
	if memberType := strings.TrimSpace(req.MemberType); memberType != "" && !validMemberType(memberType) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_member_type"))
		return
	}

	member, err := s.insertMember(req)
	if errors.Is(err, errInvalidMember) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Member number, full name, join date, and valid status are required")
		return
	}
	if isUniqueViolation(err) {
		respondError(c, http.StatusConflict, "DUPLICATE_DATA", "Member number already exists")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	loginCreated := false
	email := strings.TrimSpace(req.Email)
	password := strings.TrimSpace(req.Password)
	if email != "" || password != "" {
		if email == "" || password == "" {
			respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Email and password must both be provided to create a member login")
			return
		}
		if _, err := CreateMemberUser(s.db, email, password, member.ID); isUniqueViolation(err) {
			respondError(c, http.StatusConflict, "DUPLICATE_DATA", "Email already exists")
			return
		} else if err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
			return
		}
		loginCreated = true
	}

	respondCreatedOrHXRedirect(c, "/admin/members/"+member.ID, gin.H{
		"id":                member.ID,
		"member_no":         member.MemberNo,
		"full_name":         member.FullName,
		"phone":             member.Phone,
		"address":           member.Address,
		"join_date":         member.JoinDate,
		"status":            member.Status,
		"member_type":       member.MemberType,
		"member_type_label": member.MemberTypeLabel,
		"login_created":     loginCreated,
	})
}

func (s *Server) listMembers(c *gin.Context) {
	members, err := s.allMembers()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"members": members})
}

func (s *Server) getMember(c *gin.Context) {
	member, err := s.memberByID(c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Member not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, member)
}

func (s *Server) updateMemberType(c *gin.Context) {
	var req struct {
		MemberType string `json:"member_type" form:"member_type"`
	}
	if err := c.ShouldBind(&req); err != nil || !validMemberType(strings.TrimSpace(req.MemberType)) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_member_type"))
		return
	}

	member, err := s.setMemberType(c.Param("id"), strings.TrimSpace(req.MemberType))
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_member_not_found"))
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	if isHTMXRequest(c) {
		respondHXRedirect(c, "/admin/members/"+member.ID)
		return
	}
	c.JSON(http.StatusOK, member)
}

func (s *Server) createMemberUser(c *gin.Context) {
	member, err := s.memberByID(c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Member not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	var req struct {
		Email    string `json:"email" form:"email"`
		Password string `json:"password" form:"password"`
	}
	if err := c.ShouldBind(&req); err != nil || strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Password) == "" {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Email and password are required")
		return
	}

	user, err := CreateMemberUser(s.db, strings.TrimSpace(req.Email), req.Password, member.ID)
	if isUniqueViolation(err) {
		respondError(c, http.StatusConflict, "DUPLICATE_DATA", "Email already exists")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":        user.ID,
		"email":     user.Email,
		"role":      user.Role,
		"member_id": user.MemberID.String,
	})
}

func (s *Server) memberProfile(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, member)
}

var errInvalidMember = errors.New("invalid member")

const (
	memberTypeDailyWorker  = "daily_worker"
	memberTypeEmployee     = "employee"
	memberTypeSelfEmployed = "self_employed"
)

var memberTypeLabels = map[string]string{
	memberTypeDailyWorker:  "PHL",
	memberTypeEmployee:     "Karyawan",
	memberTypeSelfEmployed: "Mandiri",
}

func validMemberType(memberType string) bool {
	_, ok := memberTypeLabels[memberType]
	return ok
}

func memberTypeLabel(memberType string) string {
	return memberTypeLabels[memberType]
}

func (s *Server) profileMember(c *gin.Context) (Member, bool) {
	user, ok := currentUser(c)
	if !ok || !user.MemberID.Valid {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "Member identity is required")
		return Member{}, false
	}

	member, err := s.memberByID(user.MemberID.String)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Member not found")
		return Member{}, false
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return Member{}, false
	}
	return member, true
}

func (s *Server) insertMember(req memberRequest) (Member, error) {
	status := req.Status
	if status == "" {
		status = "active"
	}
	member := Member{
		ID:         newID(),
		MemberNo:   strings.TrimSpace(req.MemberNo),
		FullName:   strings.TrimSpace(req.FullName),
		Phone:      strings.TrimSpace(req.Phone),
		Address:    strings.TrimSpace(req.Address),
		JoinDate:   strings.TrimSpace(req.JoinDate),
		Status:     strings.TrimSpace(status),
		MemberType: strings.TrimSpace(req.MemberType),
	}
	if member.MemberType == "" {
		member.MemberType = memberTypeEmployee
	}
	if member.MemberNo == "" || member.FullName == "" || member.JoinDate == "" || !validMemberStatus(member.Status) || !validMemberType(member.MemberType) {
		return Member{}, errInvalidMember
	}
	member.MemberTypeLabel = memberTypeLabel(member.MemberType)

	_, err := s.db.Exec(
		`INSERT INTO members (id, member_no, full_name, phone, address, join_date, status, member_type) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		member.ID,
		member.MemberNo,
		member.FullName,
		member.Phone,
		member.Address,
		member.JoinDate,
		member.Status,
		member.MemberType,
	)
	if err != nil {
		return Member{}, err
	}
	return member, nil
}

func (s *Server) allMembers() ([]Member, error) {
	rows, err := s.db.Query(`SELECT id, member_no, full_name, phone, address, join_date, status, member_type FROM members ORDER BY member_no`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var member Member
		if err := rows.Scan(&member.ID, &member.MemberNo, &member.FullName, &member.Phone, &member.Address, &member.JoinDate, &member.Status, &member.MemberType); err != nil {
			return nil, err
		}
		member.MemberTypeLabel = memberTypeLabel(member.MemberType)
		members = append(members, member)
	}
	return members, rows.Err()
}

func (s *Server) memberByID(id string) (Member, error) {
	var member Member
	err := s.db.QueryRow(
		`SELECT id, member_no, full_name, phone, address, join_date, status, member_type FROM members WHERE id = $1`,
		id,
	).Scan(&member.ID, &member.MemberNo, &member.FullName, &member.Phone, &member.Address, &member.JoinDate, &member.Status, &member.MemberType)
	member.MemberTypeLabel = memberTypeLabel(member.MemberType)
	return member, err
}

func (s *Server) setMemberType(id, memberType string) (Member, error) {
	result, err := s.db.Exec(`UPDATE members SET member_type=$1,updated_at=CURRENT_TIMESTAMP WHERE id=$2`, memberType, id)
	if err != nil {
		return Member{}, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return Member{}, err
	} else if affected == 0 {
		return Member{}, sql.ErrNoRows
	}
	return s.memberByID(id)
}

func validMemberStatus(status string) bool {
	return status == "active" || status == "inactive" || status == "suspended"
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "duplicate")
}
