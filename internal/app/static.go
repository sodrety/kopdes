package app

import (
	"embed"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed static/vendor/*.js static/images/*.png
var staticAssetFS embed.FS

const appCSS = `:root {
  --primary: #16a34a;
  --primary-strong: #15803d;
  --primary-pale: #dcfce7;
  --pinjaman: #0891b2;
  --pinjaman-pale: #cffafe;
  --warning: #f59e0b;
  --warning-pale: #fef3c7;
  --negative: #dc2626;
  --negative-pale: #fee2e2;
  --ink: #111827;
  --body: #374151;
  --mute: #6b7280;
  --line: #d1d5db;
  --line-soft: #e5e7eb;
  --canvas: #ffffff;
  --canvas-soft: #f3f4f6;
  --sidebar: #ffffff;
  --radius-sm: 4px;
  --radius-md: 6px;
  --radius-lg: 8px;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  background: var(--canvas-soft);
  color: var(--ink);
  font-family: Inter, system-ui, -apple-system, sans-serif;
  font-size: 15px;
  line-height: 22px;
}
.auth-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 32px;
}
.auth-card, .summary-card {
  background: var(--canvas);
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  padding: 18px;
  box-shadow: none;
}
.auth-card {
  width: min(100%, 420px);
}
h1 {
  margin: 0 0 10px;
  font-size: 30px;
  line-height: 36px;
  font-weight: 700;
}
form {
  display: grid;
  gap: 12px;
}
label {
  display: grid;
  gap: 6px;
  color: var(--body);
  font-size: 13px;
  font-weight: 600;
}
input, select {
	width: 100%;
	border: 1px solid var(--line);
	border-radius: var(--radius-md);
	padding: 9px 11px;
	color: var(--ink);
	font: inherit;
	background: var(--canvas);
}
input:focus, select:focus {
  border-color: var(--primary);
  box-shadow: 0 0 0 3px rgba(22, 163, 74, 0.14);
  outline: none;
}
button {
  border: 0;
  border-radius: var(--radius-md);
  padding: 9px 14px;
  background: var(--primary);
  color: var(--canvas);
  font: inherit;
  font-weight: 600;
  cursor: pointer;
}
.button-link {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-md);
  padding: 9px 14px;
  background: var(--primary);
  color: var(--canvas);
  font-weight: 700;
  text-decoration: none;
}
.button-link-secondary {
  background: var(--canvas);
  border: 1px solid var(--line);
  color: var(--ink);
}
button:disabled {
  opacity: 0.48;
  cursor: not-allowed;
}
.button-secondary {
  background: var(--canvas);
  border: 1px solid var(--line);
  color: var(--ink);
}
.form-error {
  min-height: 20px;
  color: var(--negative);
}
.form-section {
  display: grid;
  gap: 12px;
  border-top: 1px solid var(--line);
  padding-top: 14px;
}
.form-section h3 {
  margin: 0;
  font-size: 16px;
  line-height: 24px;
}
.filter-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(140px, 1fr));
  gap: 10px;
  margin-bottom: 14px;
}
.filter-form button {
  align-self: end;
}
.nav-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 24px;
  background: var(--canvas);
}
.brand-mark {
  display: inline-flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  color: var(--ink);
}
.brand-logo {
  width: 48px;
  height: 48px;
  flex: 0 0 48px;
  object-fit: contain;
}
.brand-text {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.brand-name {
  font-size: 18px;
  line-height: 22px;
  font-weight: 800;
  overflow-wrap: anywhere;
}
.brand-subtitle {
  color: var(--body);
  font-size: 12px;
  line-height: 16px;
  font-weight: 700;
  text-transform: uppercase;
  white-space: nowrap;
}
.auth-card .brand-mark {
  margin-bottom: 24px;
}
.auth-card .brand-logo {
  width: 72px;
  height: 72px;
  flex-basis: 72px;
}
.auth-card .brand-name {
  font-size: 24px;
  line-height: 30px;
}
.admin-shell {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 248px minmax(0, 1fr);
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
  gap: 24px;
  padding: 18px;
  background: var(--sidebar);
  border-right: 1px solid var(--line);
  color: var(--ink);
  overflow: hidden;
}
.sidebar-brand {
  min-width: 0;
}
.sidebar-collapsed .sidebar-brand {
  justify-content: center;
}
.sidebar-collapsed .brand-logo {
  width: 42px;
  height: 42px;
  flex-basis: 42px;
}
.sidebar-collapsed .brand-text {
  display: none;
}
.sidebar-nav {
  display: grid;
  gap: 16px;
}
.sidebar-nav-group {
  display: grid;
  gap: 8px;
}
.sidebar-group-label {
  color: var(--mute);
  font-size: 11px;
  line-height: 16px;
  font-weight: 800;
  letter-spacing: 0;
  text-transform: uppercase;
  padding: 0 10px;
}
.sidebar-link {
  display: flex;
  align-items: center;
  gap: 10px;
  border-radius: var(--radius-md);
  padding: 9px 10px;
  border-left: 3px solid transparent;
  color: var(--body);
  text-decoration: none;
  font-size: 14px;
  line-height: 20px;
  font-weight: 600;
  white-space: nowrap;
}
.sidebar-link:hover {
  background: var(--canvas-soft);
  color: var(--primary-strong);
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
  background: var(--primary-pale);
  border-left-color: var(--primary);
  color: var(--primary-strong);
}
.sidebar-link.disabled {
  color: var(--mute);
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
.sidebar-collapsed .sidebar-nav {
  gap: 12px;
}
.sidebar-collapsed .sidebar-nav-group {
  gap: 6px;
}
.sidebar-collapsed .sidebar-group-label {
  display: none;
}
.admin-topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 24px;
}
.sidebar-toggle {
  width: 38px;
  height: 38px;
  display: inline-grid;
  place-items: center;
  border-radius: var(--radius-md);
  padding: 0;
  background: var(--canvas);
  border: 1px solid var(--line);
  color: var(--ink);
}
.logout-form {
  display: block;
}
.language-form {
  display: block;
}
.language-form label {
  display: flex;
  align-items: center;
  gap: 8px;
}
.language-form select {
  width: auto;
  min-width: 150px;
  border-radius: var(--radius-md);
  padding: 7px 10px;
  font-size: 14px;
  line-height: 20px;
}
.member-shell {
  width: min(960px, 100%);
  margin: 0 auto;
  padding: 30px 24px 44px;
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
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}
.member-nav a {
  border-radius: var(--radius-md);
  padding: 7px 10px;
  background: var(--canvas);
  border: 1px solid var(--line);
  text-decoration: none;
  white-space: nowrap;
}
.page-shell {
  width: min(1120px, 100%);
  margin: 0 auto;
  padding: 32px 24px;
}
.page-header {
  margin-bottom: 18px;
}
.page-header p {
  margin: 0;
  color: var(--body);
}
.page-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.page-shell > section + section,
.member-shell > section + section {
  margin-top: 16px;
}
.summary-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}
.chart-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 12px;
}
.dashboard-line-grid {
  display: grid;
  grid-template-columns: minmax(520px, 2.05fr) minmax(340px, 1fr);
  gap: 16px;
  align-items: stretch;
}
.chart-panel {
  display: grid;
  gap: 12px;
}
.chart-panel h2 {
  margin: 0;
}
.bar-chart {
  display: grid;
  gap: 10px;
}
.bar-row {
  display: grid;
  gap: 8px;
}
.bar-label {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  color: var(--body);
  font-size: 14px;
  font-weight: 700;
}
.bar-label strong {
  color: var(--ink);
}
.bar-track {
  height: 8px;
  overflow: hidden;
  border-radius: var(--radius-sm);
  background: var(--canvas-soft);
  border: 1px solid var(--line);
}
.bar-fill {
  display: block;
  height: 100%;
  min-width: 0;
  border-radius: inherit;
}
.line-chart-card {
  display: grid;
  grid-template-rows: auto minmax(260px, 1fr) auto;
  overflow: hidden;
  min-width: 0;
  border: 1px solid #dce5ef;
  border-radius: var(--radius-lg);
  background: #fff;
  box-shadow: 0 2px 6px rgba(15, 23, 42, 0.12);
}
.line-chart-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-height: 56px;
  padding: 0 28px;
  border-bottom: 1px solid #e3ebf4;
}
.line-chart-header h2 {
  margin: 0;
  color: #4f73f1;
  font-size: 18px;
  line-height: 24px;
  font-weight: 800;
}
.line-chart-header span {
  color: #bcc9d8;
  font-size: 24px;
  line-height: 1;
}
.line-chart-body {
  display: flex;
  align-items: center;
  min-width: 0;
  padding: 28px 30px 0;
}
.line-chart-svg {
  display: block;
  width: 100%;
  min-height: 260px;
}
.line-chart-gridline {
  stroke: #e7ecf2;
  stroke-width: 1.2;
}
.line-chart-axis {
  stroke: #d9e0e8;
  stroke-width: 1.2;
}
.line-chart-y-label,
.line-chart-x-label {
  fill: #6f747b;
  font-size: 15px;
  font-weight: 700;
}
.line-chart-y-label {
  text-anchor: end;
  dominant-baseline: middle;
}
.line-chart-x-label {
  text-anchor: middle;
}
.line-chart-path {
  fill: none;
  stroke-width: 4;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.line-chart-point {
  stroke: #fff;
  stroke-width: 2;
}
.line-chart-legend {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 28px;
  min-height: 60px;
  padding: 0 24px 18px;
  color: #4f5d70;
  font-size: 15px;
  font-weight: 700;
}
.line-chart-legend span {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.line-chart-legend i {
  width: 16px;
  height: 16px;
  border-radius: 9999px;
}
.chart-simpanan {
  background: var(--primary);
}
.chart-pinjaman {
  background: var(--pinjaman);
}
.chart-warning {
  background: var(--warning);
}
.chart-danger {
  background: var(--negative);
}
.chart-line-simpanan {
  stroke: #5b7cf0;
  background: #5b7cf0;
  fill: #5b7cf0;
}
.chart-line-pinjaman {
  stroke: #24c68a;
  background: #24c68a;
  fill: #24c68a;
}
.chart-line-neraca {
  stroke: #35bfd1;
  background: #35bfd1;
  fill: #35bfd1;
}
.summary-card {
  display: grid;
  gap: 10px;
  border-top: 3px solid var(--primary);
}
.summary-card span {
  color: var(--body);
  font-size: 12px;
  line-height: 16px;
  font-weight: 700;
  text-transform: uppercase;
}
.summary-card strong {
	font-size: 26px;
	line-height: 32px;
	font-weight: 700;
}
a {
  color: var(--ink);
  font-weight: 600;
}
.two-column {
  display: grid;
  grid-template-columns: minmax(280px, 0.8fr) minmax(320px, 1.2fr);
  gap: 16px;
  align-items: start;
}
.panel {
  background: var(--canvas);
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  padding: 18px;
  box-shadow: none;
}
.narrow-panel {
  max-width: 560px;
}
h2 {
  margin: 0 0 12px;
  font-size: 19px;
  line-height: 25px;
  font-weight: 700;
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
  padding: 9px 10px;
  border-bottom: 1px solid var(--line-soft);
  text-align: left;
  vertical-align: top;
}
th {
  background: #f9fafb;
  color: var(--body);
  font-size: 12px;
  line-height: 16px;
  font-weight: 800;
  text-transform: uppercase;
}
tbody tr:hover {
  background: #f9fafb;
}
td:nth-child(n+2):not(.review-cell) {
  font-variant-numeric: tabular-nums;
}
.table-muted {
  color: var(--mute);
  font-size: 13px;
}
.table-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.table-actions button {
	padding: 7px 10px;
	font-size: 13px;
}
.review-cell {
	min-width: 300px;
}
.review-action label {
	gap: 4px;
	font-size: 12px;
	line-height: 16px;
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
	border-radius: var(--radius-sm);
	padding: 7px 9px;
	font-size: 13px;
}
.inline-rejection-form {
	display: grid;
	grid-template-columns: minmax(160px, 1fr) auto;
	gap: 8px;
	min-width: 240px;
	margin-top: 8px;
}
.inline-rejection-form input {
	border-radius: var(--radius-sm);
	padding: 7px 9px;
	font-size: 13px;
}
.inline-rejection-form button {
	padding: 7px 10px;
	font-size: 13px;
}
.inline-repayment-form {
	display: grid;
	grid-template-columns: repeat(2, minmax(112px, 1fr));
	gap: 8px;
	min-width: 260px;
}
.inline-repayment-form input {
	border-radius: var(--radius-sm);
	padding: 7px 9px;
	font-size: 13px;
}
.inline-repayment-form button {
	grid-column: 1 / -1;
	padding: 7px 10px;
	font-size: 13px;
}
.status-badge {
	display: inline-flex;
  border-radius: 9999px;
  padding: 3px 9px;
  background: var(--primary-pale);
  color: var(--primary-strong);
  font-size: 12px;
  line-height: 16px;
  font-weight: 700;
}
.status-pending {
  background: var(--warning-pale);
  color: #92400e;
}
.status-approved,
.status-active {
  background: var(--primary-pale);
  color: var(--primary-strong);
}
.status-rejected,
.status-inactive,
.status-suspended {
  background: var(--negative-pale);
  color: var(--negative);
}
.empty-state {
  color: var(--mute);
}
.detail-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 16px;
}
.detail-grid div {
  display: grid;
  gap: 8px;
}
.detail-grid span {
  color: var(--body);
  font-size: 13px;
  font-weight: 600;
}
@media (max-width: 1120px) {
  .dashboard-line-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 760px) {
	  .admin-shell {
	    display: block;
	    min-height: 0;
	    min-width: 0;
	    overflow-x: hidden;
	  }
	  .sidebar-collapsed .admin-shell {
	    display: block;
	  }
	  .admin-sidebar {
	    position: static;
	    align-self: start;
	    height: auto;
    gap: 12px;
    padding: 16px;
    max-width: 100vw;
    overflow: hidden;
  }
  .admin-sidebar .sidebar-brand {
    width: auto;
  }
  .sidebar-collapsed .sidebar-brand {
    justify-content: flex-start;
  }
  .sidebar-collapsed .brand-text {
    display: grid;
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
  .sidebar-nav-group {
    display: flex;
    gap: 8px;
  }
  .sidebar-group-label {
    display: none;
  }
	  .sidebar-link {
	    flex: 0 0 auto;
	    gap: 8px;
	    padding: 8px 10px;
	    border-radius: var(--radius-md);
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
  .line-chart-card {
    grid-template-rows: auto auto auto;
  }
  .line-chart-header {
    min-height: 52px;
    padding: 0 16px;
  }
  .line-chart-header h2 {
    font-size: 15px;
    line-height: 20px;
  }
  .line-chart-body {
    padding: 18px 12px 0;
  }
  .line-chart-svg {
    min-height: 220px;
  }
  .line-chart-y-label,
  .line-chart-x-label {
    font-size: 13px;
  }
  .line-chart-legend {
    min-height: 48px;
    padding: 0 16px 14px;
    font-size: 14px;
  }
  .two-column {
    grid-template-columns: 1fr;
  }
	  .page-shell .panel,
	  .page-shell .summary-card {
	    border-radius: var(--radius-lg);
	    padding: 14px;
	  }
  .inline-approval-form,
  .inline-rejection-form,
  .inline-repayment-form,
  .filter-form {
    min-width: 0;
  }
  .review-cell {
    min-width: 220px;
  }
  .inline-approval-form,
  .inline-repayment-form {
    grid-template-columns: 1fr;
  }
  .inline-rejection-form,
  .filter-form {
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
  .member-topbar .language-form {
    width: 100%;
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
	    border-radius: var(--radius-lg);
	    padding: 14px;
	  }
  .member-profile-shell .summary-card {
    gap: 8px;
  }
  .member-profile-shell .summary-card:first-child {
    grid-column: 1 / -1;
  }
	  .summary-card strong {
	    font-size: 24px;
	    line-height: 30px;
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
  .language-form,
  .member-topbar .logout-form {
    width: 100%;
  }
  .member-nav a,
  .language-form select,
  .member-topbar .logout-form button {
    width: 100%;
    text-align: center;
  }
  .brand-logo {
    width: 42px;
    height: 42px;
    flex-basis: 42px;
  }
  .brand-name {
    font-size: 20px;
    line-height: 24px;
  }
  .brand-subtitle {
    white-space: normal;
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

func (s *Server) staticVendorAsset(c *gin.Context) {
	name := strings.TrimPrefix(c.Param("file"), "/")
	if name == "" || strings.Contains(name, "..") || !strings.HasSuffix(name, ".js") {
		c.Status(http.StatusNotFound)
		return
	}
	c.FileFromFS("static/vendor/"+name, http.FS(staticAssetFS))
}

func (s *Server) staticImageAsset(c *gin.Context) {
	name := strings.TrimPrefix(c.Param("file"), "/")
	if name == "" || strings.Contains(name, "..") || !strings.HasSuffix(name, ".png") {
		c.Status(http.StatusNotFound)
		return
	}
	c.FileFromFS("static/images/"+name, http.FS(staticAssetFS))
}
