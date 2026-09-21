package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type MemberTagihanConfig struct {
	MemberID         string `json:"member_id"`
	SimpananWajib    int64  `json:"simpanan_wajib"`
	SimpananManasuka int64  `json:"simpanan_manasuka"`
	CreatedAt        string `json:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

var errInvalidMemberTagihanConfig = errors.New("invalid member Tagihan configuration")

func (s *Server) memberTagihanConfig(memberID string) (MemberTagihanConfig, error) {
	config := MemberTagihanConfig{MemberID: memberID}
	err := s.db.QueryRow(`
		SELECT member_id,simpanan_wajib,simpanan_manasuka,
		       COALESCE(CAST(created_at AS TEXT),''),COALESCE(CAST(updated_at AS TEXT),'')
		FROM member_tagihan_configs
		WHERE member_id=$1`, memberID).Scan(
		&config.MemberID,
		&config.SimpananWajib,
		&config.SimpananManasuka,
		&config.CreatedAt,
		&config.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return config, nil
	}
	return config, err
}

func (s *Server) updateMemberTagihanConfig(c *gin.Context) {
	memberID := strings.TrimSpace(c.Param("id"))
	if memberID == "" {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_member_not_found"))
		return
	}
	var memberExists bool
	if err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM members WHERE id=$1)`, memberID).Scan(&memberExists); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	if !memberExists {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_member_not_found"))
		return
	}

	wajib, manasuka, err := memberTagihanConfigInput(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_tagihan_config"))
		return
	}
	if _, err := s.db.Exec(`
		INSERT INTO member_tagihan_configs (member_id,simpanan_wajib,simpanan_manasuka)
		VALUES ($1,$2,$3)
		ON CONFLICT (member_id) DO UPDATE SET
			simpanan_wajib=excluded.simpanan_wajib,
			simpanan_manasuka=excluded.simpanan_manasuka,
			updated_at=CURRENT_TIMESTAMP`, memberID, wajib, manasuka); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	config, err := s.memberTagihanConfig(memberID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	if isHTMXRequest(c) {
		respondHXRedirect(c, "/admin/members/"+memberID)
		return
	}
	c.JSON(http.StatusOK, config)
}

func memberTagihanConfigInput(c *gin.Context) (int64, int64, error) {
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		var payload struct {
			SimpananWajib    json.RawMessage `json:"simpanan_wajib"`
			SimpananManasuka json.RawMessage `json:"simpanan_manasuka"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			return 0, 0, errInvalidMemberTagihanConfig
		}
		wajib, err := parseMemberTagihanAmount(string(payload.SimpananWajib))
		if err != nil {
			return 0, 0, err
		}
		manasuka, err := parseMemberTagihanAmount(string(payload.SimpananManasuka))
		if err != nil {
			return 0, 0, err
		}
		return wajib, manasuka, nil
	}
	wajib, err := parseMemberTagihanAmount(c.PostForm("simpanan_wajib"))
	if err != nil {
		return 0, 0, err
	}
	manasuka, err := parseMemberTagihanAmount(c.PostForm("simpanan_manasuka"))
	if err != nil {
		return 0, 0, err
	}
	return wajib, manasuka, nil
}

func parseMemberTagihanAmount(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" || value == `""` {
		return 0, nil
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var decoded string
		if err := json.Unmarshal([]byte(value), &decoded); err != nil {
			return 0, errInvalidMemberTagihanConfig
		}
		value = strings.TrimSpace(decoded)
		if value == "" {
			return 0, nil
		}
	}
	amount, err := strconv.ParseInt(value, 10, 64)
	if err != nil || amount < 0 {
		return 0, errInvalidMemberTagihanConfig
	}
	return amount, nil
}

func ensureMissingMemberTagihanConfigs(db *sql.DB) error {
	isSQLite := strings.Contains(strings.ToLower(fmt.Sprintf("%T", db.Driver())), "sqlite")
	var tableExists bool
	var err error
	if isSQLite {
		err = db.QueryRow(`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type='table' AND name='member_tagihan_configs')`).Scan(&tableExists)
	} else {
		err = db.QueryRow(`SELECT to_regclass('member_tagihan_configs') IS NOT NULL`).Scan(&tableExists)
	}
	if err != nil {
		return err
	}
	if !tableExists {
		return nil
	}
	_, err = db.Exec(`
		INSERT INTO member_tagihan_configs (member_id,simpanan_wajib,simpanan_manasuka)
		SELECT m.id,
			COALESCE((SELECT CASE WHEN sr.type='deposit' THEN sr.amount ELSE 0 END
				FROM saving_records sr
				WHERE sr.member_id=m.id AND sr.category='wajib'
				ORDER BY sr.record_date DESC,sr.created_at DESC,sr.id DESC LIMIT 1),0),
			COALESCE((SELECT CASE WHEN sr.type='deposit' THEN sr.amount ELSE 0 END
				FROM saving_records sr
				WHERE sr.member_id=m.id AND sr.category='sukarela'
				ORDER BY sr.record_date DESC,sr.created_at DESC,sr.id DESC LIMIT 1),0)
		FROM members m
		WHERE m.id IS NOT NULL
		ON CONFLICT (member_id) DO NOTHING`)
	return err
}
