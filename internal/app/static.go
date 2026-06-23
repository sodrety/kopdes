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
  --primary: #0056b3;
  --primary-strong: #004494;
  --primary-pale: #e8f1ff;
  --pinjaman: #0891b2;
  --pinjaman-pale: #cffafe;
  --warning: #ffc107;
  --warning-pale: #fff3cd;
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
  --public-dark: #343a40;
  --public-light: #f8f9fa;
  --radius-sm: 4px;
  --radius-md: 6px;
  --radius-lg: 8px;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  background: #f8f9fc;
  color: var(--ink);
  font-family: Inter, system-ui, -apple-system, sans-serif;
  font-size: 15px;
  line-height: 22px;
}
.public-page,
.auth-page {
  font-family: Poppins, Inter, system-ui, -apple-system, sans-serif;
}
.public-page {
  background: #fff;
  color: var(--public-dark);
  overflow-x: hidden;
}
.public-container {
  width: min(1120px, calc(100% - 32px));
  margin: 0 auto;
}
.public-header {
  position: sticky;
  top: 0;
  z-index: 20;
  background: #fff;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.1);
}
.public-nav {
  min-height: 88px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
}
.public-logo {
  display: inline-flex;
  align-items: center;
  gap: 15px;
  color: var(--public-dark);
  text-decoration: none;
  font-size: 19px;
  font-weight: 700;
  white-space: nowrap;
}
.public-logo img {
  width: 64px;
  height: 64px;
  object-fit: contain;
}
.public-links {
  display: flex;
  align-items: center;
  gap: 24px;
}
.public-links a {
  color: var(--public-dark);
  text-decoration: none;
  font-size: 14px;
  font-weight: 600;
  white-space: nowrap;
}
.public-links a:hover {
  color: var(--primary);
}
.public-links .public-login,
.public-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border-radius: 9999px;
  padding: 12px 30px;
  background: var(--primary);
  color: #fff;
  box-shadow: 0 5px 15px rgba(0, 86, 179, 0.3);
  text-decoration: none;
  font-size: 14px;
  font-weight: 700;
  transition: transform 180ms ease, box-shadow 180ms ease, background 180ms ease;
}
.public-links .public-login:hover,
.public-button:hover {
  transform: translateY(-3px);
  background: var(--primary-strong);
  color: #fff;
  box-shadow: 0 8px 20px rgba(0, 86, 179, 0.4);
}
.public-login svg,
.public-button svg {
  width: 17px;
  height: 17px;
}
.public-hero {
  position: relative;
  overflow: hidden;
  padding: 100px 0;
  background:
    radial-gradient(circle at 15% 15%, rgba(255, 193, 7, 0.10), transparent 28%),
    linear-gradient(135deg, rgba(0, 86, 179, 0.06), rgba(255, 255, 255, 0) 42%),
    #fff;
}
.public-hero-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(360px, 0.9fr);
  gap: 56px;
  align-items: center;
}
.public-eyebrow {
  margin: 0 0 16px;
  color: var(--primary);
  font-size: 15px;
  line-height: 22px;
  font-weight: 800;
  text-transform: uppercase;
}
.public-hero h1 {
  margin: 0 0 18px;
  color: var(--public-dark);
  font-size: 50px;
  line-height: 60px;
  font-weight: 800;
}
.public-hero h1 em {
  color: var(--primary);
  font-style: normal;
}
.public-hero h1 span {
  color: var(--warning);
}
.public-hero-copy > p:not(.public-eyebrow) {
  width: min(520px, 100%);
  margin: 0 0 24px;
  color: #4b5563;
  font-size: 17px;
  line-height: 28px;
}
.public-hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
}
.public-hero-visual,
.public-about-visual,
.public-business-image,
.public-gallery-image {
  display: grid;
  place-items: center;
  background:
    radial-gradient(circle at 30% 20%, rgba(255, 193, 7, 0.34), transparent 22%),
    linear-gradient(145deg, rgba(0, 86, 179, 0.10), rgba(0, 86, 179, 0.22));
}
.public-hero-visual {
  min-height: 360px;
  position: relative;
  border-radius: 15px;
  box-shadow: 0 16px 42px rgba(0, 0, 0, 0.13);
}
.public-hero-visual img,
.public-about-visual img {
  width: min(220px, 58%);
  height: auto;
  filter: drop-shadow(0 16px 22px rgba(0, 0, 0, 0.14));
}
.public-hero-visual div {
  position: absolute;
  right: 24px;
  bottom: 24px;
  display: grid;
  gap: 2px;
  border-radius: 15px;
  padding: 16px 18px;
  background: #fff;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.10);
}
.public-hero-visual strong {
  color: var(--primary);
  font-size: 28px;
  line-height: 32px;
}
.public-hero-visual span {
  color: var(--body);
  font-size: 13px;
  font-weight: 700;
}
.public-section {
  padding: 84px 0;
  background: #fff;
}
.public-about {
  background: var(--public-light);
}
.public-section-heading {
  width: min(650px, 100%);
  margin: 0 auto 50px;
  text-align: center;
}
.public-section-heading-left {
  margin-inline: 0;
  text-align: left;
}
.public-section-heading h2 {
  position: relative;
  margin: 0;
  padding-bottom: 20px;
  color: var(--primary);
  font-size: 34px;
  line-height: 42px;
  font-weight: 800;
}
.public-section-heading h2::after {
  content: "";
  position: absolute;
  left: 50%;
  bottom: 0;
  width: 80px;
  height: 3px;
  transform: translateX(-50%);
  background: var(--warning);
}
.public-section-heading-left h2::after {
  left: 0;
  transform: none;
}
.public-section-heading em {
  color: var(--public-dark);
  font-style: normal;
}
.public-section-heading p {
  margin: 18px 0 0;
  color: var(--body);
  font-size: 17px;
  line-height: 28px;
}
.public-card-grid {
  display: grid;
  gap: 24px;
}
.public-feature-grid {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}
.public-two-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.public-feature-card,
.public-business-card,
.public-gallery-card {
  border: 0;
  border-radius: 15px;
  background: #fff;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.05);
  transition: transform 180ms ease, box-shadow 180ms ease;
}
.public-feature-card:hover,
.public-business-card:hover,
.public-gallery-card:hover {
  transform: translateY(-8px);
  box-shadow: 0 15px 35px rgba(0, 0, 0, 0.10);
}
.public-feature-card {
  min-height: 220px;
  padding: 30px;
  text-align: center;
}
.public-feature-card > svg {
  width: 42px;
  height: 42px;
  margin-bottom: 20px;
  color: var(--primary);
  stroke-width: 2;
}
.public-feature-card h3,
.public-business-card h3,
.public-gallery-card h3 {
  margin: 0 0 10px;
  color: var(--public-dark);
  font-size: 20px;
  line-height: 26px;
}
.public-feature-card p,
.public-feature-card li,
.public-business-card p,
.public-gallery-card p,
.public-about p,
.public-check-list {
  color: var(--body);
  font-size: 15px;
  line-height: 26px;
}
.public-vision-card ul {
  margin: 0;
  padding-left: 20px;
  text-align: left;
}
.public-about-grid {
  display: grid;
  grid-template-columns: minmax(320px, 0.9fr) minmax(0, 1.1fr);
  gap: 56px;
  align-items: center;
}
.public-about-visual {
  min-height: 320px;
  border-radius: 15px;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.08);
}
.public-check-list {
  display: grid;
  gap: 14px;
  margin: 24px 0 0;
  padding: 0;
  list-style: none;
  font-weight: 700;
}
.public-check-list li {
  display: flex;
  align-items: center;
  gap: 12px;
}
.public-check-list svg {
  width: 20px;
  height: 20px;
  color: var(--primary);
  flex: 0 0 20px;
}
.public-business-card {
  overflow: hidden;
}
.public-business-image {
  height: 200px;
  color: var(--primary);
}
.public-business-image svg,
.public-gallery-image svg {
  width: 70px;
  height: 70px;
  stroke-width: 1.7;
}
.public-business-card h3,
.public-business-card p {
  margin-left: 22px;
  margin-right: 22px;
}
.public-business-card h3 {
  margin-top: 22px;
}
.public-business-card p {
  margin-bottom: 24px;
}
.public-gallery-card {
  width: min(360px, 100%);
  margin: 0 auto;
  overflow: hidden;
}
.public-gallery-image {
  height: 210px;
  color: #fff;
  background: linear-gradient(135deg, var(--primary), #2a7bd4);
}
.public-gallery-card div:last-child {
  padding: 18px;
}
.public-gallery-card p {
  margin: 0;
  text-transform: uppercase;
}
.public-cta {
  text-align: center;
}
.public-footer {
  padding: 60px 0 20px;
  background: var(--public-dark);
  color: #fff;
}
.public-footer-grid {
  display: grid;
  grid-template-columns: 1.4fr 0.8fr 0.8fr 1fr;
  gap: 34px;
}
.public-footer h2 {
  margin: 0 0 18px;
  color: #fff;
  font-size: 18px;
  line-height: 24px;
}
.public-footer p,
.public-footer a {
  display: block;
  margin: 0 0 10px;
  color: #fff;
  font-size: 14px;
  line-height: 22px;
  text-decoration: none;
}
.public-footer a:hover {
  color: var(--warning);
}
.public-footer-bottom {
  width: min(1120px, calc(100% - 32px));
  margin: 34px auto 0;
  padding-top: 20px;
  border-top: 1px solid rgba(255, 255, 255, 0.1);
  display: flex;
  justify-content: space-between;
  gap: 18px;
  flex-wrap: wrap;
}
.auth-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 32px;
  background: linear-gradient(180deg, #4e73df 10%, #224abe 100%);
}
.auth-card, .summary-card {
  background: var(--canvas);
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  padding: 18px;
  box-shadow: none;
}
.auth-card {
  width: min(100%, 960px);
  display: grid;
  grid-template-columns: minmax(320px, 1fr) minmax(360px, 1fr);
  overflow: hidden;
  border: 0;
  padding: 0;
  border-radius: 8px;
  box-shadow: 0 16px 36px rgba(0, 0, 0, 0.18);
}
.auth-visual {
  min-height: 520px;
  display: grid;
  place-items: center;
  align-content: center;
  gap: 22px;
  padding: 48px;
  background:
    linear-gradient(rgba(0, 86, 179, 0.54), rgba(0, 86, 179, 0.54)),
    radial-gradient(circle at 30% 20%, rgba(255, 193, 7, 0.42), transparent 22%),
    linear-gradient(135deg, #0056b3, #4e73df);
  color: #fff;
  text-align: center;
}
.auth-visual img {
  width: 150px;
  height: auto;
  filter: drop-shadow(0 14px 20px rgba(0, 0, 0, 0.20));
}
.auth-visual div {
  display: grid;
  gap: 8px;
}
.auth-visual span {
  font-size: 28px;
  line-height: 34px;
  font-weight: 800;
}
.auth-visual strong {
  font-size: 16px;
  line-height: 24px;
}
.auth-form-panel {
  padding: 48px;
  background: #fff;
}
.auth-form-panel .language-form {
  margin: 0 0 18px;
}
.auth-form-panel form {
  gap: 16px;
}
.auth-form-panel form > label {
  color: transparent;
  font-size: 0;
}
.auth-form-panel input {
  min-height: 50px;
  border-radius: 9999px;
  padding: 14px 18px;
  font-size: 13px;
}
.auth-form-panel button {
  min-height: 48px;
  width: 100%;
  border-radius: 9999px;
  text-transform: uppercase;
  background: var(--primary);
}
.auth-form-panel button:hover {
  background: var(--primary-strong);
}
.remember-field {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--body) !important;
  font-size: 13px !important;
  font-weight: 600;
}
.remember-field input {
  width: auto;
  min-height: 0;
}
.auth-form-panel hr {
  margin: 22px 0;
  border: 0;
  border-top: 1px solid var(--line-soft);
}
.auth-small-link,
.auth-copyright {
  display: block;
  color: var(--primary);
  text-align: center;
  font-size: 13px;
  line-height: 21px;
  text-decoration: none;
}
.auth-copyright {
  margin: 20px 0 0;
  color: var(--mute);
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
  box-shadow: 0 0 0 3px rgba(0, 86, 179, 0.14);
  outline: none;
}
button {
  border: 0;
  border-radius: 9999px;
  padding: 10px 18px;
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
  border-radius: 9999px;
  padding: 10px 18px;
  background: var(--primary);
  color: var(--canvas);
  box-shadow: 0 5px 15px rgba(0, 86, 179, 0.20);
  font-weight: 700;
  text-decoration: none;
}
.button-link-secondary {
  background: var(--canvas);
  border: 1px solid #d1d3e2;
  color: var(--primary);
  box-shadow: none;
}
button:disabled {
  opacity: 0.48;
  cursor: not-allowed;
}
.button-secondary {
  background: var(--canvas);
  border: 1px solid #d1d3e2;
  color: var(--primary);
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
  justify-content: center;
  margin-bottom: 18px;
  text-align: center;
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
.auth-card .brand-subtitle {
  color: var(--mute);
}
.admin-shell {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 224px minmax(0, 1fr);
  background: #f8f9fc;
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
  gap: 0;
  padding: 0 16px 16px;
  background: linear-gradient(180deg, #4e73df 10%, #224abe 100%);
  border-right: 0;
  color: #fff;
  overflow: hidden;
}
.sidebar-brand {
  min-width: 0;
  min-height: 70px;
  padding: 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.18);
  color: #fff;
  justify-content: flex-start;
}
.admin-sidebar .brand-logo {
  width: 28px;
  height: 28px;
  flex-basis: 28px;
}
.admin-sidebar .brand-name {
  font-size: 17px;
  line-height: 22px;
  letter-spacing: 0.04em;
}
.admin-sidebar .brand-name,
.admin-sidebar .brand-subtitle {
  color: #fff;
}
.admin-sidebar .brand-subtitle {
  display: none;
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
  gap: 0;
  padding-top: 10px;
}
.sidebar-nav-group {
  display: grid;
  gap: 6px;
  padding: 12px 0 14px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.18);
}
.sidebar-group-label {
  color: rgba(255, 255, 255, 0.48);
  font-size: 11px;
  line-height: 16px;
  font-weight: 800;
  letter-spacing: 0;
  text-transform: uppercase;
  padding: 0;
}
.sidebar-link {
  display: flex;
  align-items: center;
  gap: 10px;
  border-radius: 6px;
  padding: 9px 0;
  border-left: 0;
  color: rgba(255, 255, 255, 0.74);
  text-decoration: none;
  font-size: 14px;
  line-height: 20px;
  font-weight: 600;
  white-space: nowrap;
}
.sidebar-link:hover {
  background: rgba(255, 255, 255, 0.12);
  color: #fff;
}
.sidebar-icon {
  width: 16px;
  height: 16px;
  flex: 0 0 16px;
  stroke-width: 2.2;
  opacity: 0.58;
}
.sidebar-label {
  overflow: hidden;
  text-overflow: ellipsis;
}
.sidebar-link.active {
  background: transparent;
  color: #fff;
  box-shadow: none;
}
.sidebar-link.active .sidebar-icon {
  opacity: 1;
}
.sidebar-link.disabled {
  color: rgba(255, 255, 255, 0.38);
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
  min-height: 70px;
  margin: -24px -24px 28px;
  padding: 14px 24px;
  background: #fff;
  box-shadow: 0 2px 10px rgba(58, 59, 69, 0.12);
}
.sidebar-toggle {
  width: 38px;
  height: 38px;
  display: inline-grid;
  place-items: center;
  border-radius: 9999px;
  padding: 0;
  background: var(--canvas);
  border: 1px solid #e3e6f0;
  color: var(--primary);
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
  border-radius: 9999px;
  padding: 7px 10px;
  font-size: 14px;
  line-height: 20px;
}
.member-shell {
  width: min(1120px, 100%);
  margin: 0 auto;
  padding: 30px 24px 44px;
}
.member-topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin: 0 -24px 34px;
  padding: 16px 24px;
  flex-wrap: wrap;
  background: #fff;
  box-shadow: 0 2px 10px rgba(58, 59, 69, 0.12);
}
.member-nav {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}
.member-nav a {
  border-radius: 9999px;
  padding: 8px 14px;
  background: var(--canvas);
  border: 1px solid #d1d3e2;
  color: var(--primary);
  text-decoration: none;
  white-space: nowrap;
}
.page-shell {
  width: 100%;
  margin: 0;
  padding: 24px 24px 48px;
}
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 22px;
}
.page-header h1 {
  color: #5a5c69;
  font-size: 28px;
  line-height: 36px;
  font-weight: 700;
}
.page-header p {
  margin: 0;
  color: #858796;
  text-align: right;
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
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
  gap: 18px;
}
.chart-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 18px;
}
.dashboard-line-grid {
  display: grid;
  grid-template-columns: minmax(520px, 2.05fr) minmax(340px, 1fr);
  gap: 18px;
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
  border: 1px solid #e3e6f0;
  border-radius: 6px;
  background: #fff;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.10), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
}
.line-chart-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-height: 52px;
  padding: 0 22px;
  background: #fff;
  border-bottom: 1px solid #e3e6f0;
}
.line-chart-header h2 {
  margin: 0;
  color: var(--primary);
  font-size: 14px;
  line-height: 20px;
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
  position: relative;
  display: grid;
  gap: 8px;
  min-height: 96px;
  padding: 22px 72px 20px 20px;
  border: 1px solid #e3e6f0;
  border-left: 4px solid var(--primary);
  border-radius: 6px;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.10), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
}
.summary-card::after {
  content: "";
  position: absolute;
  right: 18px;
  top: 50%;
  width: 42px;
  height: 42px;
  border-radius: 9999px;
  transform: translateY(-50%);
  background: var(--primary);
  opacity: 0.92;
}
.summary-card span {
  color: var(--primary);
  font-size: 12px;
  line-height: 16px;
  font-weight: 700;
  text-transform: uppercase;
}
.summary-card strong {
	font-size: 30px;
	line-height: 36px;
	font-weight: 700;
  color: #5a5c69;
}
.summary-grid .summary-card:nth-child(2) {
  border-left-color: #1cc88a;
}
.summary-grid .summary-card:nth-child(2)::after {
  background: #1cc88a;
}
.summary-grid .summary-card:nth-child(2) span {
  color: #1cc88a;
}
.summary-grid .summary-card:nth-child(3) {
  border-left-color: #36b9cc;
}
.summary-grid .summary-card:nth-child(3)::after {
  background: #36b9cc;
}
.summary-grid .summary-card:nth-child(3) span {
  color: #36b9cc;
}
.summary-grid .summary-card:nth-child(4) {
  border-left-color: #f6c23e;
}
.summary-grid .summary-card:nth-child(4)::after {
  background: #f6c23e;
}
.summary-grid .summary-card:nth-child(4) span {
  color: #f6c23e;
}
.summary-grid .summary-card:nth-child(5) {
  border-left-color: #4e73df;
}
.summary-grid .summary-card:nth-child(5)::after {
  background: #4e73df;
}
.summary-grid .summary-card:nth-child(6) {
  border-left-color: #858796;
}
.summary-grid .summary-card:nth-child(6)::after {
  background: #858796;
}
.dashboard-home-grid {
  display: grid;
  grid-template-columns: minmax(520px, 1.55fr) minmax(280px, 0.75fr);
  gap: 18px;
  align-items: start;
}
.panel-header {
  min-height: 52px;
  display: flex;
  align-items: center;
  padding: 0 20px;
  background: #fff;
  border-bottom: 1px solid #e3e6f0;
}
.panel-header h2 {
  margin: 0;
}
.dashboard-activity-card .table-scroll {
  margin: 0;
}
.dashboard-activity-card td:nth-child(4),
.dashboard-activity-card th:nth-child(4) {
  text-align: right;
}
.dashboard-quick-list {
  display: grid;
  gap: 10px;
  margin: 20px;
}
.dashboard-quick-list a {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 46px;
  padding: 0 14px;
  border: 1px solid #e3e6f0;
  border-radius: 6px;
  background: #f8f9fc;
  color: #5a5c69;
  text-decoration: none;
  font-size: 14px;
  font-weight: 700;
}
.dashboard-quick-list a:hover {
  border-color: #c7d4f6;
  background: var(--primary-pale);
  color: var(--primary);
}
.dashboard-quick-list svg {
  width: 18px;
  height: 18px;
  color: var(--primary);
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
  border: 1px solid #e3e6f0;
  border-radius: 6px;
  padding: 0;
  overflow: hidden;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.10), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
}
.narrow-panel {
  max-width: 560px;
}
h2 {
  margin: 0;
  color: var(--primary);
  font-size: 14px;
  line-height: 20px;
  font-weight: 700;
}
.panel > h2 {
  min-height: 52px;
  display: flex;
  align-items: center;
  padding: 0 20px;
  border-bottom: 1px solid #e3e6f0;
}
.panel > :not(h2) {
  margin: 20px;
}
.panel > .table-scroll {
  margin: 0;
}
.panel > h2 + .table-scroll {
  margin-top: 0;
}
.panel > h2 + form,
.panel > h2 + .filter-form {
  margin-top: 20px;
}
.panel > .empty-state {
  margin: 20px;
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
  padding: 12px 20px;
  border-bottom: 1px solid var(--line-soft);
  text-align: left;
  vertical-align: top;
}
th {
  background: #f8f9fc;
  color: #6e707e;
  font-size: 12px;
  line-height: 16px;
  font-weight: 800;
  text-transform: uppercase;
}
tbody tr:hover {
  background: #f8f9fc;
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
.balance-statement td:last-child {
  text-align: right;
  font-weight: 700;
}
.balance-positive {
  color: var(--primary-strong);
}
.balance-negative {
  color: var(--pinjaman);
}
.balance-warning {
  color: #92400e;
}
.balance-total-row td {
  border-top: 2px solid var(--line);
  border-bottom: 0;
  background: #f9fafb;
  font-weight: 800;
}
.balance-report-card {
  margin-top: 4px;
  overflow: hidden;
  border: 1px solid #e3e6f0;
  border-radius: 6px;
  background: #fff;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.10), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
}
.balance-report-card-header {
  min-height: 60px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 0 20px;
  background: #fff;
  border-bottom: 1px solid #e3e6f0;
}
.balance-report-card-header h2 {
  color: var(--primary);
  font-size: 14px;
  line-height: 20px;
}
.balance-export-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.balance-export-actions .button-link-secondary {
  border-color: #d1d3e2;
  background: #fff;
  color: var(--primary);
  box-shadow: none;
}
.balance-report-card-body {
  display: grid;
  gap: 24px;
  padding: 20px;
}
.balance-filter-card {
  border-left: 4px solid #36b9cc;
  border-radius: 6px;
  padding: 20px;
  background: #fff;
  box-shadow: inset 0 0 0 1px #e3e6f0;
}
.balance-filter-card h3,
.balance-detail-card h3,
.balance-meta-card h3 {
  margin: 0 0 16px;
  color: #36b9cc;
  font-size: 14px;
  line-height: 20px;
  font-weight: 700;
}
.balance-filter-form {
  display: grid;
  grid-template-columns: repeat(3, minmax(160px, 1fr)) auto auto;
  align-items: end;
  gap: 18px;
}
.balance-filter-form input,
.balance-filter-form select {
  border-color: #d1d3e2;
  border-radius: 4px;
  min-height: 40px;
}
.balance-filter-form button,
.balance-filter-form .button-link {
  min-height: 40px;
  border-radius: 4px;
  padding: 8px 16px;
}
.balance-health-alert {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  border-radius: 4px;
  padding: 16px 20px;
  background: #fff3cd;
  color: #856404;
}
.balance-health-alert span {
  display: block;
  margin-bottom: 6px;
  font-weight: 700;
}
.balance-health-alert p {
  margin: 0;
}
.balance-health-alert svg {
  width: 32px;
  height: 32px;
  color: #f6c23e;
}
.balance-kpi-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 24px;
}
.balance-kpi-card,
.balance-meta-card {
  position: relative;
  min-height: 124px;
  display: grid;
  align-content: center;
  gap: 8px;
  border-radius: 6px;
  border: 1px solid #e3e6f0;
  border-left: 4px solid #4e73df;
  padding: 20px 72px 20px 20px;
  background: #fff;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.10), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
}
.balance-kpi-card::after {
  content: "";
  position: absolute;
  right: 20px;
  top: 50%;
  width: 42px;
  height: 42px;
  border-radius: 9999px;
  transform: translateY(-50%);
  background: #4e73df;
}
.balance-kpi-card span {
  color: #4e73df;
  font-size: 12px;
  line-height: 16px;
  font-weight: 800;
  text-transform: uppercase;
}
.balance-kpi-card strong {
  color: #2d3748;
  font-size: 20px;
  line-height: 26px;
}
.balance-kpi-card small,
.balance-meta-card p {
  color: #718096;
  font-size: 13px;
  line-height: 20px;
}
.balance-kpi-warning {
  border-left-color: #f6c23e;
}
.balance-kpi-warning::after {
  background: #f6c23e;
}
.balance-kpi-warning span {
  color: #f6c23e;
}
.balance-kpi-success {
  border-left-color: #1cc88a;
}
.balance-kpi-success::after {
  background: #1cc88a;
}
.balance-kpi-success span {
  color: #1cc88a;
}
.balance-detail-card {
  overflow: hidden;
  border: 1px solid #e3e6f0;
  border-radius: 6px;
  background: #fff;
}
.balance-detail-card h3 {
  min-height: 52px;
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
  padding: 0 20px;
  border-bottom: 1px solid #e3e6f0;
  color: #4e73df;
}
.balance-detail-card .table-scroll {
  margin: 20px;
}
.balance-detail-card th {
  background: #f8f9fc;
  color: #5a5c69;
}
.balance-detail-card td:last-child {
  text-align: right;
}
.balance-section-row td {
  background: #d9d9d9;
  color: #4e73df;
  font-weight: 800;
}
.balance-row-icon {
  display: inline-block;
  width: 12px;
  height: 12px;
  margin-right: 10px;
  border-radius: 3px;
  vertical-align: middle;
}
.balance-positive-dot {
  background: #1cc88a;
}
.balance-negative-dot {
  background: #36b9cc;
}
.balance-warning-dot {
  background: #f6c23e;
}
.balance-meta-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 24px;
}
.balance-meta-card {
  min-height: 160px;
  align-content: start;
  padding-right: 20px;
}
.balance-meta-info {
  border-left-color: #36b9cc;
}
.balance-meta-secondary {
  border-left-color: #858796;
}
.profit-period-badge {
  justify-self: end;
  display: inline-flex;
  align-items: center;
  border-radius: 4px;
  margin: -4px 0 8px auto;
  padding: 9px 14px;
  background: #4e73df;
  color: #fff;
  font-size: 12px;
  line-height: 16px;
  font-weight: 800;
}
.profit-period-badge svg {
  width: 15px;
  height: 15px;
}
.profit-kpi-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 24px;
  margin-bottom: 30px;
}
.profit-kpi-card {
  position: relative;
  min-height: 128px;
  display: grid;
  align-content: center;
  gap: 8px;
  border-radius: 6px;
  border: 1px solid #e3e6f0;
  border-left: 4px solid #1cc88a;
  padding: 22px 72px 20px 20px;
  background: #fff;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.10), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
}
.profit-kpi-card::after {
  content: "";
  position: absolute;
  right: 20px;
  top: 50%;
  width: 42px;
  height: 42px;
  border-radius: 9999px;
  transform: translateY(-50%);
  background: #1cc88a;
}
.profit-kpi-card span {
  color: #1cc88a;
  font-size: 12px;
  line-height: 16px;
  font-weight: 800;
  text-transform: uppercase;
}
.profit-kpi-card strong {
  color: #2d3748;
  font-size: 22px;
  line-height: 28px;
}
.profit-kpi-card small {
  width: max-content;
  border-radius: 9999px;
  padding: 3px 8px;
  background: #1cc88a;
  color: #fff;
  font-size: 12px;
  font-weight: 800;
}
.profit-kpi-cost {
  border-left-color: #e74a3b;
}
.profit-kpi-cost::after,
.profit-kpi-cost small {
  background: #e74a3b;
}
.profit-kpi-cost span {
  color: #e74a3b;
}
.profit-kpi-net {
  border-left-color: #4e73df;
}
.profit-kpi-net::after,
.profit-kpi-net small {
  background: #4e73df;
}
.profit-kpi-net span {
  color: #4e73df;
}
.profit-card,
.profit-tabs-card,
.profit-insights-card {
  overflow: hidden;
  border: 1px solid #e3e6f0;
  border-radius: 6px;
  margin-top: 24px;
  background: #fff;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.10), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
}
.profit-card-header,
.profit-insights-header {
  min-height: 52px;
  display: flex;
  align-items: center;
  padding: 0 20px;
  background: #fff;
  border-bottom: 1px solid #e3e6f0;
}
.profit-card-header h2,
.profit-insights-header h2 {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--primary);
  font-size: 14px;
  line-height: 20px;
}
.profit-card-header svg,
.profit-insights-header svg {
  width: 16px;
  height: 16px;
}
.profit-card-body {
  padding: 20px;
}
.profit-filter-form {
  display: grid;
  grid-template-columns: repeat(3, minmax(170px, 1fr));
  gap: 18px;
}
.profit-filter-form input,
.profit-filter-form select {
  border-color: #d1d3e2;
  border-radius: 4px;
  min-height: 42px;
}
.profit-action-row {
  grid-column: 1 / -1;
  display: flex;
  flex-wrap: wrap;
  gap: 0;
}
.profit-action-row button,
.profit-action-row .button-link {
  min-height: 34px;
  border-radius: 3px;
  padding: 7px 13px;
  box-shadow: none;
  font-size: 13px;
}
.profit-action-row .button-link-secondary {
  background: #858796;
  border-color: #858796;
  color: #fff;
}
.profit-export-button {
  background: #1cc88a;
}
.profit-print-button {
  background: #36b9cc;
}
.profit-tabs-header {
  display: flex;
  align-items: center;
  gap: 0;
  min-height: 50px;
  padding: 0 12px;
  background: #fff;
  border-bottom: 1px solid #e3e6f0;
}
.profit-tabs-header span {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border-radius: 4px 4px 0 0;
  padding: 12px 18px;
  color: #858796;
  font-weight: 800;
}
.profit-tabs-header span.active {
  background: var(--primary-pale);
  color: var(--primary);
}
.profit-tabs-header svg {
  width: 16px;
  height: 16px;
}
.profit-tabs-body {
  min-height: 320px;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 32px;
  padding: 28px 24px;
}
.profit-detail-column h2 {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #1cc88a;
  font-size: 18px;
  line-height: 24px;
  margin-bottom: 10px;
}
.profit-detail-column:nth-child(2) h2 {
  color: #e74a3b;
}
.profit-detail-column p,
.profit-empty {
  color: #718096;
}
.profit-empty {
  min-height: 180px;
  display: grid;
  place-items: center;
  text-align: center;
}
.profit-total-line {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  border-top: 1px solid #e3e6f0;
  padding-top: 16px;
}
.profit-total-line strong {
  color: #2d3748;
}
.profit-insights-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 24px;
  padding: 20px;
}
.profit-insight-box {
  border-left: 4px solid #36b9cc;
  border-radius: 6px;
  padding: 20px;
  box-shadow: inset 0 0 0 1px #e3e6f0;
}
.profit-insight-box h3 {
  margin: 0 0 12px;
  color: #2d3748;
  font-size: 16px;
}
.profit-insight-box p {
  margin: 0 0 8px;
  color: #4a5568;
}
.profit-insight-primary {
  border-left-color: #4e73df;
}
.profit-insight-primary strong {
  color: #2d3748;
  font-size: 22px;
}
@media (max-width: 1120px) {
  .dashboard-line-grid,
  .dashboard-home-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 760px) {
  .public-nav {
    min-height: auto;
    padding: 14px 0;
    align-items: flex-start;
    flex-direction: column;
  }
  .public-links {
    width: 100%;
    gap: 10px;
    overflow-x: auto;
    padding-bottom: 6px;
  }
  .public-links a {
    flex: 0 0 auto;
  }
  .public-hero {
    padding: 64px 0;
  }
  .public-hero-grid,
  .public-about-grid,
  .public-feature-grid,
  .public-two-grid,
  .public-footer-grid {
    grid-template-columns: 1fr;
  }
  .public-hero h1 {
    font-size: 38px;
    line-height: 46px;
  }
  .public-hero-visual,
  .public-about-visual {
    min-height: 260px;
  }
  .public-section {
    padding: 58px 0;
  }
  .public-section-heading h2 {
    font-size: 29px;
    line-height: 36px;
  }
  .public-footer-bottom {
    display: grid;
  }
  .auth-page {
    padding: 18px;
  }
  .auth-card {
    grid-template-columns: 1fr;
  }
  .auth-visual {
    display: none;
  }
  .auth-form-panel {
    padding: 34px 24px;
  }
  .balance-report-card-header {
    align-items: flex-start;
    flex-direction: column;
    padding: 16px;
  }
  .balance-report-card-body {
    padding: 16px;
  }
  .balance-filter-form,
  .balance-kpi-grid,
  .balance-meta-grid,
  .profit-kpi-grid,
  .profit-filter-form,
  .profit-tabs-body,
  .profit-insights-grid {
    grid-template-columns: 1fr;
  }
  .balance-filter-form button,
  .balance-filter-form .button-link,
  .profit-action-row button,
  .profit-action-row .button-link {
    width: 100%;
  }
  .profit-period-badge {
    justify-self: stretch;
    width: 100%;
    justify-content: center;
  }
  .profit-tabs-header {
    align-items: stretch;
    flex-direction: column;
    padding: 8px;
  }
  .profit-tabs-header span {
    border-radius: 4px;
  }
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
