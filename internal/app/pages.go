package app

import (
	"database/sql"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"
)

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Kopdes Login</title>
  <script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js" integrity="sha384-H5SrcfygHmAuTDZphMHqBJLc3FhssKjG7w/CeCpFReSfwBWDTKpkzPP8c+cLsK+V" crossorigin="anonymous"></script>
  <script src="https://unpkg.com/lucide/dist/umd/lucide.min.js"></script>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body class="auth-page">
  <main class="auth-card">
    <h1>Kopdes</h1>
    <form method="post" action="/api/auth/login" hx-post="/api/auth/login" hx-target="#login-error" hx-swap="innerHTML">
      <label>Email <input name="email" type="email" autocomplete="username" required></label>
      <label>Password <input name="password" type="password" autocomplete="current-password" required></label>
      <button type="submit">Log in</button>
    </form>
    <p id="login-error" class="form-error"></p>
  </main>
</body>
</html>`))

var dashboardTemplate = template.Must(template.New("dashboard").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Admin Dashboard - Kopdes</title>
  <script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js" integrity="sha384-H5SrcfygHmAuTDZphMHqBJLc3FhssKjG7w/CeCpFReSfwBWDTKpkzPP8c+cLsK+V" crossorigin="anonymous"></script>
  <script src="https://unpkg.com/lucide/dist/umd/lucide.min.js"></script>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <div class="admin-shell">
    <aside class="admin-sidebar" aria-label="Admin menu">
      <div class="sidebar-brand">Kopdes</div>
      <nav class="sidebar-nav">
        <a class="sidebar-link active" href="/admin/dashboard" title="Dashboard"><i class="sidebar-icon" data-lucide="layout-dashboard"></i><span class="sidebar-label">Dashboard</span></a>
        <a class="sidebar-link" href="/admin/members" title="Members"><i class="sidebar-icon" data-lucide="users"></i><span class="sidebar-label">Members</span></a>
        <a class="sidebar-link" href="/admin/savings" title="Savings"><i class="sidebar-icon" data-lucide="piggy-bank"></i><span class="sidebar-label">Savings</span></a>
        <a class="sidebar-link" href="/admin/loan-requests" title="Loan requests"><i class="sidebar-icon" data-lucide="file-clock"></i><span class="sidebar-label">Loan requests</span></a>
        <a class="sidebar-link" href="/admin/loans" title="Loans"><i class="sidebar-icon" data-lucide="landmark"></i><span class="sidebar-label">Loans</span></a>
        <a class="sidebar-link" href="/admin/repayments" title="Repayments"><i class="sidebar-icon" data-lucide="receipt"></i><span class="sidebar-label">Repayments</span></a>
      </nav>
    </aside>
    <main class="page-shell">
      <header class="admin-topbar">
        <button class="sidebar-toggle" type="button" aria-label="Toggle sidebar" onclick="document.body.classList.toggle('sidebar-collapsed')">☰</button>
        <form class="logout-form" method="post" action="/logout">
          <button class="button-secondary" type="submit">Logout</button>
        </form>
      </header>
      <header class="page-header">
        <h1>Dashboard</h1>
        <p>Cooperative operating summary.</p>
      </header>
      <section class="summary-grid">
        <article class="summary-card"><span>Total members</span><strong>{{.TotalMembers}}</strong></article>
        <article class="summary-card"><span>Active members</span><strong>{{.ActiveMembers}}</strong></article>
        <article class="summary-card"><span>Total savings</span><strong>{{.TotalSavings}}</strong></article>
        <article class="summary-card"><span>Active loans</span><strong>{{.ActiveLoans}}</strong></article>
        <article class="summary-card"><span>Outstanding loan</span><strong>{{.TotalOutstandingLoan}}</strong></article>
        <article class="summary-card"><span>Pending requests</span><strong>{{.PendingLoanRequests}}</strong></article>
      </section>
    </main>
  </div>
  <script>if (window.lucide) { lucide.createIcons(); }</script>
</body>
</html>`))

