//go:build linux

package platform

import (
	"context"
	"strings"

	ss "github.com/zalando/go-keyring/secret_service"
)

func linuxGitHubSecret(ctx context.Context, operation, account, value string) (string, error) {
	if ctx.Err() != nil || !ValidGitHubLogin(account) {
		return "", ErrCredentialUnavailable
	}
	svc, err := ss.NewSecretService()
	if err != nil {
		return "", ErrCredentialUnavailable
	}
	collection := svc.GetLoginCollection()
	if err = svc.Unlock(collection.Path()); err != nil || ctx.Err() != nil {
		return "", ErrCredentialUnavailable
	}
	attrs := map[string]string{"service": "esp32-monitor.github", "account": account}
	if operation == "save" {
		session, err := svc.OpenSession()
		if err != nil {
			return "", ErrCredentialUnavailable
		}
		defer svc.Close(session)
		if ctx.Err() != nil || svc.CreateItem(collection, "ESP32 Monitor GitHub", attrs, ss.NewSecret(session.Path(), value)) != nil {
			return "", ErrCredentialUnavailable
		}
		return "", nil
	}
	items, err := svc.SearchItems(collection, attrs)
	if err != nil {
		return "", ErrCredentialUnavailable
	}
	if operation == "delete" {
		for _, item := range items {
			if ctx.Err() != nil || svc.Delete(item) != nil {
				return "", ErrCredentialUnavailable
			}
		}
		return "", nil
	}
	if len(items) != 1 || ctx.Err() != nil {
		return "", ErrCredentialUnavailable
	}
	if err = svc.Unlock(items[0]); err != nil {
		return "", ErrCredentialUnavailable
	}
	session, err := svc.OpenSession()
	if err != nil {
		return "", ErrCredentialUnavailable
	}
	defer svc.Close(session)
	secret, err := svc.GetSecret(items[0], session.Path())
	if err != nil || ctx.Err() != nil || len(secret.Value) == 0 || len(secret.Value) > 16384 {
		return "", ErrCredentialUnavailable
	}
	token := strings.TrimSpace(string(secret.Value))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", ErrCredentialUnavailable
	}
	return token, nil
}
