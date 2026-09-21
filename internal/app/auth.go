package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID                 string
	Email              string
	Role               string
	MemberID           sql.NullString
	MemberNo           string
	MemberStatus       string
	FullName           string
	Active             bool
	MustChangePassword bool
}

// EnsureAdminUser is retained for compatibility with existing deployments and
// test fixtures. Legacy Admin users are Managers in the Officer model.
func EnsureAdminUser(db *sql.DB, email, password string) error {
	if email == "" || password == "" {
		return nil
	}

	var existingID string
	err := db.QueryRow(`SELECT id FROM users WHERE email = $1`, email).Scan(&existingID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	memberID := newID()
	userID := newID()
	memberNo := "BOOTSTRAP-" + strings.ToUpper(memberID[:8])
	if _, err := tx.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ($1,$2,$3,$4,'active')`, memberID, memberNo, email, time.Now().Format("2006-01-02")); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active,must_change_password,historical_identity) VALUES ($1,$2,$3,'member',$4,$5,TRUE,FALSE,FALSE)`, userID, email, string(hash), memberID, email); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO officer_appointments (id,member_id,role,active) VALUES ($1,$2,'manager',TRUE)`, newID(), memberID); err != nil {
		return err
	}
	return tx.Commit()
}

// EnsureSuperAdminUser bootstraps the single memberless platform identity.
// Existing active credentials are never overwritten on application restart.
func EnsureSuperAdminUser(db *sql.DB, email, password, fullName string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	password = strings.TrimSpace(password)
	fullName = strings.TrimSpace(fullName)

	var activeCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE role='super_admin' AND active=TRUE AND historical_identity=FALSE`).Scan(&activeCount); err != nil {
		return err
	}
	if activeCount > 0 {
		return nil
	}
	if email == "" || len(password) < 8 {
		return errors.New("SUPER_ADMIN_EMAIL and SUPER_ADMIN_PASSWORD are required when no active super admin exists")
	}
	if fullName == "" {
		fullName = email
	}

	var existing struct {
		ID       string
		Role     string
		MemberID sql.NullString
		Active   bool
	}
	err := db.QueryRow(`SELECT id,role,member_id,active FROM users WHERE LOWER(email)=LOWER($1)`, email).Scan(&existing.ID, &existing.Role, &existing.MemberID, &existing.Active)
	if err == nil {
		if existing.Role != "super_admin" || existing.MemberID.Valid {
			return fmt.Errorf("SUPER_ADMIN_EMAIL already belongs to a non-super-admin user")
		}
		if existing.Active {
			return fmt.Errorf("SUPER_ADMIN_EMAIL belongs to an active user")
		}
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return hashErr
		}
		_, err = db.Exec(`UPDATE users SET password_hash=$1,full_name=$2,active=TRUE,must_change_password=TRUE,historical_identity=FALSE,updated_at=CURRENT_TIMESTAMP WHERE id=$3`, string(hash), fullName, existing.ID)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active,must_change_password,historical_identity) VALUES ($1,$2,$3,'super_admin',NULL,$4,TRUE,TRUE,FALSE)`, newID(), email, string(hash), fullName)
	return err
}

func EnsureKetuaUtamaUser(db *sql.DB, memberID, email, password string) error {
	memberID = strings.TrimSpace(memberID)
	email = strings.ToLower(strings.TrimSpace(email))
	if memberID == "" && email == "" && password == "" {
		return nil
	}
	if memberID == "" {
		return errors.New("KETUA_UTAMA_MEMBER_ID is required for Ketua Utama bootstrap")
	}
	var fullName, status string
	if err := db.QueryRow(`SELECT full_name,status FROM members WHERE id=$1`, memberID).Scan(&fullName, &status); err != nil {
		return fmt.Errorf("load Ketua Utama Member: %w", err)
	}
	if status != "active" {
		return errors.New("KETUA_UTAMA_MEMBER_ID must identify an active Member")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var userID string
	err = tx.QueryRow(`SELECT id FROM users WHERE member_id=$1 AND historical_identity=FALSE`, memberID).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		if email == "" || len(password) < 8 {
			return errors.New("KETUA_UTAMA_EMAIL and KETUA_UTAMA_PASSWORD are required when the Member has no User")
		}
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return hashErr
		}
		userID = newID()
		if _, err := tx.Exec(`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active,must_change_password,historical_identity) VALUES ($1,$2,$3,'member',$4,$5,TRUE,TRUE,FALSE)`, userID, email, string(hash), memberID, fullName); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var appointmentID string
	err = tx.QueryRow(`SELECT id FROM officer_appointments WHERE member_id=$1`, memberID).Scan(&appointmentID)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.Exec(`INSERT INTO officer_appointments (id,member_id,role,active) VALUES ($1,$2,'ketua_utama',TRUE)`, newID(), memberID)
	} else if err == nil {
		_, err = tx.Exec(`UPDATE officer_appointments SET role='ketua_utama',active=TRUE,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, appointmentID)
	}
	if err != nil {
		return err
	}
	if err := syncOfficerNotifications(tx, userID, "ketua_utama", true); err != nil {
		return err
	}
	return tx.Commit()
}