var membersTemplate = template.Must(template.New("members").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Members - Kopdes</title>
  <script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js" integrity="sha384-H5SrcfygHmAuTDZphMHqBJLc3FhssKjG7w/CeCpFReSfwBWDTKpkzPP8c+cLsK+V" crossorigin="anonymous"></script>
  <script src="https://unpkg.com/lucide/dist/umd/lucide.min.js"></script>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <div class="admin-shell">
    <aside class="admin-sidebar" aria-label="Admin menu">
      <div class="sidebar-brand">Kopdes</div>
      <nav class="sidebar-nav">
        <a class="sidebar-link" href="/admin/dashboard" title="Dashboard"><i class="sidebar-icon" data-lucide="layout-dashboard"></i><span class="sidebar-label">Dashboard</span></a>
        <a class="sidebar-link active" href="/admin/members" title="Members"><i class="sidebar-icon" data-lucide="users"></i><span class="sidebar-label">Members</span></a>
        <a class="sidebar-link" href="/admin/savings" title="Savings"><i class="sidebar-icon" data-lucide="piggy-bank"></i><span class="sidebar-label">Savings</span></a>
        <a class="sidebar-link" href="/admin/loan-requests" title="Loan requests"><i class="sidebar-icon" data-lucide="file-clock"></i><span class="sidebar-label">Loan requests</span></a>
        <a class="sidebar-link" href="/admin/loans" title="Loans"><i class="sidebar-icon" data-lucide="landmark"></i><span class="sidebar-label">Loans</span></a>
        <a class="sidebar-link" href="/admin/repayments" title="Repayments"><i class="sidebar-icon" data-lucide="receipt"></i><span class="sidebar-label">Repayments</span></a>
      </nav>
    </aside>
    <main class="page-shell">
      <header class="admin-topbar">
        <button class="sidebar-toggle" type="button" aria-label="Toggle sidebar" onclick="document.body.classList.toggle('sidebar-collapsed')">☰</button>
        <form class="logout-form" method="post" action="/logout">
          <button class="button-secondary" type="submit">Logout</button>
        </form>
      </header>
      <header class="page-header">
        <h1>Members</h1>
        <p>Create and inspect cooperative members.</p>
      </header>
      <section class="two-column">
        <article class="panel">
          <h2>Create member</h2>
          <form method="post" action="/api/admin/members" hx-post="/api/admin/members" hx-target="#member-form-error" hx-swap="innerHTML">
            <label>Member number <input name="member_no" required></label>
            <label>Full name <input name="full_name" required></label>
            <label>Phone <input name="phone"></label>
            <label>Address <input name="address"></label>
            <label>Join date <input name="join_date" type="date" required></label>
            <label>Status
              <select name="status">
                <option value="active">active</option>
                <option value="inactive">inactive</option>
                <option value="suspended">suspended</option>
              </select>
            </label>
            <div class="form-section">
              <h3>Member login</h3>
              <label>Email <input name="email" type="email" autocomplete="off"></label>
              <label>Password <input name="password" type="password" autocomplete="new-password"></label>
            </div>
            <button type="submit">Create member</button>
          </form>
          <p id="member-form-error" class="form-error"></p>
        </article>
        <article class="panel">
          <h2>Member list</h2>
          {{if .Members}}
          <div class="table-scroll">
          <table>
            <thead>
              <tr>
                <th>Member no</th>
                <th>Name</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {{range .Members}}
              <tr>
                <td><a href="/admin/members/{{.ID}}">{{.MemberNo}}</a></td>
                <td>{{.FullName}}</td>
                <td><span class="status-badge">{{.Status}}</span></td>
              </tr>
              {{end}}
            </tbody>
          </table>
          </div>
          {{else}}
          <p class="empty-state">No members yet.</p>
          {{end}}
        </article>
      </section>
    </main>
  </div>
  <script>if (window.lucide) { lucide.createIcons(); }</script>
</body>
</html>`))

var savingsTemplate = template.Must(template.New("savings").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Savings - Kopdes</title>
  <script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js" integrity="sha384-H5SrcfygHmAuTDZphMHqBJLc3FhssKjG7w/CeCpFReSfwBWDTKpkzPP8c+cLsK+V" crossorigin="anonymous"></script>
  <script src="https://unpkg.com/lucide/dist/umd/lucide.min.js"></script>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <div class="admin-shell">
    <aside class="admin-sidebar" aria-label="Admin menu">
      <div class="sidebar-brand">Kopdes</div>
      <nav class="sidebar-nav">
        <a class="sidebar-link" href="/admin/dashboard" title="Dashboard"><i class="sidebar-icon" data-lucide="layout-dashboard"></i><span class="sidebar-label">Dashboard</span></a>
        <a class="sidebar-link" href="/admin/members" title="Members"><i class="sidebar-icon" data-lucide="users"></i><span class="sidebar-label">Members</span></a>
        <a class="sidebar-link active" href="/admin/savings" title="Savings"><i class="sidebar-icon" data-lucide="piggy-bank"></i><span class="sidebar-label">Savings</span></a>
        <a class="sidebar-link" href="/admin/loan-requests" title="Loan requests"><i class="sidebar-icon" data-lucide="file-clock"></i><span class="sidebar-label">Loan requests</span></a>
        <a class="sidebar-link" href="/admin/loans" title="Loans"><i class="sidebar-icon" data-lucide="landmark"></i><span class="sidebar-label">Loans</span></a>
        <a class="sidebar-link" href="/admin/repayments" title="Repayments"><i class="sidebar-icon" data-lucide="receipt"></i><span class="sidebar-label">Repayments</span></a>
      </nav>
    </aside>
    <main class="page-shell">
      <header class="admin-topbar">
        <button class="sidebar-toggle" type="button" aria-label="Toggle sidebar" onclick="document.body.classList.toggle('sidebar-collapsed')">☰</button>
        <form class="logout-form" method="post" action="/logout">
          <button class="button-secondary" type="submit">Logout</button>
        </form>
      </header>
      <header class="page-header">
        <h1>Record saving</h1>
        <p>Record verified member saving deposits.</p>
      </header>
      <section class="panel narrow-panel">
        <form method="post" action="/api/admin/savings" hx-post="/api/admin/savings" hx-target="#saving-form-error" hx-swap="innerHTML">
          <label>Member
            <select name="member_id" required>
              {{range .Members}}
              <option value="{{.ID}}">{{.FullName}} - {{.MemberNo}}</option>
              {{end}}
            </select>
          </label>
          <label>Type
            <select name="type">
              <option value="deposit">deposit</option>
              <option value="withdrawal">withdrawal</option>
            </select>
          </label>
          <label>Amount <input name="amount" type="number" min="1" required></label>
          <label>Record date <input name="record_date" type="date" required></label>
          <label>Reference number <input name="reference_no"></label>
          <label>Note <input name="note"></label>
          <button type="submit">Record deposit</button>
        </form>
        <p id="saving-form-error" class="form-error"></p>
      </section>
    </main>
  </div>
  <script>if (window.lucide) { lucide.createIcons(); }</script>
</body>
</html>`))

