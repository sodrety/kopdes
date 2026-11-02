package app

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

type loanFormPrintField struct {
	Left     float64
	Top      float64
	FontSize float64
	Value    string
}

type loanFormPrintMask struct {
	Left   float64
	Top    float64
	Width  float64
	Height float64
}

func (s *Server) adminLoanApplicationFormPrintPage(c *gin.Context) {
	report, ok := s.loanReportData(c)
	if !ok {
		return
	}
	application, err := s.loanApplicationFormPrintDataFromReport(report)
	if err != nil {
		respondLoanFormPrintInternalError(c)
		return
	}
	background, texts, masks, err := loanApplicationFormPrintLayout(application)
	if err != nil {
		respondLoanFormPrintInternalError(c)
		return
	}
	loanID := url.PathEscape(report.Loan.ID)
	renderLoanFormPrintPage(c, translate(report.Language, "loan_application_print"), "/admin/loans/"+loanID, background, texts, masks)
}

func (s *Server) adminLoanAcceptanceFormPrintPage(c *gin.Context) {
	report, ok := s.loanReportData(c)
	if !ok {
		return
	}
	background, texts, masks, err := loanAcceptanceFormPrintLayout(report)
	if err != nil {
		respondLoanFormPrintInternalError(c)
		return
	}
	loanID := url.PathEscape(report.Loan.ID)
	renderLoanFormPrintPage(c, translate(report.Language, "loan_acceptance_print"), "/admin/loans/"+loanID, background, texts, masks)
}

func (s *Server) adminLoanRequestApplicationFormPrintPage(c *gin.Context) {
	lang := languageFromRequest(c)
	application, err := s.loanRequestApplicationFormData(c.Param("id"))
	if err != nil {
		respondLoanRequestFormPrintError(c, err)
		return
	}
	background, texts, masks, err := loanApplicationFormPrintLayout(application)
	if err != nil {
		respondLoanFormPrintInternalError(c)
		return
	}
	requestID := url.PathEscape(c.Param("id"))
	renderLoanFormPrintPage(c, translate(lang, "loan_application_print"), "/admin/loan-requests#loan-request-review-"+requestID, background, texts, masks)
}

func (s *Server) adminLoanRequestAcceptanceFormPrintPage(c *gin.Context) {
	lang := languageFromRequest(c)
	report, err := s.loanRequestAcceptanceReportData(c.Param("id"), lang)
	if err != nil {
		respondLoanRequestFormPrintError(c, err)
		return
	}
	background, texts, masks, err := loanAcceptanceFormPrintLayout(report)
	if err != nil {
		respondLoanFormPrintInternalError(c)
		return
	}
	requestID := url.PathEscape(c.Param("id"))
	renderLoanFormPrintPage(c, translate(lang, "loan_acceptance_print"), "/admin/loan-requests#loan-request-review-"+requestID, background, texts, masks)
}

func (s *Server) adminLoanDisbursementApplicationFormPrintPage(c *gin.Context) {
	lang := languageFromRequest(c)
	user, _ := currentUser(c)
	if user.Role != "bendahara" {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_loan_disbursement_bendahara_only"))
		return
	}
	application, err := s.loanDisbursementApplicationFormData(c.Param("id"))
	if err != nil {
		respondLoanRequestFormPrintError(c, err)
		return
	}
	background, texts, masks, err := loanApplicationFormPrintLayout(application)
	if err != nil {
		respondLoanFormPrintInternalError(c)
		return
	}
	requestID := url.PathEscape(c.Param("id"))
	renderLoanFormPrintPage(c, translate(lang, "loan_application_print"), "/admin/loan-requests#loan-disbursement-"+requestID, background, texts, masks)
}

func respondLoanRequestFormPrintError(c *gin.Context, err error) {
	lang := languageFromRequest(c)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errDisbursementApplicationFormUnavailable) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error.Loan request not found"))
		return
	}
	respondLoanFormPrintInternalError(c)
}

func respondLoanFormPrintInternalError(c *gin.Context) {
	lang := languageFromRequest(c)
	respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
}

