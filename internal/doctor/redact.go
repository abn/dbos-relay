package doctor

import "regexp"

var (
	// userinfoPattern masks credentials in scheme://user:password@host URLs.
	userinfoPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s@]+@`)
	// secretKeyPattern masks key=value credentials in DSNs and query strings.
	secretKeyPattern = regexp.MustCompile(`(?i)\b(password|passfile|sslpassword|sslkey|secret|token)\s*=\s*[^\s;&]+`)
)

// SanitizeError renders an error without credentials. Doctor checks database
// connections and secret files, so error text may otherwise echo a DSN.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	message := userinfoPattern.ReplaceAllString(err.Error(), "${1}[REDACTED]@")
	return secretKeyPattern.ReplaceAllString(message, "$1=[REDACTED]")
}
