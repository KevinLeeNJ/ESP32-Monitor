package github

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Store is injected; production uses platform credential storage, tests use memory.
type Store interface {
	Load(context.Context, string) (string, error)
	Save(context.Context, string, string) error
	Delete(context.Context, string) error
}
type Options struct {
	ClientID    string // Public application registration ID; never a client_secret.
	Store       Store
	OpenBrowser func(context.Context, string) error
	HTTPClient  *http.Client
	Now         func() time.Time
}
type State string

const (
	SignedOut             State = "signed_out"
	Starting              State = "starting"
	AwaitingAuthorization State = "awaiting_authorization"
	Authorized            State = "authorized"
	Denied                State = "denied"
	Expired               State = "expired"
	Cancelled             State = "cancelled"
	Failed                State = "error"
)

// Session is safe for GUI bindings. Device/access/refresh tokens stay private.
type Session struct {
	ID              string    `json:"id"`
	State           State     `json:"state"`
	UserCode        string    `json:"user_code,omitempty"`
	VerificationURI string    `json:"verification_uri,omitempty"`
	ExpiresAt       time.Time `json:"expires_at"`
	NextPollAt      time.Time `json:"next_poll_at"`
	Login           string    `json:"login,omitempty"`
	ErrorCode       string    `json:"error_code,omitempty"`
}
type secretRecord struct {
	AccessToken      string     `json:"access_token"`
	RefreshToken     string     `json:"refresh_token,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	RefreshExpiresAt *time.Time `json:"refresh_expires_at,omitempty"`
}
type Client struct {
	mu                           sync.Mutex
	tokenMu                      sync.Mutex
	opts                         Options
	http                         *http.Client
	session                      Session
	deviceCode                   string
	interval                     time.Duration
	cancel                       context.CancelFunc
	busy                         bool
	deviceURL, tokenURL, userURL string
}

func New(opts Options) *Client {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	client := defaultClient()
	if opts.HTTPClient != nil {
		copy := *opts.HTTPClient
		copy.CheckRedirect = client.CheckRedirect
		client = &copy
	}
	return &Client{opts: opts, http: client, session: Session{State: SignedOut}, deviceURL: "https://github.com/login/device/code", tokenURL: "https://github.com/login/oauth/access_token", userURL: "https://api.github.com/user"}
}

var loginRegex = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

func validLogin(login string) bool {
	return loginRegex.MatchString(login) && !strings.Contains(login, "--")
}
func active(s State) bool { return s == Starting || s == AwaitingAuthorization }
func (c *Client) Status() Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	if active(c.session.State) && !c.session.ExpiresAt.IsZero() && !c.opts.Now().Before(c.session.ExpiresAt) {
		c.finish(Expired, "authorization_expired")
	}
	return c.session
}
func (c *Client) finish(state State, code string) {
	c.session.State = state
	c.session.ErrorCode = code
	c.deviceCode = ""
	c.session.UserCode = ""
	c.session.VerificationURI = ""
	c.session.NextPollAt = time.Time{}
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
}

// StartLogin requests a short-lived code and asks the operating system to open
// its default browser. PollLogin is called by the Agent/GUI while pending.
func (c *Client) StartLogin(ctx context.Context) (Session, error) {
	c.mu.Lock()
	if active(c.session.State) {
		s := c.session
		c.mu.Unlock()
		return s, errors.New("login_in_progress")
	}
	if c.opts.ClientID == "" || c.opts.Store == nil || c.opts.OpenBrowser == nil {
		c.mu.Unlock()
		return Session{}, errors.New("github_oauth_not_configured")
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		c.mu.Unlock()
		return Session{}, errors.New("auth_random_failed")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	c.cancel = cancel
	c.session = Session{ID: hex.EncodeToString(id), State: Starting}
	c.busy = false
	sessionID := c.session.ID
	c.mu.Unlock()
	var result deviceResponse
	err := c.post(requestCtx, c.deviceURL, url.Values{"client_id": {c.opts.ClientID}, "scope": {"read:user"}}, &result)
	if err == nil && (result.DeviceCode == "" || result.UserCode == "" || result.VerificationURI != "https://github.com/login/device" || result.ExpiresIn <= 0 || result.ExpiresIn > 900 || result.Interval < 1 || result.Interval > 900) {
		err = errors.New("auth_invalid_response")
	}
	c.mu.Lock()
	if c.session.ID != sessionID || c.session.State != Starting {
		s := c.session
		c.mu.Unlock()
		cancel()
		return s, errors.New("login_cancelled")
	}
	if err != nil {
		c.finish(Failed, err.Error())
		s := c.session
		c.mu.Unlock()
		return s, err
	}
	c.deviceCode = result.DeviceCode
	c.interval = time.Duration(result.Interval) * time.Second
	c.session.State = AwaitingAuthorization
	c.session.UserCode = result.UserCode
	c.session.VerificationURI = result.VerificationURI
	c.session.ExpiresAt = c.opts.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	c.session.NextPollAt = c.opts.Now().Add(c.interval)
	c.mu.Unlock()
	err = c.opts.OpenBrowser(requestCtx, result.VerificationURI)
	c.mu.Lock()
	defer c.mu.Unlock()
	defer cancel()
	if c.session.ID != sessionID || c.session.State != AwaitingAuthorization {
		return c.session, errors.New("login_cancelled")
	}
	c.cancel = nil
	if err != nil {
		c.finish(Failed, "browser_open_failed")
		return c.session, errors.New("browser_open_failed")
	}
	return c.session, nil
}
func (c *Client) CancelLogin(id string) Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == c.session.ID && active(c.session.State) {
		c.finish(Cancelled, "login_cancelled")
	}
	return c.session
}

// PollLogin honors the upstream interval even if the GUI calls it repeatedly.
func (c *Client) PollLogin(ctx context.Context, id string) (Session, error) {
	c.mu.Lock()
	if id != c.session.ID {
		c.mu.Unlock()
		return Session{}, errors.New("unknown_login_session")
	}
	if c.session.State != AwaitingAuthorization || c.busy {
		s := c.session
		c.mu.Unlock()
		return s, nil
	}
	now := c.opts.Now()
	if !now.Before(c.session.ExpiresAt) {
		c.finish(Expired, "authorization_expired")
		s := c.session
		c.mu.Unlock()
		return s, nil
	}
	if now.Before(c.session.NextPollAt) {
		s := c.session
		c.mu.Unlock()
		return s, nil
	}
	c.busy = true
	c.session.NextPollAt = now.Add(c.interval)
	deviceCode := c.deviceCode
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	c.cancel = cancel
	c.mu.Unlock()
	defer cancel()
	var token tokenResponse
	err := c.post(requestCtx, c.tokenURL, url.Values{"client_id": {c.opts.ClientID}, "device_code": {deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, &token)
	c.mu.Lock()
	if c.session.ID != id || c.session.State != AwaitingAuthorization {
		s := c.session
		c.mu.Unlock()
		return s, errors.New("login_cancelled")
	}
	c.busy = false
	c.cancel = nil
	if err != nil {
		c.session.ErrorCode = err.Error()
		c.session.NextPollAt = c.opts.Now().Add(max(c.interval, 30*time.Second))
		s := c.session
		c.mu.Unlock()
		return s, err
	}
	c.session.ErrorCode = ""
	switch token.Error {
	case "authorization_pending":
		s := c.session
		c.mu.Unlock()
		return s, nil
	case "slow_down":
		c.interval = max(c.interval+5*time.Second, time.Duration(min(token.Interval, 900))*time.Second)
		c.session.NextPollAt = c.opts.Now().Add(c.interval)
		s := c.session
		c.mu.Unlock()
		return s, nil
	case "access_denied":
		c.finish(Denied, "access_denied")
		s := c.session
		c.mu.Unlock()
		return s, nil
	case "expired_token", "token_expired":
		c.finish(Expired, "authorization_expired")
		s := c.session
		c.mu.Unlock()
		return s, nil
	case "":
	default:
		c.finish(Failed, "oauth_configuration_error")
		s := c.session
		c.mu.Unlock()
		return s, errors.New("oauth_configuration_error")
	}
	if token.AccessToken == "" || !strings.EqualFold(token.Type, "bearer") {
		c.finish(Failed, "auth_invalid_response")
		s := c.session
		c.mu.Unlock()
		return s, errors.New("auth_invalid_response")
	}
	// Verify the account returned by GitHub rather than trusting a user-entered login.
	c.busy = true
	c.cancel = cancel
	c.mu.Unlock()
	login, err := c.identity(requestCtx, token.AccessToken)
	record, recordErr := makeRecord(token, c.opts.Now())
	if err == nil {
		err = recordErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session.ID != id || c.session.State != AwaitingAuthorization {
		return c.session, errors.New("login_cancelled")
	}
	c.busy = false
	c.cancel = nil
	if err == nil {
		// Serialize save against cancellation/logout so cancelled sessions cannot log in later.
		c.tokenMu.Lock()
		defer c.tokenMu.Unlock()
		bytes, _ := json.Marshal(record)
		if c.opts.Store.Save(requestCtx, login, string(bytes)) != nil {
			err = errors.New("credential_save_failed")
		}
	}
	if err != nil {
		c.finish(Failed, err.Error())
		return c.session, err
	}
	c.session.Login = login
	c.finish(Authorized, "")
	return c.session, nil
}
func makeRecord(token tokenResponse, now time.Time) (secretRecord, error) {
	r := secretRecord{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken}
	if token.ExpiresIn < 0 || token.RefreshExpiresIn < 0 {
		return r, errors.New("auth_invalid_response")
	}
	if token.ExpiresIn > 0 {
		if token.RefreshToken == "" {
			return r, errors.New("auth_invalid_response")
		}
		v := now.Add(time.Duration(min(token.ExpiresIn, 31536000)) * time.Second)
		r.ExpiresAt = &v
	}
	if token.RefreshExpiresIn > 0 {
		v := now.Add(time.Duration(min(token.RefreshExpiresIn, 31536000)) * time.Second)
		r.RefreshExpiresAt = &v
	}
	return r, nil
}

// Token is the contribution provider's TokenSource. Refresh tokens are rotated
// inside the platform credential store and never returned to the GUI or snapshots.
func (c *Client) Token(ctx context.Context, login string) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if !validLogin(login) || c.opts.Store == nil {
		return "", errors.New("auth_required")
	}
	raw, err := c.opts.Store.Load(ctx, login)
	if err != nil {
		return "", errors.New("auth_required")
	}
	var record secretRecord
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return raw, nil
	} // Existing manually registered PAT.
	if json.Unmarshal([]byte(raw), &record) != nil || record.AccessToken == "" {
		return "", errors.New("auth_required")
	}
	now := c.opts.Now()
	if record.ExpiresAt == nil || record.ExpiresAt.After(now.Add(time.Minute)) {
		return record.AccessToken, nil
	}
	if c.opts.ClientID == "" || record.RefreshToken == "" || (record.RefreshExpiresAt != nil && !record.RefreshExpiresAt.After(now)) {
		return "", errors.New("auth_required")
	}
	var token tokenResponse
	err = c.post(ctx, c.tokenURL, url.Values{"client_id": {c.opts.ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {record.RefreshToken}}, &token)
	if err != nil {
		return "", err
	}
	if token.Error != "" || token.AccessToken == "" || !strings.EqualFold(token.Type, "bearer") {
		return "", errors.New("auth_required")
	}
	identity, err := c.identity(ctx, token.AccessToken)
	if err != nil || !strings.EqualFold(identity, login) {
		return "", errors.New("auth_required")
	}
	next, err := makeRecord(token, now)
	if err != nil {
		return "", err
	}
	bytes, _ := json.Marshal(next)
	if c.opts.Store.Save(ctx, login, string(bytes)) != nil {
		return "", errors.New("credential_save_failed")
	}
	return next.AccessToken, nil
}

// Logout removes local credentials. It does not revoke every GitHub grant.
func (c *Client) Logout(ctx context.Context, login string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if active(c.session.State) {
		c.finish(Cancelled, "login_cancelled")
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if !validLogin(login) || c.opts.Store == nil {
		return errors.New("invalid_github_login")
	}
	if c.opts.Store.Delete(ctx, login) != nil {
		return errors.New("credential_delete_failed")
	}
	c.session = Session{State: SignedOut}
	return nil
}
