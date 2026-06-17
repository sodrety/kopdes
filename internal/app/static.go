package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const appCSS = `:root {
  --primary: #9fe870;
  --primary-pale: #e2f6d5;
  --ink: #0e0f0c;
  --body: #454745;
  --mute: #868685;
  --canvas: #ffffff;
  --canvas-soft: #e8ebe6;
  --negative: #d03238;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  background: var(--canvas-soft);
  color: var(--ink);
  font-family: Inter, system-ui, -apple-system, sans-serif;
  font-size: 16px;
  line-height: 24px;
}
.auth-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 32px;
}
.auth-card, .summary-card {
  background: var(--canvas);
  border-radius: 24px;
  padding: 24px;
}
.auth-card {
  width: min(100%, 420px);
}
h1 {
  margin: 0 0 24px;
  font-size: 40px;
  line-height: 40px;
  font-weight: 900;
}
form {
  display: grid;
  gap: 16px;
}
label {
  display: grid;
  gap: 8px;
  color: var(--body);
  font-size: 14px;
  font-weight: 600;
}
input, select {
	width: 100%;
	border: 1px solid var(--ink);
	border-radius: 12px;
	padding: 12px 16px;
	color: var(--ink);
	font: inherit;
	background: var(--canvas);
}
button {
  border: 0;
  border-radius: 24px;
  padding: 12px 24px;
  background: var(--primary);
  color: var(--ink);
  font: inherit;
  font-weight: 600;
  cursor: pointer;
}
button:disabled {
  opacity: 0.48;
  cursor: not-allowed;
}
.button-secondary {
  background: var(--canvas-soft);
}
.form-error {
  min-height: 20px;
  color: var(--negative);
}
.form-section {
  display: grid;
  gap: 16px;
  border-top: 1px solid var(--canvas-soft);
  padding-top: 16px;
}
.form-section h3 {
  margin: 0;
  font-size: 16px;
  line-height: 24px;
}
.nav-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 24px;
  background: var(--canvas);
}
.admin-shell {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  transition: grid-template-columns 160ms ease;
}
.sidebar-collapsed .admin-shell {
  grid-template-columns: 84px minmax(0, 1fr);
}
.admin-sidebar {
  position: sticky;
  top: 0;
  height: 100vh;
  display: flex;
  flex-direction: column;
  gap: 32px;
  padding: 24px;
  background: var(--ink);
  color: var(--primary);
  overflow: hidden;
}
.sidebar-brand {
  font-size: 32px;
  line-height: 34px;
  font-weight: 900;
  white-space: nowrap;
}
.sidebar-collapsed .sidebar-brand {
  font-size: 18px;
  line-height: 24px;
}
.sidebar-nav {
  display: grid;
  gap: 8px;
}
.sidebar-link {
  display: flex;
  align-items: center;
  gap: 12px;
  border-radius: 12px;
  padding: 12px 16px;
  color: var(--canvas-soft);
  text-decoration: none;
  font-size: 14px;
  line-height: 20px;
  font-weight: 600;
  white-space: nowrap;
}
.sidebar-icon {
  width: 20px;
  height: 20px;
  flex: 0 0 20px;
  stroke-width: 2.2;
}
.sidebar-label {
  overflow: hidden;
  text-overflow: ellipsis;
}
.sidebar-link.active {
  background: var(--primary);
  color: var(--ink);
}
.sidebar-link.disabled {
  color: rgba(232, 235, 230, 0.48);
  cursor: default;
}
.sidebar-collapsed .sidebar-link {
  justify-content: center;
  padding: 12px;
  overflow: hidden;
}
.sidebar-collapsed .sidebar-label {
  display: none;
}
.admin-topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 32px;
}
.sidebar-toggle {
  width: 44px;
  height: 44px;
  display: inline-grid;
  place-items: center;
  border-radius: 9999px;
  padding: 0;
  background: var(--canvas);
  border: 1px solid var(--canvas-soft);
}
.logout-form {
  display: block;
}
.member-shell {
  width: min(960px, 100%);
  margin: 0 auto;
  padding: 32px 24px 48px;
}
.member-topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 48px;
  flex-wrap: wrap;
}
.member-nav {
  display: flex;
  gap: 16px;
  align-items: center;
  flex-wrap: wrap;
}
.member-nav a {
  border-radius: 9999px;
  padding: 8px 12px;
  background: var(--canvas);
  text-decoration: none;
  white-space: nowrap;
}
.page-shell {
  width: min(1120px, 100%);
  margin: 0 auto;
  padding: 48px 24px;
}
.page-header {
  margin-bottom: 24px;
}
.page-header p {
  color: var(--body);
}
.page-shell > section + section,
.member-shell > section + section {
  margin-top: 24px;
}
.summary-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 16px;
}
.summary-card {
  display: grid;
  gap: 16px;
}
.summary-card span {
  color: var(--body);
  font-size: 14px;
  font-weight: 600;
}
.summary-card strong {
	font-size: 32px;
	line-height: 38px;
}
a {
  color: var(--ink);
  font-weight: 600;
}
.two-column {
  display: grid;
  grid-template-columns: minmax(280px, 0.8fr) minmax(320px, 1.2fr);
  gap: 24px;
  align-items: start;
}
.panel {
  background: var(--canvas);
  border-radius: 24px;
  padding: 24px;
}
.narrow-panel {
  max-width: 560px;
}
h2 {
  margin: 0 0 16px;
  font-size: 24px;
  line-height: 31px;
}
table {
  width: 100%;
  border-collapse: collapse;
}
.table-scroll {
  width: 100%;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}
.table-scroll table {
  min-width: 560px;
}
th, td {
  padding: 12px 0;
  border-bottom: 1px solid var(--canvas-soft);
  text-align: left;
}
th {
  color: var(--body);
  font-size: 12px;
  line-height: 16px;
  text-transform: uppercase;
}
.table-muted {
  color: var(--mute);
  font-size: 14px;
}
.table-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.table-actions button {
	padding: 8px 12px;
	font-size: 14px;
}
.inline-approval-form {
	display: grid;
	grid-template-columns: minmax(112px, 1fr) minmax(80px, 0.6fr);
	gap: 8px;
	min-width: 240px;
}
.inline-approval-form .table-actions {
	grid-column: 1 / -1;
}
.inline-approval-form input {
	border-radius: 8px;
	padding: 8px 10px;
	font-size: 14px;
}
.inline-rejection-form {
	display: grid;
	grid-template-columns: minmax(160px, 1fr) auto;
	gap: 8px;
	min-width: 240px;
	margin-top: 8px;
}
.inline-rejection-form input {
	border-radius: 8px;
	padding: 8px 10px;
	font-size: 14px;
}
.inline-rejection-form button {
	padding: 8px 12px;
	font-size: 14px;
}
.inline-repayment-form {
	display: grid;
	grid-template-columns: repeat(2, minmax(112px, 1fr));
	gap: 8px;
	min-width: 260px;
}
.inline-repayment-form input {
	border-radius: 8px;
	padding: 8px 10px;
	font-size: 14px;
}
.inline-repayment-form button {
	grid-column: 1 / -1;
	padding: 8px 12px;
	font-size: 14px;
}
.status-badge {
	display: inline-flex;
  border-radius: 9999px;
  padding: 4px 12px;
  background: var(--primary-pale);
  color: var(--ink);
  font-size: 14px;
  font-weight: 600;
}
.empty-state {
  color: var(--mute);
}
.detail-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 24px;
}
.detail-grid div {
  display: grid;
  gap: 8px;
}
.detail-grid span {
  color: var(--body);
  font-size: 14px;
  font-weight: 600;
}
@media (max-width: 760px) {
  .admin-shell {
    grid-template-columns: 1fr;
    min-width: 0;
    overflow-x: hidden;
  }
  .sidebar-collapsed .admin-shell {
    grid-template-columns: 1fr;
  }
  .admin-sidebar {
    position: static;
    height: auto;
    gap: 12px;
    padding: 16px;
    max-width: 100vw;
    overflow: hidden;
  }
  .admin-sidebar .sidebar-brand {
    font-size: 24px;
    line-height: 28px;
  }
  .sidebar-collapsed .sidebar-brand {
    font-size: 24px;
    line-height: 28px;
  }
  .sidebar-collapsed .sidebar-link {
    padding: 10px 12px;
    justify-content: flex-start;
  }
  .sidebar-collapsed .sidebar-label {
    display: inline;
  }
  .sidebar-nav {
    display: flex;
    gap: 8px;
    min-width: 0;
    max-width: 100%;
    overflow-x: auto;
    padding-bottom: 4px;
    -webkit-overflow-scrolling: touch;
  }
  .sidebar-link {
    flex: 0 0 auto;
    gap: 8px;
    padding: 10px 12px;
    border-radius: 9999px;
    font-size: 13px;
    line-height: 18px;
  }
  .sidebar-icon {
    width: 18px;
    height: 18px;
    flex-basis: 18px;
  }
  .page-shell {
    width: 100%;
    max-width: 100%;
    min-width: 0;
    padding: 24px 16px 40px;
  }
  .admin-topbar {
    margin-bottom: 24px;
  }
  .page-header {
    margin-bottom: 16px;
  }
  .page-header h1 {
    margin-bottom: 12px;
  }
  .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .two-column {
    grid-template-columns: 1fr;
  }
  .page-shell .panel,
  .page-shell .summary-card {
    border-radius: 16px;
    padding: 16px;
  }
  .inline-approval-form,
  .inline-rejection-form,
  .inline-repayment-form {
    min-width: 0;
  }
  .inline-approval-form,
  .inline-repayment-form {
    grid-template-columns: 1fr;
  }
  .inline-rejection-form {
    grid-template-columns: 1fr;
  }
  .inline-approval-form input,
  .inline-rejection-form input,
  .inline-repayment-form input,
  .inline-approval-form button,
  .inline-rejection-form button,
  .inline-repayment-form button {
    width: 100%;
  }
  .table-scroll td:last-child {
    min-width: 220px;
  }
  .narrow-panel {
    max-width: none;
  }
  .member-shell {
    padding: 24px 16px 40px;
  }
  .member-topbar {
    align-items: flex-start;
    gap: 12px;
    margin-bottom: 32px;
  }
  .member-topbar .sidebar-brand {
    width: 100%;
  }
  .member-nav {
    gap: 8px;
  }
  .member-nav a {
    padding: 8px 10px;
    font-size: 14px;
    line-height: 20px;
  }
  .member-topbar .logout-form {
    margin-left: auto;
  }
  .member-topbar .button-secondary {
    padding: 8px 14px;
    font-size: 14px;
    line-height: 20px;
  }
  .member-profile-shell .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .member-profile-shell .summary-card,
  .member-profile-shell .panel,
  .member-loan-requests-shell .panel {
    border-radius: 16px;
    padding: 16px;
  }
  .member-profile-shell .summary-card {
    gap: 8px;
  }
  .member-profile-shell .summary-card:first-child {
    grid-column: 1 / -1;
  }
  .summary-card strong {
    font-size: 28px;
    line-height: 34px;
    overflow-wrap: anywhere;
  }
  .detail-grid {
    grid-template-columns: 1fr;
    gap: 16px;
  }
  .member-loan-requests-shell .two-column {
    gap: 16px;
  }
  .member-loan-requests-shell form button {
    width: 100%;
  }
}
@media (max-width: 420px) {
  h1 {
    font-size: 32px;
    line-height: 34px;
  }
  .member-shell {
    padding: 20px 12px 32px;
  }
  .page-shell {
    padding: 20px 12px 32px;
  }
  .admin-sidebar {
    padding: 14px 12px;
  }
  .summary-grid {
    grid-template-columns: 1fr;
  }
  .member-profile-shell .summary-grid {
    grid-template-columns: 1fr;
  }
  .member-profile-shell .summary-card:first-child {
    grid-column: auto;
  }
  .member-nav,
  .member-topbar .logout-form {
    width: 100%;
  }
  .member-nav a,
  .member-topbar .logout-form button {
    width: 100%;
    text-align: center;
  }
  .table-scroll {
    margin-inline: -4px;
    padding-inline: 4px;
  }
  .member-loan-requests-shell .panel {
    padding: 16px;
  }
}`

func (s *Server) staticCSS(c *gin.Context) {
	c.Data(http.StatusOK, "text/css; charset=utf-8", []byte(appCSS))
}
