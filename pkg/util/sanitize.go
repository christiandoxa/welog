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
	// RedactedValue replaces sensitive values before logs leave the process.
	RedactedValue   = "[REDACTED]"
	truncatedSuffix = "...[truncated]"
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
	defaultSensitiveKeyStrategy = fragmentSensitiveKeyStrategy{fragments: sensitiveKeyFragments}
	defaultSanitizer            = NewSanitizer(DefaultMaxLoggedBodyBytes, defaultSensitiveKeyStrategy)
)

// SensitiveKeyStrategy decides which structured keys must be redacted.
type SensitiveKeyStrategy interface {
	IsSensitiveKey(key string) bool
}

type fragmentSensitiveKeyStrategy struct {
	fragments []string
}

func (s fragmentSensitiveKeyStrategy) IsSensitiveKey(key string) bool {
	normalized := normalizeKey(key)
	for _, fragment := range s.fragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

// Sanitizer applies the redaction strategy and payload size policy for logs.
type Sanitizer struct {
	maxBodyBytes int
	keyStrategy  SensitiveKeyStrategy
}

// NewSanitizer creates a sanitizer with explicit body-size and key-matching strategies.
func NewSanitizer(maxBodyBytes int, keyStrategy SensitiveKeyStrategy) Sanitizer {
	if maxBodyBytes <= 0 {
		maxBodyBytes = DefaultMaxLoggedBodyBytes
	}
	if keyStrategy == nil {
		keyStrategy = defaultSensitiveKeyStrategy
	}
	return Sanitizer{
		maxBodyBytes: maxBodyBytes,
		keyStrategy:  keyStrategy,
	}
}

// BuildBodyLogFields returns a sanitized JSON object and bounded string form for logging.
func BuildBodyLogFields(body []byte) (logrus.Fields, string) {
	return defaultSanitizer.BuildBodyLogFields(body)
}

// BuildBodyLogFields returns a sanitized JSON object and bounded string form for logging.
func (s Sanitizer) BuildBodyLogFields(body []byte) (logrus.Fields, string) {
	clipped, truncated := s.clipBytes(body)
	if len(clipped) == 0 {
		return logrus.Fields{}, ""
	}

	var decoded any
	if err := json.Unmarshal(clipped, &decoded); err != nil {
		return logrus.Fields{}, s.sanitizeRawBytes(clipped, truncated)
	}

	sanitized := s.SanitizeValue(decoded)
	bodyString, err := json.Marshal(sanitized)
	if err != nil {
		return logrus.Fields{}, s.sanitizeRawBytes(clipped, truncated)
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
	return defaultSanitizer.SanitizeRawString(value)
}

// SanitizeRawString caps and best-effort redacts unstructured payload strings.
func (s Sanitizer) SanitizeRawString(value string) string {
	clipped, truncated := s.clipBytes([]byte(value))
	return s.sanitizeRawBytes(clipped, truncated)
}

// SanitizeURL redacts sensitive query parameters and user-info passwords.
func SanitizeURL(rawURL string) string {
	return defaultSanitizer.SanitizeURL(rawURL)
}

// SanitizeURL redacts sensitive query parameters and user-info passwords.
func (s Sanitizer) SanitizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return s.SanitizeRawString(rawURL)
	}

	if parsed.User != nil {
		username := parsed.User.Username()
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(username, RedactedValue)
		}
	}

	query := parsed.Query()
	for key := range query {
		if s.IsSensitiveKey(key) {
			query[key] = []string{RedactedValue}
		}
	}
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

// SanitizeHTTPHeader copies and redacts net/http headers for logging.
func SanitizeHTTPHeader(headers http.Header) map[string]any {
	return defaultSanitizer.SanitizeHTTPHeader(headers)
}

// SanitizeHTTPHeader copies and redacts net/http headers for logging.
func (s Sanitizer) SanitizeHTTPHeader(headers http.Header) map[string]any {
	return s.SanitizeStringSliceMap(map[string][]string(headers))
}

// SanitizeStringSliceMap copies and redacts string-slice keyed values such as headers or metadata.
func SanitizeStringSliceMap(values map[string][]string) map[string]any {
	return defaultSanitizer.SanitizeStringSliceMap(values)
}

// SanitizeStringSliceMap copies and redacts string-slice keyed values such as headers or metadata.
func (s Sanitizer) SanitizeStringSliceMap(values map[string][]string) map[string]any {
	sanitized := make(map[string]any, len(values))
	for key, value := range values {
		if s.IsSensitiveKey(key) {
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
	return defaultSanitizer.SanitizeHeaderMap(headers)
}

// SanitizeHeaderMap copies and redacts generic header maps.
func (s Sanitizer) SanitizeHeaderMap(headers map[string]any) map[string]any {
	sanitized := make(map[string]any, len(headers))
	for key, value := range headers {
		if s.IsSensitiveKey(key) {
			sanitized[key] = RedactedValue
			continue
		}
		sanitized[key] = s.SanitizeValue(value)
	}
	return sanitized
}

// SanitizeValue recursively redacts fields whose keys commonly carry secrets.
func SanitizeValue(value any) any {
	return defaultSanitizer.SanitizeValue(value)
}

// SanitizeValue recursively redacts fields whose keys commonly carry secrets.
func (s Sanitizer) SanitizeValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return s.sanitizeAnyMap(v)
	case logrus.Fields:
		return s.sanitizeLogrusFields(v)
	case map[string]string:
		return s.sanitizeStringMap(v)
	case []any:
		return s.sanitizeAnySlice(v)
	default:
		return value
	}
}

func (s Sanitizer) sanitizeAnyMap(values map[string]any) map[string]any {
	sanitized := make(map[string]any, len(values))
	for key, item := range values {
		sanitized[key] = s.sanitizeKeyedValue(key, item)
	}
	return sanitized
}

func (s Sanitizer) sanitizeLogrusFields(values logrus.Fields) logrus.Fields {
	sanitized := logrus.Fields{}
	for key, item := range values {
		sanitized[key] = s.sanitizeKeyedValue(key, item)
	}
	return sanitized
}

func (s Sanitizer) sanitizeStringMap(values map[string]string) map[string]string {
	sanitized := make(map[string]string, len(values))
	for key, item := range values {
		if s.IsSensitiveKey(key) {
			sanitized[key] = RedactedValue
			continue
		}
		sanitized[key] = item
	}
	return sanitized
}

func (s Sanitizer) sanitizeAnySlice(values []any) []any {
	sanitized := make([]any, len(values))
	for i, item := range values {
		sanitized[i] = s.SanitizeValue(item)
	}
	return sanitized
}

func (s Sanitizer) sanitizeKeyedValue(key string, value any) any {
	if s.IsSensitiveKey(key) {
		return RedactedValue
	}
	return s.SanitizeValue(value)
}

// IsSensitiveKey reports whether a key should have its value redacted.
func IsSensitiveKey(key string) bool {
	return defaultSanitizer.IsSensitiveKey(key)
}

// IsSensitiveKey reports whether a key should have its value redacted.
func (s Sanitizer) IsSensitiveKey(key string) bool {
	return s.keyStrategy.IsSensitiveKey(key)
}

func (s Sanitizer) clipBytes(body []byte) ([]byte, bool) {
	if len(body) <= s.maxBodyBytes {
		return body, false
	}
	return body[:s.maxBodyBytes], true
}

func (s Sanitizer) sanitizeRawBytes(body []byte, truncated bool) string {
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
