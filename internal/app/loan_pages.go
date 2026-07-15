package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type LoanStartDateAudit struct {
	OldStartDate string
	NewStartDate string
	ChangedBy    string
	CreatedAt    string
}

func (s *Server) loanInstallments(loanID string) ([]LoanInstallment, error) {
	rows, err := s.db.Query(`SELECT id, loan_id, installment_no, due_date, scheduled_amount, paid_amount FROM loan_installments WHERE loan_id = $1 ORDER BY installment_no`, loanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []LoanInstallment{}
	for rows.Next() {
		var item LoanInstallment
		if err := rows.Scan(&item.ID, &item.LoanID, &item.InstallmentNo, &item.DueDate, &item.ScheduledAmount, &item.PaidAmount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loanStartDateAudits(loanID string) ([]LoanStartDateAudit, error) {
	rows, err := s.db.Query(`SELECT a.old_start_date, a.new_start_date, COALESCE(u.email, a.changed_by), a.created_at FROM loan_start_date_audits a LEFT JOIN users u ON u.id = a.changed_by WHERE a.loan_id = $1 ORDER BY a.created_at DESC`, loanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []LoanStartDateAudit{}
	for rows.Next() {
		var item LoanStartDateAudit
		if err := rows.Scan(&item.OldStartDate, &item.NewStartDate, &item.ChangedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) adminLoanDetailPage(c *gin.Context) {
	loan, err := s.loanByID(c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Loan not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	installments, err := s.loanInstallments(loan.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	audits, err := s.loanStartDateAudits(loan.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	var repaymentCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM loan_repayments WHERE loan_id = $1`, loan.ID).Scan(&repaymentCount); err != nil {
		respondError(c, 500, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	lang := languageFromRequest(c)
	eligibleStatus := loan.Status == "active" || loan.Status == "adjustment_due"
	renderPage(c, "admin-loan-detail", pageData(c, translate(lang, "loan_detail")+" - KKSUK PD Dharma Jaya", "loans", "loan_detail", loan.ID, gin.H{"Loan": loan, "Installments": installments, "Audits": audits, "CanCorrectStartDate": repaymentCount == 0 && eligibleStatus}))
}

func (s *Server) exportLoanPDF(c *gin.Context) {
	loan, err := s.loanByID(c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, 404, "NOT_FOUND", "Loan not found")
		return
	}
	if err != nil {
		respondError(c, 500, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	installments, err := s.loanInstallments(loan.ID)
	if err != nil {
		respondError(c, 500, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	lang := languageFromRequest(c)
	lines := []string{
		fmt.Sprintf("%s: %s", translate(lang, "loan_id"), loan.ID),
		fmt.Sprintf("%s: Rp %d", translate(lang, "approved_principal"), loan.ApprovedAmount),
		fmt.Sprintf("%s: Rp %d", translate(lang, "total_admin_fee"), loan.TotalAdminFee),
		fmt.Sprintf("%s: Rp %d", translate(lang, "total_obligation"), loan.TotalObligation),
		fmt.Sprintf("%s: Rp %d", translate(lang, "remaining_balance"), loan.RemainingBalance),
		fmt.Sprintf("%s: %s", translate(lang, "start_date"), loan.StartDate),
		fmt.Sprintf("%s: %s", translate(lang, "next_due_date"), loan.NextDueDate),
		fmt.Sprintf("%s: %s", translate(lang, "final_due_date"), loan.FinalDueDate),
		fmt.Sprintf("%s: %s", translate(lang, "status"), translate(lang, "status_"+loan.Status)),
	}
	if loan.MonthlyAdminFee != nil {
		lines = append(lines, fmt.Sprintf("%s: Rp %d", translate(lang, "monthly_admin_fee"), *loan.MonthlyAdminFee))
	}
	for _, item := range installments {
		lines = append(lines, fmt.Sprintf("#%d | %s | Rp %d | %s Rp %d", item.InstallmentNo, item.DueDate, item.ScheduledAmount, translate(lang, "paid_amount"), item.PaidAmount))
	}
	writeReportPDF(c, "loan-"+loan.ID+".pdf", "KKSUK PD Dharma Jaya - "+translate(lang, "loan_detail"), lines)
}
