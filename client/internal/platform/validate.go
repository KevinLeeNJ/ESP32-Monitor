package platform

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var errInvalidURL = errors.New("invalid_server_url")

var githubLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

// ValidGitHubLogin reports whether login is accepted by the credential store
// and GitHub providers.
func ValidGitHubLogin(login string) bool {
	return githubLoginPattern.MatchString(login) && !strings.Contains(login, "--")
}

// ParseHTTPURL parses an absolute http(s) URL without userinfo, query or
// fragment. Callers add their own host/path restrictions.
func ParseHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errInvalidURL
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errInvalidURL
	}
	return u, nil
}