func renderLoanFormPrintPage(c *gin.Context, title, backURL string, background []byte, texts []loanPDFText, masks []loanPDFMask) {
	fields := make([]loanFormPrintField, 0, len(texts))
	for _, item := range texts {
		value := strings.TrimSpace(item.value)
		if value == "" {
			continue
		}
		value = loanPDFASCII(truncateLoanPDFText(value, item.maxRunes))
		fields = append(fields, loanFormPrintField{
			Left:     item.x * 100 / loanFormReferenceWidth,
			Top:      item.y * 100 / loanFormReferenceHeight,
			FontSize: item.size,
			Value:    value,
		})
	}
	printMasks := make([]loanFormPrintMask, 0, len(masks))
	for _, mask := range masks {
		printMasks = append(printMasks, loanFormPrintMask{
			Left:   mask.x * 100 / loanFormReferenceWidth,
			Top:    mask.y * 100 / loanFormReferenceHeight,
			Width:  mask.width * 100 / loanFormReferenceWidth,
			Height: mask.height * 100 / loanFormReferenceHeight,
		})
	}
	renderPage(c, "admin-loan-form-print", gin.H{
		"Lang":              languageFromRequest(c),
		"Title":             title,
		"BackURL":           backURL,
		"BackgroundDataURI": template.URL("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(background)),
		"Fields":            fields,
		"Masks":             printMasks,
	})
}

func (s *Server) loanApplicationFormPrintDataFromReport(report loanReportData) (disbursementApplicationFormData, error) {
	member, err := s.disbursementFormMember(report.Loan.MemberID)
	if err != nil {
		return disbursementApplicationFormData{}, err
	}
	var settleOutstandingLoans bool
	var loanPayoffAmount int64
	if report.Loan.LoanRequestID != "" {
		if err := s.db.QueryRow(`SELECT COALESCE(settle_outstanding_loans,FALSE),COALESCE(loan_payoff_amount,0) FROM loan_requests WHERE id=$1`, report.Loan.LoanRequestID).Scan(&settleOutstandingLoans, &loanPayoffAmount); err != nil {
			return disbursementApplicationFormData{}, err
		}
	}
	if !settleOutstandingLoans {
		loanPayoffAmount = 0
	}
	netReceivedAmount := report.Loan.ApprovedAmount - loanPayoffAmount
	if netReceivedAmount < 0 {
		netReceivedAmount = 0
	}
	return disbursementApplicationFormData{
		ID:                     report.Loan.ID,
		MemberID:               report.Loan.MemberID,
		MemberNo:               member.MemberNo,
		FullName:               report.MemberName,
		MemberType:             report.MemberType,
		MemberStatus:           report.MemberStatus,
		BankName:               member.BankName,
		BankAccount:            member.BankAccount,
		Purpose:                report.Purpose,
		RequestDate:            report.RequestDate,
		RequestedAmount:        report.RequestAmount,
		RequestedDuration:      report.RequestDuration,
		ApprovedAmount:         report.Loan.ApprovedAmount,
		ApprovedDuration:       report.Loan.DurationMonths,
		TotalAdminFee:          report.Loan.TotalAdminFee,
		TotalObligation:        report.Loan.TotalObligation,
		MonthlyInstallment:     report.Loan.MonthlyInstallment,
		SettleOutstandingLoans: settleOutstandingLoans,
		LoanPayoffAmount:       loanPayoffAmount,
		NetReceivedAmount:      netReceivedAmount,
		Savings:                report.Savings,
		PriorCashLoan:          report.PriorCashLoan,
		PriorGoodsLoan:         report.PriorGoodsLoan,
		Approvals:              report.Approvals,
	}, nil
}

func loanApplicationFormPrintLayout(report disbursementApplicationFormData) ([]byte, []loanPDFText, []loanPDFMask, error) {
	background, err := disbursementApplicationFormTemplate.ReadFile("templates/loan-disbursement-application-form.jpg")
	if err != nil {
		return nil, nil, nil, err
	}
	// Keep the print preview aligned with buildLoanApplicationFormPDF.
	masks := []loanPDFMask{{x: 70, y: 686, width: 236, height: 14}}
	return background, loanDisbursementApplicationFormText(report), masks, nil
}

func loanAcceptanceFormPrintLayout(report loanReportData) ([]byte, []loanPDFText, []loanPDFMask, error) {
	background, err := loanReportTemplates.ReadFile("templates/loan-acceptance-form.jpg")
	if err != nil {
		return nil, nil, nil, err
	}
	return background, loanAcceptanceFormText(report), loanAcceptanceFormMasks(report), nil
}
