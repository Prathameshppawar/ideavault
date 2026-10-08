// Package security holds credential encryption, password hashing, session tokens
// and the SSRF-safe HTTP client used for fetching public URLs.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Sealer encrypts secrets at rest with AES-256-GCM. The key never touches the database.
type Sealer struct {
	aead    cipher.AEAD
	version int
}

// NewSealer builds a sealer from a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead, version: 1}, nil
}

// Seal encrypts plaintext. additionalData binds the ciphertext to its owner (e.g. "user:provider:name").
func (s *Sealer) Seal(plaintext []byte, additionalData string) (ciphertext, nonce []byte, version int, err error) {
	nonce = make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, 0, err
	}
	return s.aead.Seal(nil, nonce, plaintext, []byte(additionalData)), nonce, s.version, nil
}

// Open decrypts a sealed secret.
func (s *Sealer) Open(ciphertext, nonce []byte, additionalData string) ([]byte, error) {
	pt, err := s.aead.Open(nil, nonce, ciphertext, []byte(additionalData))
	if err != nil {
		return nil, errors.New("credential decryption failed (wrong MASTER_KEY or tampered data)")
	}
	return pt, nil
}

// MaskSecret renders a non-reversible hint like "sk-…a1b2" for UI display.
func MaskSecret(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 8 {
		return "••••"
	}
	prefix := ""
	for _, p := range []string{"sk-ant-", "sk-proj-", "sk-", "gsk_", "AIza", "ghp_", "github_pat_", "xoxb-"} {
		if strings.HasPrefix(s, p) {
			prefix = p
			break
		}
	}
	return prefix + "…" + s[len(s)-4:]
}

// Argon2id parameters (OWASP-recommended baseline for interactive logins).
const (
	argonTime    = 2
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword returns an encoded Argon2id hash.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("password must be at least 8 characters")
	}
	if len(password) > 1024 {
		return "", errors.New("password is too long")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a password against an encoded Argon2id hash in constant time.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var mem uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, mem, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewToken returns a random URL-safe token and its SHA-256 hash (only the hash is stored).
func NewToken(prefix string) (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", nil, err
	}
	token = prefix + base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken hashes a bearer/session token for storage and lookup.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
