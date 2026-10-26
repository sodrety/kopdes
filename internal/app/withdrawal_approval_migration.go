package app

import (
	"database/sql"
	"fmt"
)

type legacyWithdrawalAwaitingKetuaUtama struct {
	ID               string
	MemberID         string
	KetuaIApproverID string
}

// finalizeLegacyWithdrawalsAwaitingKetuaUtama preserves the old approval
// history while treating an existing Ketua I approval as the final required
// approval under the new withdrawal workflow.
func finalizeLegacyWithdrawalsAwaitingKetuaUtama(tx *sql.Tx, isSQLite bool) error {
	query := `SELECT wr.id,wr.member_id,ketua_i.officer_id
		FROM withdrawal_requests wr
		JOIN members m ON m.id=wr.member_id AND m.status='active'
		JOIN withdrawal_reservations r ON r.request_id=wr.id AND r.member_id=wr.member_id AND r.amount=wr.amount AND r.status='active'
		JOIN withdrawal_request_approvals manager ON manager.request_id=wr.id AND manager.stage='manager' AND manager.decision='approved'
		JOIN withdrawal_request_approvals ketua_ii ON ketua_ii.request_id=wr.id AND ketua_ii.stage='ketua_ii' AND ketua_ii.decision='approved'
		JOIN withdrawal_request_approvals ketua_i ON ketua_i.request_id=wr.id AND ketua_i.stage='ketua_i' AND ketua_i.decision='approved'
		WHERE wr.status='pending' AND wr.current_approval_stage='ketua_utama'
			AND wr.disbursement_date='' AND wr.disbursed_by IS NULL AND wr.disbursed_at IS NULL AND wr.saving_record_id IS NULL
			AND NOT EXISTS (SELECT 1 FROM withdrawal_request_approvals a WHERE a.request_id=wr.id AND a.stage='ketua_utama')
		ORDER BY wr.created_at,wr.id`
	if !isSQLite {
		query += ` FOR UPDATE OF wr`
	}
	rows, err := tx.Query(query)
	if err != nil {
		return fmt.Errorf("find legacy withdrawals awaiting Ketua Utama: %w", err)
	}
	var requests []legacyWithdrawalAwaitingKetuaUtama
	for rows.Next() {
		var request legacyWithdrawalAwaitingKetuaUtama
		if err := rows.Scan(&request.ID, &request.MemberID, &request.KetuaIApproverID); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read legacy withdrawal awaiting Ketua Utama: %w", err)
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate legacy withdrawals awaiting Ketua Utama: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close legacy withdrawal query: %w", err)
	}

	for _, request := range requests {
		result, err := tx.Exec(`UPDATE withdrawal_requests
			SET status='approved',current_approval_stage=NULL,reviewed_by=$1,
				reviewed_at=(SELECT a.created_at FROM withdrawal_request_approvals a WHERE a.request_id=withdrawal_requests.id AND a.stage='ketua_i' AND a.decision='approved' AND a.officer_id=$1),
				updated_at=CURRENT_TIMESTAMP
			WHERE id=$2 AND status='pending' AND current_approval_stage='ketua_utama'
				AND disbursement_date='' AND disbursed_by IS NULL AND disbursed_at IS NULL AND saving_record_id IS NULL
				AND EXISTS (SELECT 1 FROM members m WHERE m.id=withdrawal_requests.member_id AND m.status='active')
				AND EXISTS (SELECT 1 FROM withdrawal_reservations r WHERE r.request_id=withdrawal_requests.id AND r.member_id=withdrawal_requests.member_id AND r.amount=withdrawal_requests.amount AND r.status='active')
				AND EXISTS (SELECT 1 FROM withdrawal_request_approvals a WHERE a.request_id=withdrawal_requests.id AND a.stage='manager' AND a.decision='approved')
				AND EXISTS (SELECT 1 FROM withdrawal_request_approvals a WHERE a.request_id=withdrawal_requests.id AND a.stage='ketua_ii' AND a.decision='approved')
				AND EXISTS (SELECT 1 FROM withdrawal_request_approvals a WHERE a.request_id=withdrawal_requests.id AND a.stage='ketua_i' AND a.decision='approved' AND a.officer_id=$1)
				AND NOT EXISTS (SELECT 1 FROM withdrawal_request_approvals a WHERE a.request_id=withdrawal_requests.id AND a.stage='ketua_utama')`, request.KetuaIApproverID, request.ID)
		if err != nil {
			return fmt.Errorf("approve legacy withdrawal %s after Ketua I: %w", request.ID, err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			continue
		}
		if err := resolveRequestNotifications(tx, "withdrawal", request.ID); err != nil {
			return fmt.Errorf("resolve legacy withdrawal notifications %s: %w", request.ID, err)
		}
		if err := createMemberOutcomeNotification(tx, "withdrawal", request.ID, request.MemberID, "approved", "/member/withdrawal-requests"); err != nil {
			return fmt.Errorf("create legacy withdrawal outcome notification %s: %w", request.ID, err)
		}
		if err := createWithdrawalDisbursementReadyNotification(tx, request.ID); err != nil {
			return fmt.Errorf("create legacy withdrawal disbursement notification %s: %w", request.ID, err)
		}
	}
	return nil
}