var memberDetailTemplate = template.Must(template.New("member-detail").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Member detail - Kopdes</title>
  <script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js" integrity="sha384-H5SrcfygHmAuTDZphMHqBJLc3FhssKjG7w/CeCpFReSfwBWDTKpkzPP8c+cLsK+V" crossorigin="anonymous"></script>
  <script src="https://unpkg.com/lucide/dist/umd/lucide.min.js"></script>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <div class="admin-shell">
    <aside class="admin-sidebar" aria-label="Admin menu">
      <div class="sidebar-brand">Kopdes</div>
      <nav class="sidebar-nav">
        <a class="sidebar-link" href="/admin/dashboard" title="Dashboard"><i class="sidebar-icon" data-lucide="layout-dashboard"></i><span class="sidebar-label">Dashboard</span></a>
        <a class="sidebar-link active" href="/admin/members" title="Members"><i class="sidebar-icon" data-lucide="users"></i><span class="sidebar-label">Members</span></a>
        <a class="sidebar-link" href="/admin/savings" title="Savings"><i class="sidebar-icon" data-lucide="piggy-bank"></i><span class="sidebar-label">Savings</span></a>
        <a class="sidebar-link" href="/admin/loan-requests" title="Loan requests"><i class="sidebar-icon" data-lucide="file-clock"></i><span class="sidebar-label">Loan requests</span></a>
        <a class="sidebar-link" href="/admin/loans" title="Loans"><i class="sidebar-icon" data-lucide="landmark"></i><span class="sidebar-label">Loans</span></a>
        <a class="sidebar-link" href="/admin/repayments" title="Repayments"><i class="sidebar-icon" data-lucide="receipt"></i><span class="sidebar-label">Repayments</span></a>
      </nav>
    </aside>
    <main class="page-shell">
      <header class="admin-topbar">
        <button class="sidebar-toggle" type="button" aria-label="Toggle sidebar" onclick="document.body.classList.toggle('sidebar-collapsed')">☰</button>
        <form class="logout-form" method="post" action="/logout">
          <button class="button-secondary" type="submit">Logout</button>
        </form>
      </header>
      <header class="page-header">
        <h1>Member detail</h1>
        <p>{{.FullName}}</p>
      </header>
      <section class="panel detail-grid">
        <div><span>Member no</span><strong>{{.MemberNo}}</strong></div>
        <div><span>Full name</span><strong>{{.FullName}}</strong></div>
        <div><span>Phone</span><strong>{{.Phone}}</strong></div>
        <div><span>Address</span><strong>{{.Address}}</strong></div>
        <div><span>Join date</span><strong>{{.JoinDate}}</strong></div>
        <div><span>Status</span><strong>{{.Status}}</strong></div>
      </section>
    </main>
  </div>
  <script>if (window.lucide) { lucide.createIcons(); }</script>
</body>
</html>`))

var adminLoanRequestsTemplate = template.Must(template.New("admin-loan-requests").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Loan request review - Kopdes</title>
  <script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js" integrity="sha384-H5SrcfygHmAuTDZphMHqBJLc3FhssKjG7w/CeCpFReSfwBWDTKpkzPP8c+cLsK+V" crossorigin="anonymous"></script>
  <script src="https://unpkg.com/lucide/dist/umd/lucide.min.js"></script>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <div class="admin-shell">
    <aside class="admin-sidebar" aria-label="Admin menu">
      <div class="sidebar-brand">Kopdes</div>
      <nav class="sidebar-nav">
        <a class="sidebar-link" href="/admin/dashboard" title="Dashboard"><i class="sidebar-icon" data-lucide="layout-dashboard"></i><span class="sidebar-label">Dashboard</span></a>
        <a class="sidebar-link" href="/admin/members" title="Members"><i class="sidebar-icon" data-lucide="users"></i><span class="sidebar-label">Members</span></a>
        <a class="sidebar-link" href="/admin/savings" title="Savings"><i class="sidebar-icon" data-lucide="piggy-bank"></i><span class="sidebar-label">Savings</span></a>
        <a class="sidebar-link active" href="/admin/loan-requests" title="Loan requests"><i class="sidebar-icon" data-lucide="file-clock"></i><span class="sidebar-label">Loan requests</span></a>
        <a class="sidebar-link" href="/admin/loans" title="Loans"><i class="sidebar-icon" data-lucide="landmark"></i><span class="sidebar-label">Loans</span></a>
        <a class="sidebar-link" href="/admin/repayments" title="Repayments"><i class="sidebar-icon" data-lucide="receipt"></i><span class="sidebar-label">Repayments</span></a>
      </nav>
    </aside>
    <main class="page-shell">
      <header class="admin-topbar">
        <button class="sidebar-toggle" type="button" aria-label="Toggle sidebar" onclick="document.body.classList.toggle('sidebar-collapsed')">☰</button>
        <form class="logout-form" method="post" action="/logout">
          <button class="button-secondary" type="submit">Logout</button>
        </form>
      </header>
      <header class="page-header">
        <h1>Loan request review</h1>
        <p>Inspect pending member loan requests before approval or rejection.</p>
      </header>
      <section class="panel">
        {{if .LoanRequests}}
        <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Member</th>
              <th>Amount</th>
              <th>Duration</th>
              <th>Purpose</th>
              <th>Status</th>
              <th>Created</th>
              <th>Review</th>
            </tr>
          </thead>
          <tbody>
            {{range .LoanRequests}}
            <tr>
              <td><strong>{{.FullName}}</strong><br><span class="table-muted">{{.MemberNo}}</span></td>
              <td>{{.RequestedAmount}}</td>
              <td>{{.DurationMonths}} months</td>
              <td>{{.Purpose}}</td>
              <td><span class="status-badge">{{.Status}}</span></td>
              <td>{{.CreatedAt}}</td>
              <td>
                <form class="inline-approval-form" method="post" action="/api/admin/loan-requests/{{.ID}}/approve" hx-post="/api/admin/loan-requests/{{.ID}}/approve" hx-target="#loan-review-error" hx-swap="innerHTML">
                  <input name="approved_amount" type="number" min="1" value="{{.RequestedAmount}}" aria-label="Approved amount">
                  <input name="duration_months" type="number" min="1" value="{{.DurationMonths}}" aria-label="Duration months">
                  <div class="table-actions">
                    <button type="submit">Approve</button>
                  </div>
                </form>
                <form class="inline-rejection-form" method="post" action="/api/admin/loan-requests/{{.ID}}/reject" hx-post="/api/admin/loan-requests/{{.ID}}/reject" hx-target="#loan-review-error" hx-swap="innerHTML">
                  <input name="rejection_reason" aria-label="Rejection reason" placeholder="Reason">
                  <button class="button-secondary" type="submit">Reject</button>
                </form>
              </td>
            </tr>
            {{end}}
          </tbody>
        </table>
        </div>
        {{else}}
        <p class="empty-state">No pending loan requests.</p>
        {{end}}
        <p id="loan-review-error" class="form-error"></p>
      </section>
    </main>
  </div>
  <script>if (window.lucide) { lucide.createIcons(); }</script>
</body>
</html>`))

