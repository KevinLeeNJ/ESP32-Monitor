// Package logexport packages the bounded client and Agent logs plus a redacted
// diagnostic snapshot into one archive. Redaction is intentionally aggressive:
// exported files may be attached to public issues.
package logexport

import (
	"regexp"
	"strings"
)

type replaceRule struct {
	pattern     *regexp.Regexp
	replacement string
}

// Bearer credentials, provider tokens, secret-looking key/value pairs, URL
// credentials, long hex digests and UUIDs are masked. Device UUIDs keep their
// first 8 characters so separate log lines stay correlatable.
var redactionRules = []replaceRule{
	{regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]{8,}`), `${1}[redacted]`},
	{regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`), `[redacted-token]`},
	{regexp.MustCompile(`(?i)([?&](?:access_token|refresh_token|token|api[_-]?key|key|secret|code|password)=)[^&\s"'#]+`), `${1}[redacted]`},
	{regexp.MustCompile(`(?i)\b([a-z0-9_]*(?:token|secret|password|passwd|api[_-]?key|authorization)[a-z0-9_]*=)[^\s&"',;]+`), `${1}[redacted]`},
	{regexp.MustCompile(`(?i)("(?:[a-z0-9_]*(?:token|secret|password|api[_-]?key)[a-z0-9_]*)"\s*:\s*")[^"]*(")`), `${1}[redacted]${2}`},
	{regexp.MustCompile(`://[^/\s:@]+:[^/\s@]+@`), `://[redacted]@`},
	{regexp.MustCompile(`\b[0-9a-fA-F]{40,}\b`), `[redacted-hex]`},
	{regexp.MustCompile(`\b([0-9a-fA-F]{8})-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`), `${1}-[redacted-id]`},
}

// Redactor masks personal paths and credentials while keeping values that are
// useful for support: timestamps, components, event names, counters, LAN
// addresses and error codes.
type Redactor struct {
	paths []string
}

// NewRedactor builds a redactor for one machine. Empty paths are ignored.
func NewRedactor(paths ...string) *Redactor {
	seen := make(map[string]struct{}, len(paths)*2)
	r := &Redactor{}
	for _, path := range paths {
		path = strings.TrimRight(path, `/\`)
		if len(path) < 3 {
			continue
		}
		candidates := []string{path}
		if strings.Contains(path, `\`) {
			candidates = append(candidates,
				strings.ReplaceAll(path, `\`, `\\`), // JSON/structured logs escape separators
				strings.ReplaceAll(path, `\`, `/`),  // URLs and JSON may use forward slashes
			)
		}
		for _, candidate := range candidates {
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			r.paths = append(r.paths, candidate)
		}
	}
	return r
}

// Text returns a copy safe to place in an exported archive.
func (r *Redactor) Text(value string) string {
	if value == "" {
		return value
	}
	if r != nil {
		for _, path := range r.paths {
			value = strings.ReplaceAll(value, path, "~")
		}
	}
	for _, rule := range redactionRules {
		value = rule.pattern.ReplaceAllString(value, rule.replacement)
	}
	return value
}
