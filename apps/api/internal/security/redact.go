package security

import "github.com/Prathameshppawar/ideavault/apps/api/internal/observability"

// RedactSecretError renders an error message with any secret-looking substrings removed.
func RedactSecretError(err error) string {
	if err == nil {
		return ""
	}
	return observability.RedactString(err.Error())
}
