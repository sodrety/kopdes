package app

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) memberLoanRequestDetailPage(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}

	request, err := s.loanRequestByMemberID(c.Param("id"), member.ID)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_loan_request_not_found"))
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}

	loan, err := s.loanForMemberRequest(request.ID, member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}

	var repayments []LoanRepayment
	if loan != nil {
		repayments, err = s.repaymentsByLoanForMember(loan.ID, member.ID)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
	}

	lang := languageFromRequest(c)
	renderPage(c, "member-loan-request-detail", pageData(c, translate(lang, "loan_detail")+" - KKSUK PD Dharma Jaya", "loan-requests", "loan_detail", request.ID, gin.H{
		"Request":    request,
		"Loan":       loan,
		"Repayments": repayments,
		"ShellClass": "member-loan-request-detail-shell",
	}))
}

func (s *Server) loanRequestByMemberID(requestID, memberID string) (LoanRequest, error) {
	var request LoanRequest
	err := s.db.QueryRow(
		`SELECT id, member_id, requested_amount, duration_months, purpose, status, loan_type, legacy_terms, COALESCE(current_approval_stage,''), COALESCE(proposed_approved_amount,0), COALESCE(proposed_duration_months,0), proposed_start_date, COALESCE(proposed_admin_fee_policy,''), proposed_monthly_admin_fee, COALESCE(proposed_total_admin_fee,0), COALESCE(proposed_total_obligation,0), rejection_reason, created_at, updated_at
		FROM loan_requests
		WHERE id = $1 AND member_id = $2`,
		requestID,
		memberID,
	).Scan(&request.ID, &request.MemberID, &request.RequestedAmount, &request.DurationMonths, &request.Purpose, &request.Status, &request.LoanType, &request.LegacyTerms, &request.CurrentApprovalStage, &request.ProposedApprovedAmount, &request.ProposedDurationMonths, &request.ProposedStartDate, &request.ProposedAdminFeePolicy, &request.ProposedMonthlyAdminFee, &request.ProposedTotalAdminFee, &request.ProposedTotalObligation, &request.RejectionReason, &request.CreatedAt, &request.UpdatedAt)
	if err == nil {
		request.LatestDecision, err = latestApprovalDecision(s.db, "loan_request_approvals", request.ID)
	}
	return request, err
}

func (s *Server) loanForMemberRequest(requestID, memberID string) (*Loan, error) {
	var loanID string
	err := s.db.QueryRow(`SELECT id FROM loans WHERE loan_request_id = $1 AND member_id = $2`, requestID, memberID).Scan(&loanID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	loan, err := s.loanByID(loanID)
	if err != nil {
		return nil, err
	}
	return &loan, nil
}

func (s *Server) repaymentsByLoanForMember(loanID, memberID string) ([]LoanRepayment, error) {
	rows, err := s.db.Query(
		`SELECT id, loan_id, member_id, amount, record_date, reference_no, note, recorded_by, created_at, event_type, historical, included_in_totals
		FROM (`+repaymentHistoryQuery+`) history
		WHERE loan_id = $1 AND member_id = $2
		ORDER BY record_date DESC, created_at DESC, id DESC`,
		loanID,
		memberID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repayments []LoanRepayment
	for rows.Next() {
		var repayment LoanRepayment
		if err := rows.Scan(&repayment.ID, &repayment.LoanID, &repayment.MemberID, &repayment.Amount, &repayment.RecordDate, &repayment.ReferenceNo, &repayment.Note, &repayment.RecordedBy, &repayment.CreatedAt, &repayment.Type, &repayment.Historical, &repayment.IncludedInTotals); err != nil {
			return nil, err
		}
		repayments = append(repayments, repayment)
	}
	return repayments, rows.Err()
}
