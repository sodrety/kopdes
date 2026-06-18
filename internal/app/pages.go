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
	"t":    translateTemplate,
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

func pageData(c *gin.Context, title, active, heading, description string, values gin.H) gin.H {
	if values == nil {
		values = gin.H{}
	}
	values["Lang"] = languageFromRequest(c)
	values["CurrentPath"] = c.Request.URL.RequestURI()
	values["Title"] = title
	values["Active"] = active
	values["Heading"] = heading
	values["Description"] = description
	return values
}

func (s *Server) loginPage(c *gin.Context) {
	renderPage(c, "login", pageData(c, "Kopdes Login", "", "", "", nil))
}

func (s *Server) adminDashboardPage(c *gin.Context) {
	summary, err := s.adminDashboardSummary()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-dashboard", pageData(c, "Admin Dashboard - Kopdes", "dashboard", "dashboard", "operating_summary", gin.H{
		"Summary": summary,
	}))
}

func (s *Server) adminMembersPage(c *gin.Context) {
	members, err := s.allMembers()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-members", pageData(c, "Members - Kopdes", "members", "members", "member_list", gin.H{
		"Members": members,
	}))
}

func (s *Server) adminMemberNewPage(c *gin.Context) {
	renderPage(c, "admin-member-new", pageData(c, "Create member - Kopdes", "members", "create_member", "create_and_inspect_members", nil))
}

func (s *Server) adminSavingsPage(c *gin.Context) {
	members, err := s.allMembers()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	filters := savingFiltersFromQuery(c)
	savings, err := s.savingsForAdmin(filters)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-savings", pageData(c, "Savings - Kopdes", "savings", "saving_records", "record_saving_deposits", gin.H{
		"Members":     members,
		"Filters":     filters,
		"Savings":     savings,
		"CurrentDate": time.Now().Format("2006-01-02"),
	}))
}

func (s *Server) adminSavingNewPage(c *gin.Context) {
	members, err := s.allMembers()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-saving-new", pageData(c, "Record saving - Kopdes", "savings", "record_saving", "record_saving_deposits", gin.H{
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
	renderPage(c, "admin-member-detail", pageData(c, "Member detail - Kopdes", "members", "member_detail", member.FullName, gin.H{
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
	renderPage(c, "admin-loan-requests", pageData(c, "Loan request review - Kopdes", "loan-requests", "loan_request_review", "inspect_pending_loan_requests", gin.H{
		"LoanRequests": requests,
	}))
}

func (s *Server) adminLoansPage(c *gin.Context) {
	loans, err := s.loansForAdmin("active")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-loans", pageData(c, "Active loans - Kopdes", "loans", "active_loans", "monitor_loans", gin.H{
		"Loans": loans,
	}))
}

func (s *Server) adminRepaymentsPage(c *gin.Context) {
	repayments, err := s.repaymentsForAdmin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-repayments", pageData(c, "Repayments - Kopdes", "repayments", "repayments", "review_repayments", gin.H{
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
	renderPage(c, "member-profile", pageData(c, "Member profile - Kopdes", "profile", "member_profile", member.FullName, gin.H{
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
	renderPage(c, "member-dashboard", pageData(c, "Member dashboard - Kopdes", "dashboard", "dashboard", member.FullName, gin.H{
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
	renderPage(c, "member-loan-requests", pageData(c, "Loan requests - Kopdes", "loan-requests", "loan_requests", "track_loan_requests", gin.H{
		"LoanRequests": requests,
		"ShellClass":   "member-loan-requests-shell",
	}))
}
