package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID       string
	Email    string
	Role     string
	MemberID sql.NullString
}

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

	_, err = db.Exec(
		`INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, $3, 'admin')`,
		newID(),
		email,
		string(hash),
	)
	return err
}

func CreateMemberUser(db *sql.DB, email, password, memberID string) (User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}

	user := User{
		ID:       newID(),
		Email:    email,
		Role:     "member",
		MemberID: sql.NullString{String: memberID, Valid: true},
	}
	_, err = db.Exec(
		`INSERT INTO users (id, email, password_hash, role, member_id) VALUES ($1, $2, $3, 'member', $4)`,
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
		`SELECT id, email, password_hash, role, member_id FROM users WHERE email = $1`,
		email,
	).Scan(&user.ID, &user.Email, &passwordHash, &user.Role, &user.MemberID)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func UserByID(db *sql.DB, id string) (User, error) {
	var user User
	err := db.QueryRow(
		`SELECT id, email, role, member_id FROM users WHERE id = $1`,
		id,
	).Scan(&user.ID, &user.Email, &user.Role, &user.MemberID)
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
