package app

import (
	"bytes"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed templates/*.tmpl
var pageTemplateFS embed.FS

var pageTemplates = template.Must(template.New("").Funcs(template.FuncMap{
	"dict": templateDict,
}).ParseFS(pageTemplateFS, "templates/*.tmpl"))

func templateDict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict requires key-value pairs")
	}
	result := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict keys must be strings")
		}
		result[key] = values[i+1]
	}
	return result, nil
}

func renderPage(c *gin.Context, name string, data gin.H) {
	var body bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&body, name, data); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(body.Bytes())
}

func pageData(title, active, heading, description string, values gin.H) gin.H {
	if values == nil {
		values = gin.H{}
	}
	values["Title"] = title
	values["Active"] = active
	values["Heading"] = heading
	values["Description"] = description
	return values
}

func (s *Server) loginPage(c *gin.Context) {
	renderPage(c, "login", pageData("Kopdes Login", "", "", "", nil))
}

func (s *Server) adminDashboardPage(c *gin.Context) {
	summary, err := s.adminDashboardSummary()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-dashboard", pageData("Admin Dashboard - Kopdes", "dashboard", "Dashboard", "Cooperative operating summary.", gin.H{
		"Summary": summary,
	}))
}

func (s *Server) adminMembersPage(c *gin.Context) {
	members, err := s.allMembers()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-members", pageData("Members - Kopdes", "members", "Members", "Create and inspect cooperative members.", gin.H{
		"Members": members,
	}))
}

func (s *Server) adminSavingsPage(c *gin.Context) {
	members, err := s.allMembers()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-savings", pageData("Savings - Kopdes", "savings", "Record saving", "Record verified member saving deposits.", gin.H{
		"Members":     members,
		"CurrentDate": time.Now().Format("2006-01-02"),
	}))
}

func (s *Server) adminMemberDetailPage(c *gin.Context) {
	member, err := s.memberByID(c.Param("id"))
	if err == sql.ErrNoRows {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Member not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	summary, err := s.savingSummary(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	savings, err := s.savingsByMember(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	requests, err := s.loanRequestsByMember(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	var activeLoan any
	loan, err := s.activeLoanByMember(member.ID)
	if err == nil {
		activeLoan = loan
	}
	if err != nil && err != sql.ErrNoRows {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	repayments, err := s.repaymentsByMember(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-member-detail", pageData("Member detail - Kopdes", "members", "Member detail", member.FullName, gin.H{
		"Member":       member,
		"Summary":      summary,
		"Savings":      savings,
		"LoanRequests": requests,
		"ActiveLoan":   activeLoan,
		"Repayments":   repayments,
	}))
}

func (s *Server) adminLoanRequestsPage(c *gin.Context) {
	requests, err := s.loanRequestsForAdmin("pending")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-loan-requests", pageData("Loan request review - Kopdes", "loan-requests", "Loan request review", "Inspect pending member loan requests before approval or rejection.", gin.H{
		"LoanRequests": requests,
	}))
}

func (s *Server) adminLoansPage(c *gin.Context) {
	loans, err := s.loansForAdmin("active")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-loans", pageData("Active loans - Kopdes", "loans", "Active loans", "Monitor approved cooperative loans and remaining balances.", gin.H{
		"Loans": loans,
	}))
}

func (s *Server) adminRepaymentsPage(c *gin.Context) {
	repayments, err := s.repaymentsForAdmin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-repayments", pageData("Repayments - Kopdes", "repayments", "Repayments", "Review recorded loan repayments.", gin.H{
		"Repayments": repayments,
	}))
}

func (s *Server) memberProfilePage(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}
	summary, err := s.savingSummary(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	savings, err := s.savingsByMember(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	var activeLoan any
	loan, err := s.activeLoanByMember(member.ID)
	if err == nil {
		activeLoan = loan
	}
	if err != nil && err != sql.ErrNoRows {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	repayments, err := s.repaymentsByMember(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "member-profile", pageData("Member profile - Kopdes", "profile", "Member profile", member.FullName, gin.H{
		"Member":     member,
		"Summary":    summary,
		"Savings":    savings,
		"ActiveLoan": activeLoan,
		"Repayments": repayments,
		"ShellClass": "member-profile-shell",
	}))
}

func (s *Server) memberDashboardPage(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}
	dashboard, err := s.memberDashboardSummary(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "member-dashboard", pageData("Member dashboard - Kopdes", "dashboard", "Dashboard", member.FullName, gin.H{
		"Member":     member,
		"Dashboard":  dashboard,
		"ShellClass": "member-dashboard-shell",
	}))
}

func (s *Server) memberLoanRequestsPage(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}
	requests, err := s.loanRequestsByMember(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "member-loan-requests", pageData("Loan requests - Kopdes", "loan-requests", "Loan requests", "Submit and track cooperative loan requests.", gin.H{
		"LoanRequests": requests,
		"ShellClass":   "member-loan-requests-shell",
	}))
}
