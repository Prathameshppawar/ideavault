package service

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/security"
)

// AuthResult is returned on successful setup/login.
type AuthResult struct {
	User      *domain.User    `json:"user"`
	Session   *domain.Session `json:"session"`
	Token     string          `json:"-"`
	ExpiresAt time.Time       `json:"expires_at"`
}

// SetupRequired reports whether the vault has no owner yet (first run).
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	n, err := s.store.CountUsers(ctx)
	return n == 0, err
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", domain.Invalid("email", "enter a valid email address")
	}
	return email, nil
}

// Setup creates the vault owner. Allowed only when no user exists (or ALLOW_SIGNUP is set).
func (s *Service) Setup(ctx context.Context, email, password, displayName, ua, ip string) (*AuthResult, error) {
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	if n > 0 && (s.cfg == nil || !s.cfg.AllowSignup) {
		return nil, domain.Forbidden("this vault already has an owner; sign in instead")
	}
	email, err = normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return nil, domain.Invalid("password", err.Error())
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = strings.Split(email, "@")[0]
	}
	u, err := s.store.CreateUser(ctx, email, trimTo(displayName, 80), hash)
	if err != nil {
		return nil, err
	}
	return s.newSession(ctx, u, "browser", "", ua, ip)
}

// Login verifies credentials and starts a session.
func (s *Service) Login(ctx context.Context, email, password, ua, ip string) (*AuthResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, hash, err := s.store.UserByEmail(ctx, email)
	if err != nil {
		// Equalize timing to avoid user enumeration.
		security.VerifyPassword(password, "$argon2id$v=19$m=65536,t=2,p=2$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g")
		return nil, domain.Unauthorized("invalid email or password")
	}
	if !security.VerifyPassword(password, hash) {
		return nil, domain.Unauthorized("invalid email or password")
	}
	return s.newSession(ctx, u, "browser", "", ua, ip)
}

func (s *Service) sessionTTL() time.Duration {
	if s.cfg != nil && s.cfg.SessionTTL > 0 {
		return s.cfg.SessionTTL
	}
	return 30 * 24 * time.Hour
}

func (s *Service) newSession(ctx context.Context, u *domain.User, kind, label, ua, ip string) (*AuthResult, error) {
	prefix := "ivs_"
	if kind == "api_token" {
		prefix = "ivt_"
	}
	tok, hash, err := security.NewToken(prefix)
	if err != nil {
		return nil, err
	}
	exp := time.Now().Add(s.sessionTTL())
	if kind == "api_token" {
		exp = time.Now().AddDate(1, 0, 0)
	}
	ss, err := s.store.CreateSession(ctx, u.ID, hash, kind, label, trimTo(ua, 300), trimTo(ip, 80), exp)
	if err != nil {
		return nil, err
	}
	return &AuthResult{User: u, Session: ss, Token: tok, ExpiresAt: exp}, nil
}

// Authenticate resolves a session/API token to its user.
func (s *Service) Authenticate(ctx context.Context, token string) (*domain.User, *domain.Session, error) {
	if !strings.HasPrefix(token, "ivs_") && !strings.HasPrefix(token, "ivt_") {
		return nil, nil, domain.Unauthorized("invalid session")
	}
	ss, err := s.store.SessionByTokenHash(ctx, security.HashToken(token))
	if err != nil {
		return nil, nil, domain.Unauthorized("session expired or invalid")
	}
	u, err := s.store.UserByID(ctx, ss.UserID)
	if err != nil {
		return nil, nil, domain.Unauthorized("session user not found")
	}
	return u, ss, nil
}

// Logout revokes a session token.
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.RevokeSession(ctx, security.HashToken(token))
}

// CreateAPIToken creates a personal API token (shown once).
func (s *Service) CreateAPIToken(ctx context.Context, userID uuid.UUID, label string) (*AuthResult, error) {
	u, err := s.store.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(label) == "" {
		label = "API token"
	}
	return s.newSession(ctx, u, "api_token", trimTo(label, 80), "", "")
}

// ChangePassword updates the password after verifying the current one.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, current, next string) error {
	u, err := s.store.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	_, hash, err := s.store.UserByEmail(ctx, u.Email)
	if err != nil {
		return err
	}
	if !security.VerifyPassword(current, hash) {
		return domain.Unauthorized("current password is incorrect")
	}
	nh, err := security.HashPassword(next)
	if err != nil {
		return domain.Invalid("password", err.Error())
	}
	return s.store.UpdatePassword(ctx, userID, nh)
}

// EnsureBootstrapUser creates the owner from BOOTSTRAP_EMAIL/BOOTSTRAP_PASSWORD when the vault is empty.
func (s *Service) EnsureBootstrapUser(ctx context.Context) error {
	if s.cfg == nil || s.cfg.BootstrapEmail == "" || s.cfg.BootstrapPass == "" {
		return nil
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil || n > 0 {
		return err
	}
	email, err := normalizeEmail(s.cfg.BootstrapEmail)
	if err != nil {
		return err
	}
	hash, err := security.HashPassword(s.cfg.BootstrapPass)
	if err != nil {
		return err
	}
	_, err = s.store.CreateUser(ctx, email, strings.Split(email, "@")[0], hash)
	return err
}

// FirstUserID returns the vault owner's id (personal mode).
func (s *Service) FirstUserID(ctx context.Context) (uuid.UUID, error) {
	return s.store.FirstUserID(ctx)
}
