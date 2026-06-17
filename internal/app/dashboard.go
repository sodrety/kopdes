package app

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type AdminDashboardSummary struct {
	TotalMembers         int `json:"total_members"`
	ActiveMembers        int `json:"active_members"`
	TotalSavings         int `json:"total_savings"`
	ActiveLoans          int `json:"active_loans"`
	TotalOutstandingLoan int `json:"total_outstanding_loan"`
	PendingLoanRequests  int `json:"pending_loan_requests"`
}

type MemberDashboardSummary struct {
	SavingBalance        int             `json:"saving_balance"`
	ActiveLoan           *Loan           `json:"active_loan"`
	RemainingLoanBalance int             `json:"remaining_loan_balance"`
	LatestSavings        []SavingRecord  `json:"latest_savings"`
	LatestRepayments     []LoanRepayment `json:"latest_repayments"`
}

func (s *Server) memberDashboard(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}

	dashboard, err := s.memberDashboardSummary(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, dashboard)
}

func (s *Server) adminDashboardSummary() (AdminDashboardSummary, error) {
	var summary AdminDashboardSummary
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM members`).Scan(&summary.TotalMembers); err != nil {
		return AdminDashboardSummary{}, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM members WHERE status = 'active'`).Scan(&summary.ActiveMembers); err != nil {
		return AdminDashboardSummary{}, err
	}
	if err := s.db.QueryRow(
		`SELECT COALESCE(SUM(CASE WHEN type = 'deposit' THEN amount ELSE -amount END), 0) FROM saving_records`,
	).Scan(&summary.TotalSavings); err != nil {
		return AdminDashboardSummary{}, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM loans WHERE status = 'active'`).Scan(&summary.ActiveLoans); err != nil {
		return AdminDashboardSummary{}, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(remaining_balance), 0) FROM loans WHERE status = 'active'`).Scan(&summary.TotalOutstandingLoan); err != nil {
		return AdminDashboardSummary{}, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM loan_requests WHERE status = 'pending'`).Scan(&summary.PendingLoanRequests); err != nil {
		return AdminDashboardSummary{}, err
	}
	return summary, nil
}

func (s *Server) memberDashboardSummary(memberID string) (MemberDashboardSummary, error) {
	savingSummary, err := s.savingSummary(memberID)
	if err != nil {
		return MemberDashboardSummary{}, err
	}
	savings, err := s.latestSavingsByMember(memberID, 5)
	if err != nil {
		return MemberDashboardSummary{}, err
	}
	repayments, err := s.latestRepaymentsByMember(memberID, 5)
	if err != nil {
		return MemberDashboardSummary{}, err
	}

	var activeLoan *Loan
	loan, err := s.activeLoanByMember(memberID)
	if err == nil {
		activeLoan = &loan
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return MemberDashboardSummary{}, err
	}

	remainingBalance := 0
	if activeLoan != nil {
		remainingBalance = activeLoan.RemainingBalance
	}
	return MemberDashboardSummary{
		SavingBalance:        savingSummary.CurrentBalance,
		ActiveLoan:           activeLoan,
		RemainingLoanBalance: remainingBalance,
		LatestSavings:        savings,
		LatestRepayments:     repayments,
	}, nil
}