var adminLoansTemplate = template.Must(template.New("admin-loans").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Active loans - Kopdes</title>
  <script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js" integrity="sha384-H5SrcfygHmAuTDZphMHqBJLc3FhssKjG7w/CeCpFReSfwBWDTKpkzPP8c+cLsK+V" crossorigin="anonymous"></script>
  <script src="https://unpkg.com/lucide/dist/umd/lucide.min.js"></script>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <div class="admin-shell">
    <aside class="admin-sidebar" aria-label="Admin menu">
      <div class="sidebar-brand">Kopdes</div>
      <nav class="sidebar-nav">
        <a class="sidebar-link" href="/admin/dashboard" title="Dashboard"><i class="sidebar-icon" data-lucide="layout-dashboard"></i><span class="sidebar-label">Dashboard</span></a>
        <a class="sidebar-link" href="/admin/members" title="Members"><i class="sidebar-icon" data-lucide="users"></i><span class="sidebar-label">Members</span></a>
        <a class="sidebar-link" href="/admin/savings" title="Savings"><i class="sidebar-icon" data-lucide="piggy-bank"></i><span class="sidebar-label">Savings</span></a>
        <a class="sidebar-link" href="/admin/loan-requests" title="Loan requests"><i class="sidebar-icon" data-lucide="file-clock"></i><span class="sidebar-label">Loan requests</span></a>
        <a class="sidebar-link active" href="/admin/loans" title="Loans"><i class="sidebar-icon" data-lucide="landmark"></i><span class="sidebar-label">Loans</span></a>
        <a class="sidebar-link" href="/admin/repayments" title="Repayments"><i class="sidebar-icon" data-lucide="receipt"></i><span class="sidebar-label">Repayments</span></a>
      </nav>
    </aside>
    <main class="page-shell">
      <header class="admin-topbar">
        <button class="sidebar-toggle" type="button" aria-label="Toggle sidebar" onclick="document.body.classList.toggle('sidebar-collapsed')">☰</button>
        <form class="logout-form" method="post" action="/logout">
          <button class="button-secondary" type="submit">Logout</button>
        </form>
      </header>
      <header class="page-header">
        <h1>Active loans</h1>
        <p>Monitor approved cooperative loans and remaining balances.</p>
      </header>
      <section class="panel">
        {{if .Loans}}
        <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Member</th>
              <th>Approved</th>
              <th>Installment</th>
              <th>Remaining</th>
              <th>Duration</th>
              <th>Status</th>
              <th>Repayment</th>
            </tr>
          </thead>
          <tbody>
            {{range .Loans}}
            <tr>
              <td><strong>{{.FullName}}</strong><br><span class="table-muted">{{.MemberNo}}</span></td>
              <td>{{.ApprovedAmount}}</td>
              <td>{{.MonthlyInstallment}}</td>
              <td>{{.RemainingBalance}}</td>
              <td>{{.DurationMonths}} months</td>
              <td><span class="status-badge">{{.Status}}</span></td>
              <td>
                <form class="inline-repayment-form" method="post" action="/api/admin/loans/{{.ID}}/repayments" hx-post="/api/admin/loans/{{.ID}}/repayments" hx-target="#repayment-form-error" hx-swap="innerHTML">
                  <input name="amount" type="number" min="1" max="{{.RemainingBalance}}" placeholder="Amount" aria-label="Repayment amount">
                  <input name="record_date" type="date" aria-label="Repayment date">
                  <input name="reference_no" placeholder="Reference" aria-label="Reference number">
                  <input name="note" placeholder="Note" aria-label="Repayment note">
                  <button type="submit">Record repayment</button>
                </form>
              </td>
            </tr>
            {{end}}
          </tbody>
        </table>
        </div>
        {{else}}
        <p class="empty-state">No active loans.</p>
        {{end}}
        <p id="repayment-form-error" class="form-error"></p>
      </section>
    </main>
  </div>
  <script>if (window.lucide) { lucide.createIcons(); }</script>
</body>
</html>`))

var adminRepaymentsTemplate = template.Must(template.New("admin-repayments").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Repayments - Kopdes</title>
  <script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js" integrity="sha384-H5SrcfygHmAuTDZphMHqBJLc3FhssKjG7w/CeCpFReSfwBWDTKpkzPP8c+cLsK+V" crossorigin="anonymous"></script>
  <script src="https://unpkg.com/lucide/dist/umd/lucide.min.js"></script>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <div class="admin-shell">
    <aside class="admin-sidebar" aria-label="Admin menu">
      <div class="sidebar-brand">Kopdes</div>
      <nav class="sidebar-nav">
        <a class="sidebar-link" href="/admin/dashboard" title="Dashboard"><i class="sidebar-icon" data-lucide="layout-dashboard"></i><span class="sidebar-label">Dashboard</span></a>
        <a class="sidebar-link" href="/admin/members" title="Members"><i class="sidebar-icon" data-lucide="users"></i><span class="sidebar-label">Members</span></a>
        <a class="sidebar-link" href="/admin/savings" title="Savings"><i class="sidebar-icon" data-lucide="piggy-bank"></i><span class="sidebar-label">Savings</span></a>
        <a class="sidebar-link" href="/admin/loan-requests" title="Loan requests"><i class="sidebar-icon" data-lucide="file-clock"></i><span class="sidebar-label">Loan requests</span></a>
        <a class="sidebar-link" href="/admin/loans" title="Loans"><i class="sidebar-icon" data-lucide="landmark"></i><span class="sidebar-label">Loans</span></a>
        <a class="sidebar-link active" href="/admin/repayments" title="Repayments"><i class="sidebar-icon" data-lucide="receipt"></i><span class="sidebar-label">Repayments</span></a>
      </nav>
    </aside>
    <main class="page-shell">
      <header class="admin-topbar">
        <button class="sidebar-toggle" type="button" aria-label="Toggle sidebar" onclick="document.body.classList.toggle('sidebar-collapsed')">☰</button>
        <form class="logout-form" method="post" action="/logout">
          <button class="button-secondary" type="submit">Logout</button>
        </form>
      </header>
      <header class="page-header">
        <h1>Repayments</h1>
        <p>Review recorded loan repayments.</p>
      </header>
      <section class="panel">
        {{if .Repayments}}
        <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Date</th>
              <th>Member</th>
              <th>Amount</th>
              <th>Reference</th>
              <th>Note</th>
            </tr>
          </thead>
          <tbody>
            {{range .Repayments}}
            <tr>
              <td>{{.RecordDate}}</td>
              <td><strong>{{.FullName}}</strong><br><span class="table-muted">{{.MemberNo}}</span></td>
              <td>{{.Amount}}</td>
              <td>{{.ReferenceNo}}</td>
              <td>{{.Note}}</td>
            </tr>
            {{end}}
          </tbody>
        </table>
        </div>
        {{else}}
        <p class="empty-state">No repayments yet.</p>
        {{end}}
      </section>
    </main>
  </div>
  <script>if (window.lucide) { lucide.createIcons(); }</script>
</body>
</html>`))