func CreateMemberUser(db *sql.DB, email, password, memberID string) (User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}

	user := User{
		ID:                 newID(),
		Email:              email,
		Role:               "member",
		MemberID:           sql.NullString{String: memberID, Valid: true},
		Active:             true,
		MustChangePassword: true,
	}
	_, err = db.Exec(
		`INSERT INTO users (id, email, password_hash, role, member_id, must_change_password, historical_identity) VALUES ($1, $2, $3, 'member', $4, TRUE, FALSE)`,
		user.ID,
		user.Email,
		string(hash),
		memberID,
	)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func AuthenticateUser(db *sql.DB, email, password string) (User, error) {
	var user User
	var passwordHash string
	err := db.QueryRow(
		`SELECT u.id,u.email,u.password_hash,CASE WHEN u.role='super_admin' THEN 'super_admin' ELSE COALESCE(CASE WHEN oa.active=TRUE AND m.status='active' THEN oa.role END,'member') END,u.member_id,COALESCE(m.member_no,''),COALESCE(m.status,''),COALESCE(NULLIF(u.full_name,''),m.full_name,u.email),u.active,u.must_change_password
		 FROM users u LEFT JOIN members m ON m.id=u.member_id LEFT JOIN officer_appointments oa ON oa.member_id=m.id
		 WHERE LOWER(u.email)=LOWER($1) AND u.historical_identity=FALSE`,
		email,
	).Scan(&user.ID, &user.Email, &passwordHash, &user.Role, &user.MemberID, &user.MemberNo, &user.MemberStatus, &user.FullName, &user.Active, &user.MustChangePassword)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if !user.Active || (user.Role != "super_admin" && user.MemberStatus != "active") {
		return User{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func UserByID(db *sql.DB, id string) (User, error) {
	var user User
	err := db.QueryRow(
		`SELECT u.id,u.email,CASE WHEN u.role='super_admin' THEN 'super_admin' ELSE COALESCE(CASE WHEN oa.active=TRUE AND m.status='active' THEN oa.role END,'member') END,u.member_id,COALESCE(m.member_no,''),COALESCE(m.status,''),COALESCE(NULLIF(u.full_name,''),m.full_name,u.email),u.active,u.must_change_password
		 FROM users u LEFT JOIN members m ON m.id=u.member_id LEFT JOIN officer_appointments oa ON oa.member_id=m.id
		 WHERE u.id=$1 AND u.historical_identity=FALSE`,
		id,
	).Scan(&user.ID, &user.Email, &user.Role, &user.MemberID, &user.MemberNo, &user.MemberStatus, &user.FullName, &user.Active, &user.MustChangePassword)
	return user, err
}

func SignToken(secret string, user User) (string, error) {
	claims := jwt.MapClaims{
		"user_id": user.ID,
		"email":   user.Email,
		"role":    user.Role,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	}
	if user.MemberID.Valid {
		claims["member_id"] = user.MemberID.String
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func ParseToken(secret, tokenValue string) (User, error) {
	token, err := jwt.Parse(tokenValue, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrUnauthorized
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return User{}, ErrUnauthorized
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return User{}, ErrUnauthorized
	}

	id, _ := claims["user_id"].(string)
	email, _ := claims["email"].(string)
	role, _ := claims["role"].(string)
	if id == "" || email == "" || role == "" {
		return User{}, ErrUnauthorized
	}

	user := User{ID: id, Email: email, Role: role}
	if memberID, ok := claims["member_id"].(string); ok && memberID != "" {
		user.MemberID = sql.NullString{String: memberID, Valid: true}
	}
	return user, nil
}

func newID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return hex.EncodeToString(data[:])
}
