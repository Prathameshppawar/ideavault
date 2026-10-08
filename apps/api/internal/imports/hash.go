package imports

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ContentHash returns a stable hex SHA-256 over the conversation's normalized
// (role, content) sequence, for deduplicating repeated imports of the same
// conversation. Titles, ids, timestamps, models and whitespace differences
// are deliberately ignored, as are empty messages.
func ContentHash(conv NormalizedConversation) string {
	h := sha256.New()
	h.Write([]byte("ideavault-conversation-v1\x1e"))
	for _, m := range conv.Messages {
		content := strings.Join(strings.Fields(SanitizeText(m.Content)), " ")
		if content == "" {
			continue
		}
		role := NormalizeRole(m.Role)
		if role == "" {
			role = strings.ToLower(strings.TrimSpace(m.Role))
		}
		h.Write([]byte(role))
		h.Write([]byte{0x1f})
		h.Write([]byte(content))
		h.Write([]byte{0x1e})
	}
	return hex.EncodeToString(h.Sum(nil))
}