var memberProfileTemplate = template.Must(template.New("member-profile").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Member profile - Kopdes</title>
  <script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js" integrity="sha384-H5SrcfygHmAuTDZphMHqBJLc3FhssKjG7w/CeCpFReSfwBWDTKpkzPP8c+cLsK+V" crossorigin="anonymous"></script>
  <script src="https://unpkg.com/lucide@latest/dist/umd/lucide.min.js"></script>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <main class="member-shell member-profile-shell">
    <header class="member-topbar">
      <div class="sidebar-brand">Kopdes</div>
      <nav class="member-nav">
        <a href="/member/profile">Profile</a>
        <a href="/member/loan-requests">Loan requests</a>
      </nav>
      <form class="logout-form" method="post" action="/logout">
        <button class="button-secondary" type="submit">Logout</button>
      </form>
    </header>
    <section class="page-header">
      <h1>Member profile</h1>
      <p>{{.Member.FullName}}</p>
    </section>
    <section class="summary-grid">
      <article class="summary-card"><span>Saving balance</span><strong>{{.Summary.CurrentBalance}}</strong></article>
      <article class="summary-card"><span>Total deposits</span><strong>{{.Summary.TotalDeposit}}</strong></article>
      <article class="summary-card"><span>Total withdrawals</span><strong>{{.Summary.TotalWithdrawal}}</strong></article>
    </section>
    {{if .ActiveLoan}}
    <section class="panel">
      <h2>Active loan</h2>
      <div class="detail-grid">
        <div><span>Approved amount</span><strong>{{.ActiveLoan.ApprovedAmount}}</strong></div>
        <div><span>Monthly installment</span><strong>{{.ActiveLoan.MonthlyInstallment}}</strong></div>
        <div><span>Remaining balance</span><strong>{{.ActiveLoan.RemainingBalance}}</strong></div>
        <div><span>Duration</span><strong>{{.ActiveLoan.DurationMonths}} months</strong></div>
        <div><span>Status</span><strong>{{.ActiveLoan.Status}}</strong></div>
      </div>
    </section>
    {{end}}
    <section class="panel detail-grid">
      <div><span>Member no</span><strong>{{.Member.MemberNo}}</strong></div>
      <div><span>Full name</span><strong>{{.Member.FullName}}</strong></div>
      <div><span>Phone</span><strong>{{.Member.Phone}}</strong></div>
      <div><span>Address</span><strong>{{.Member.Address}}</strong></div>
      <div><span>Join date</span><strong>{{.Member.JoinDate}}</strong></div>
      <div><span>Status</span><strong>{{.Member.Status}}</strong></div>
    </section>
    <section class="panel">
      <h2>Saving history</h2>
      {{if .Savings}}
      <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Date</th>
            <th>Type</th>
            <th>Amount</th>
            <th>Reference</th>
            <th>Note</th>
          </tr>
        </thead>
        <tbody>
          {{range .Savings}}
          <tr>
            <td>{{.RecordDate}}</td>
            <td>{{.Type}}</td>
            <td>{{.Amount}}</td>
            <td>{{.ReferenceNo}}</td>
            <td>{{.Note}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
      </div>
      {{else}}
      <p class="empty-state">No saving records yet.</p>
      {{end}}
    </section>
    <section class="panel">
      <h2>Repayment history</h2>
      {{if .Repayments}}
      <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Date</th>
            <th>Amount</th>
            <th>Reference</th>
            <th>Note</th>
          </tr>
        </thead>
        <tbody>
          {{range .Repayments}}
          <tr>
            <td>{{.RecordDate}}</td>
            <td>{{.Amount}}</td>
            <td>{{.ReferenceNo}}</td>
            <td>{{.Note}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
      </div>
      {{else}}
      <p class="empty-state">No repayments yet.</p>
      {{end}}
    </section>
  </main>
</body>
</html>`))

var memberLoanRequestsTemplate = template.Must(template.New("member-loan-requests").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Loan requests - Kopdes</title>
  <script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.10/dist/htmx.min.js" integrity="sha384-H5SrcfygHmAuTDZphMHqBJLc3FhssKjG7w/CeCpFReSfwBWDTKpkzPP8c+cLsK+V" crossorigin="anonymous"></script>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <main class="member-shell member-loan-requests-shell">
    <header class="member-topbar">
      <div class="sidebar-brand">Kopdes</div>
      <nav class="member-nav">
        <a href="/member/profile">Profile</a>
        <a href="/member/loan-requests">Loan requests</a>
      </nav>
      <form class="logout-form" method="post" action="/logout">
        <button class="button-secondary" type="submit">Logout</button>
      </form>
    </header>
    <section class="page-header">
      <h1>Loan requests</h1>
      <p>Submit and track cooperative loan requests.</p>
    </section>
    <section class="two-column">
      <article class="panel">
        <h2>Submit loan request</h2>
        <form method="post" action="/api/member/loan-requests" hx-post="/api/member/loan-requests" hx-target="#loan-request-error" hx-swap="innerHTML">
          <label>Requested amount <input name="requested_amount" type="number" min="1" required></label>
          <label>Duration months <input name="duration_months" type="number" min="1" required></label>
          <label>Purpose <input name="purpose"></label>
          <button type="submit">Submit loan request</button>
        </form>
        <p id="loan-request-error" class="form-error"></p>
      </article>
      <article class="panel">
        <h2>Request history</h2>
        {{if .LoanRequests}}
        <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Amount</th>
              <th>Duration</th>
              <th>Status</th>
              <th>Purpose</th>
            </tr>
          </thead>
          <tbody>
            {{range .LoanRequests}}
            <tr>
              <td>{{.RequestedAmount}}</td>
              <td>{{.DurationMonths}}</td>
              <td><span class="status-badge">{{.Status}}</span></td>
              <td>{{.Purpose}}</td>
            </tr>
            {{end}}
          </tbody>
        </table>
        </div>
        {{else}}
        <p class="empty-state">No loan requests yet.</p>
        {{end}}
      </article>
    </section>
  </main>
</body>
</html>`))

func (s *Server) loginPage(c *gin.Context) {
	c.Status(http.StatusOK)
	_ = loginTemplate.Execute(c.Writer, nil)
}

func (s *Server) adminDashboardPage(c *gin.Context) {
	summary, err := s.adminDashboardSummary()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.Status(http.StatusOK)
	_ = dashboardTemplate.Execute(c.Writer, summary)
}

func (s *Server) adminMembersPage(c *gin.Context) {
	members, err := s.allMembers()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.Status(http.StatusOK)
	_ = membersTemplate.Execute(c.Writer, gin.H{"Members": members})
}

func (s *Server) adminSavingsPage(c *gin.Context) {
	members, err := s.allMembers()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.Status(http.StatusOK)
	_ = savingsTemplate.Execute(c.Writer, gin.H{"Members": members})
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
	c.Status(http.StatusOK)
	_ = memberDetailTemplate.Execute(c.Writer, member)
}

func (s *Server) adminLoanRequestsPage(c *gin.Context) {
	requests, err := s.loanRequestsForAdmin("pending")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.Status(http.StatusOK)
	_ = adminLoanRequestsTemplate.Execute(c.Writer, gin.H{"LoanRequests": requests})
}

func (s *Server) adminLoansPage(c *gin.Context) {
	loans, err := s.loansForAdmin("active")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.Status(http.StatusOK)
	_ = adminLoansTemplate.Execute(c.Writer, gin.H{"Loans": loans})
}

func (s *Server) adminRepaymentsPage(c *gin.Context) {
	repayments, err := s.repaymentsForAdmin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.Status(http.StatusOK)
	_ = adminRepaymentsTemplate.Execute(c.Writer, gin.H{"Repayments": repayments})
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
	c.Status(http.StatusOK)
	_ = memberProfileTemplate.Execute(c.Writer, gin.H{
		"Member":     member,
		"Summary":    summary,
		"Savings":    savings,
		"ActiveLoan": activeLoan,
		"Repayments": repayments,
	})
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
	c.Status(http.StatusOK)
	_ = memberLoanRequestsTemplate.Execute(c.Writer, gin.H{"LoanRequests": requests})
}
