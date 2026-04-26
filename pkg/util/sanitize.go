package util

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/goccy/go-json"
	"github.com/sirupsen/logrus"
)

const (
	// DefaultMaxLoggedBodyBytes caps payload copies kept only for logging.
	DefaultMaxLoggedBodyBytes = 64 * 1024
	RedactedValue             = "[REDACTED]"
	truncatedSuffix           = "...[truncated]"
)

var (
	jsonSensitiveValuePattern = regexp.MustCompile(`(?i)("([^"\\]|\\.)*(password|passwd|secret|token|authorization|cookie|credential|api[_-]?key|client[_-]?secret|private[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|jwt|session[_-]?id)([^"\\]|\\.)*"\s*:\s*)"([^"\\]|\\.)*"`)
	formSensitiveValuePattern = regexp.MustCompile(`(?i)(^|[&\s])((?:password|passwd|secret|token|authorization|credential|api[_-]?key|client[_-]?secret|access[_-]?token|refresh[_-]?token|id[_-]?token|jwt|session[_-]?id)=)([^&\s]+)`)
	sensitiveKeyFragments     = []string{
		"authorization",
		"cookie",
		"password",
		"passwd",
		"secret",
		"token",
		"apikey",
		"clientsecret",
		"privatekey",
		"credential",
		"sessionid",
		"jwt",
		"accesstoken",
		"refreshtoken",
		"idtoken",
	}
)

// BuildBodyLogFields returns a sanitized JSON object and bounded string form for logging.
func BuildBodyLogFields(body []byte) (logrus.Fields, string) {
	clipped, truncated := clipBytes(body)
	if len(clipped) == 0 {
		return logrus.Fields{}, ""
	}

	var decoded any
	if err := json.Unmarshal(clipped, &decoded); err != nil {
		return logrus.Fields{}, sanitizeRawBytes(clipped, truncated)
	}

	sanitized := SanitizeValue(decoded)
	bodyString, err := json.Marshal(sanitized)
	if err != nil {
		return logrus.Fields{}, sanitizeRawBytes(clipped, truncated)
	}

	fields := logrus.Fields{}
	if mapped, ok := sanitized.(map[string]any); ok {
		for key, value := range mapped {
			fields[key] = value
		}
	}
	if truncated {
		fields["_truncated"] = true
		return fields, string(bodyString) + truncatedSuffix
	}

	return fields, string(bodyString)
}

// SanitizeRawString caps and best-effort redacts unstructured payload strings.
func SanitizeRawString(value string) string {
	clipped, truncated := clipBytes([]byte(value))
	return sanitizeRawBytes(clipped, truncated)
}

// SanitizeURL redacts sensitive query parameters and user-info passwords.
func SanitizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return SanitizeRawString(rawURL)
	}

	if parsed.User != nil {
		username := parsed.User.Username()
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(username, RedactedValue)
		}
	}

	query := parsed.Query()
	for key := range query {
		if IsSensitiveKey(key) {
			query[key] = []string{RedactedValue}
		}
	}
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

// SanitizeHTTPHeader copies and redacts net/http headers for logging.
func SanitizeHTTPHeader(headers http.Header) map[string]any {
	return SanitizeStringSliceMap(map[string][]string(headers))
}

// SanitizeStringSliceMap copies and redacts string-slice keyed values such as headers or metadata.
func SanitizeStringSliceMap(values map[string][]string) map[string]any {
	sanitized := make(map[string]any, len(values))
	for key, value := range values {
		if IsSensitiveKey(key) {
			sanitized[key] = RedactedValue
			continue
		}
		if len(value) == 1 {
			sanitized[key] = value[0]
			continue
		}
		copied := append([]string(nil), value...)
		sanitized[key] = copied
	}
	return sanitized
}

// SanitizeHeaderMap copies and redacts generic header maps.
func SanitizeHeaderMap(headers map[string]any) map[string]any {
	sanitized := make(map[string]any, len(headers))
	for key, value := range headers {
		if IsSensitiveKey(key) {
			sanitized[key] = RedactedValue
			continue
		}
		sanitized[key] = SanitizeValue(value)
	}
	return sanitized
}

// SanitizeValue recursively redacts fields whose keys commonly carry secrets.
func SanitizeValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		sanitized := make(map[string]any, len(v))
		for key, item := range v {
			if IsSensitiveKey(key) {
				sanitized[key] = RedactedValue
				continue
			}
			sanitized[key] = SanitizeValue(item)
		}
		return sanitized
	case logrus.Fields:
		sanitized := logrus.Fields{}
		for key, item := range v {
			if IsSensitiveKey(key) {
				sanitized[key] = RedactedValue
				continue
			}
			sanitized[key] = SanitizeValue(item)
		}
		return sanitized
	case map[string]string:
		sanitized := make(map[string]string, len(v))
		for key, item := range v {
			if IsSensitiveKey(key) {
				sanitized[key] = RedactedValue
				continue
			}
			sanitized[key] = item
		}
		return sanitized
	case []any:
		sanitized := make([]any, len(v))
		for i, item := range v {
			sanitized[i] = SanitizeValue(item)
		}
		return sanitized
	default:
		return value
	}
}

func IsSensitiveKey(key string) bool {
	normalized := normalizeKey(key)
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func clipBytes(body []byte) ([]byte, bool) {
	if len(body) <= DefaultMaxLoggedBodyBytes {
		return body, false
	}
	return body[:DefaultMaxLoggedBodyBytes], true
}

func sanitizeRawBytes(body []byte, truncated bool) string {
	sanitized := string(body)
	sanitized = jsonSensitiveValuePattern.ReplaceAllString(sanitized, `${1}"`+RedactedValue+`"`)
	sanitized = formSensitiveValuePattern.ReplaceAllString(sanitized, `${1}${2}`+RedactedValue)
	if truncated {
		sanitized += truncatedSuffix
	}
	return sanitized
}

func normalizeKey(key string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r >= '0' && r <= '9':
			return r
		default:
			return -1
		}
	}, key)
}
