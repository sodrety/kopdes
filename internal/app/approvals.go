package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	approvalStageManager    = "manager"
	approvalStageKetuaI     = "ketua_i"
	approvalStageKetuaII    = "ketua_ii"
	approvalStageKetuaUtama = "ketua_utama"
)

type ApprovalDecision struct {
	ID          string `json:"id"`
	Stage       string `json:"stage"`
	Decision    string `json:"decision"`
	OfficerID   string `json:"officer_id,omitempty"`
	OfficerName string `json:"officer_name,omitempty"`
	OfficerRole string `json:"officer_role"`
	Note        string `json:"note,omitempty"`
	Reason      string `json:"reason,omitempty"`
	CreatedAt   string `json:"created_at"`
}

var errWrongApprovalStage = errors.New("wrong approval stage")

func nextApprovalStage(stage string) string {
	switch stage {
	case approvalStageManager:
		return approvalStageKetuaI
	case approvalStageKetuaI:
		return approvalStageKetuaII
	case approvalStageKetuaII:
		return approvalStageKetuaUtama
	default:
		return ""
	}
}

func insertApprovalDecision(tx *sql.Tx, table, requestID string, officer User, decision, note, reason string) error {
	if table != "loan_request_approvals" && table != "withdrawal_request_approvals" {
		return fmt.Errorf("unsupported approval table %q", table)
	}
	name := strings.TrimSpace(officer.FullName)
	if name == "" {
		name = officer.Email
	}
	query := `INSERT INTO ` + table + ` (id,request_id,stage,decision,officer_id,officer_name,officer_role,note,reason) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	_, err := tx.Exec(query, newID(), requestID, officer.Role, decision, officer.ID, name, officer.Role, strings.TrimSpace(note), strings.TrimSpace(reason))
	return err
}

func approvalHistory(db *sql.DB, table, requestID string, includeOfficer bool) ([]ApprovalDecision, error) {
	if table != "loan_request_approvals" && table != "withdrawal_request_approvals" {
		return nil, fmt.Errorf("unsupported approval table %q", table)
	}
	rows, err := db.Query(`SELECT id,stage,decision,officer_id,officer_name,officer_role,note,reason,created_at FROM `+table+` WHERE request_id=$1 ORDER BY created_at,id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var history []ApprovalDecision
	for rows.Next() {
		var decision ApprovalDecision
		if err := rows.Scan(&decision.ID, &decision.Stage, &decision.Decision, &decision.OfficerID, &decision.OfficerName, &decision.OfficerRole, &decision.Note, &decision.Reason, &decision.CreatedAt); err != nil {
			return nil, err
		}
		if !includeOfficer {
			decision.OfficerID = ""
			decision.OfficerName = ""
		}
		history = append(history, decision)
	}
	return history, rows.Err()
}

func latestApprovalDecision(db *sql.DB, table, requestID string) (*ApprovalDecision, error) {
	history, err := approvalHistory(db, table, requestID, false)
	if err != nil || len(history) == 0 {
		return nil, err
	}
	return &history[len(history)-1], nil
}

func createStageNotification(tx *sql.Tx, requestType, requestID, stage, link string) error {
	payload, _ := json.Marshal(map[string]string{"stage": stage})
	eventID := newID()
	if _, err := tx.Exec(`INSERT INTO notification_events (id,event_type,request_type,request_id,payload) VALUES ($1,'approval_stage_ready',$2,$3,$4)`, eventID, requestType, requestID, string(payload)); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT id FROM users WHERE role=$1 AND active=TRUE`, stage)
	if err != nil {
		return err
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, userID := range userIDs {
		if _, err := tx.Exec(`INSERT INTO notifications (id,event_id,user_id,title_key,body_key,link) VALUES ($1,$2,$3,'notification_approval_title','notification_approval_body',$4)`, newID(), eventID, userID, link); err != nil {
			return err
		}
	}
	return nil
}

func resolveRequestNotifications(tx *sql.Tx, requestType, requestID string) error {
	_, err := tx.Exec(`UPDATE notifications SET resolved_at=CURRENT_TIMESTAMP WHERE resolved_at IS NULL AND event_id IN (SELECT id FROM notification_events WHERE request_type=$1 AND request_id=$2)`, requestType, requestID)
	return err
}

func createMemberOutcomeNotification(tx *sql.Tx, requestType, requestID, memberID, outcome, link string) error {
	payload, _ := json.Marshal(map[string]string{"outcome": outcome})
	eventID := newID()
	if _, err := tx.Exec(`INSERT INTO notification_events (id,event_type,request_type,request_id,payload) VALUES ($1,$2,$3,$4,$5)`, eventID, "request_"+outcome, requestType, requestID, string(payload)); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT id FROM users WHERE member_id=$1 AND role='member' AND active=TRUE`, memberID)
	if err != nil {
		return err
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, userID := range userIDs {
		if _, err := tx.Exec(`INSERT INTO notifications (id,event_id,user_id,title_key,body_key,link) VALUES ($1,$2,$3,$4,$5,$6)`, newID(), eventID, userID, "notification_"+outcome+"_title", "notification_"+outcome+"_body", link); err != nil {
			return err
		}
	}
	return nil
}
