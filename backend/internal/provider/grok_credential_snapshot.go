package provider

import (
	"encoding/json"
	"errors"
	"strings"
)

type grokCredentialFailureSnapshotError struct {
	cause    error
	snapshot CredentialMutationSnapshot
}

func (e *grokCredentialFailureSnapshotError) Error() string { return e.cause.Error() }
func (e *grokCredentialFailureSnapshotError) Unwrap() error { return e.cause }
func WithGrokCredentialFailureSnapshot(err error, provider *Record) error {
	if err == nil || provider == nil || !provider.IsGrokOAuth() {
		return err
	}
	if _, ok := errors.AsType[*grokCredentialFailureSnapshotError](err); ok {
		return err
	}
	return &grokCredentialFailureSnapshotError{cause: err, snapshot: GrokCredentialMutationSnapshot(provider)}
}

func GrokCredentialFailureSnapshot(err error) (CredentialMutationSnapshot, bool) {
	var snapshotErr *grokCredentialFailureSnapshotError
	if !errors.As(err, &snapshotErr) || snapshotErr == nil {
		return CredentialMutationSnapshot{}, false
	}
	return snapshotErr.snapshot, true
}

func GrokCredentialMutationSnapshot(provider *Record) CredentialMutationSnapshot {
	if provider == nil {
		return CredentialMutationSnapshot{}
	}
	credentialsJSON := "null"
	if encoded, err := json.Marshal(provider.Credentials); err == nil {
		credentialsJSON = string(encoded)
	}
	snapshot := CredentialMutationSnapshot{
		CredentialsJSON: credentialsJSON,

		AccessToken: strings.TrimSpace(provider.GetGrokAccessToken()),

		RefreshToken: strings.TrimSpace(provider.GetGrokRefreshToken()),

		TokenVersion: provider.GetCredentialAsInt64("_token_version"),
	}
	if provider.ProxyID != nil {
		proxyID := *provider.ProxyID
		snapshot.ProxyID = &proxyID
	}
	return snapshot
}

func GrokCredentialProxyIDsEqual(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
