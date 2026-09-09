package llm

import (
	"net/url"
	"regexp"
	"strings"
)

var credentialQuery = regexp.MustCompile(`(?i)([?&](?:key|api_key|api-key|access_token)=)[^&\s"<>]+`)

// RedactDiagnostic also covers persisted errors from older query-key clients.
func RedactDiagnostic(message string) string {
	return credentialQuery.ReplaceAllString(message, "${1}[REDACTED]")
}

type redactedError struct {
	message string
	cause   error
}

func (e *redactedError) Error() string { return e.message }
func (e *redactedError) Unwrap() error { return e.cause }

// Preserve error identity for cancellation and retry checks without displaying
// credentials reflected by a transport, proxy, or remote error response.
func redactGeminiError(err error, key string) error {
	if err == nil {
		return err
	}
	message := RedactDiagnostic(err.Error())
	for _, secret := range []string{key, url.QueryEscape(key), url.PathEscape(key)} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	if message == err.Error() {
		return err
	}
	return &redactedError{message: message, cause: err}
}
