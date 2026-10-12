package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/qoder"
)

type qoderValidationHTTPUpstreamStub struct {
	proxyURL            string
	providerID          int64
	providerConcurrency int
}

type qoderValidationStore struct {
	providercore.AdminStore
	providers map[int64]*providercore.Record
}

func TestValidateQoderCosyCredentialsAcceptsDirectToken(t *testing.T) {
	validator := &qoderCredentialValidator{}
	provider := &providercore.Record{
		Platform: capability.PlatformQoder,
		Type:     capability.ProviderTypeCosy,
		Credentials: map[string]any{
			"security_oauth_token": "dt-token",
			"machine_id":           "machine-1",
			"uid":                  "uid-1",
		},
	}

	require.NoError(t, validator.validate(context.Background(), provider))
	require.Empty(t, provider.GetCredential("machine_token"))
	require.Empty(t, provider.GetCredential("machine_type"))
}

func TestCreateQoderDirectTokenProviderPersistsStableMachineIdentity(t *testing.T) {
	validator := &qoderCredentialValidator{}
	repo := &qoderValidationStore{}
	svc := newQoderValidationAdmin(repo, validator)

	created, err := svc.CreateProvider(context.Background(), &providercore.CreateProviderInput{
		Name:     "qoder-direct",
		Platform: capability.PlatformQoder,
		Type:     capability.ProviderTypeCosy,
		Credentials: map[string]any{
			"security_oauth_token": "dt-token",
			"machine_id":           "machine-1",
			"uid":                  "uid-1",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "machine-1", created.GetCredential("machine_id"))
	require.NotEmpty(t, created.GetCredential("machine_token"))
	require.NotEmpty(t, created.GetCredential("machine_type"))
}

func TestCreateQoderDirectTokenProviderRejectsMissingMachineID(t *testing.T) {
	validator := &qoderCredentialValidator{}
	repo := &qoderValidationStore{}
	svc := newQoderValidationAdmin(repo, validator)
	credentials := map[string]any{
		"security_oauth_token": "dt-token",
		"uid":                  "uid-1",
	}

	_, err := svc.CreateProvider(context.Background(), &providercore.CreateProviderInput{
		Name:        "qoder-direct",
		Platform:    capability.PlatformQoder,
		Type:        capability.ProviderTypeCosy,
		Credentials: credentials,
	})

	require.ErrorContains(t, err, "machine_id")
	require.Empty(t, credentials["machine_id"])
	require.Empty(t, credentials["machine_token"])
	require.Empty(t, credentials["machine_type"])
}

func TestCreateQoderPATProviderPersistsStableMachineIdentity(t *testing.T) {
	validator := &qoderCredentialValidator{}
	validator.validatePAT = func(_ context.Context, _ *providercore.Record, _ string, machine *qoder.MachineIdentity) (*qoder.AuthIdentity, error) {
		require.NotEmpty(t, machine.MachineID)
		require.NotEmpty(t, machine.MachineToken)
		require.NotEmpty(t, machine.MachineType)
		return &qoder.AuthIdentity{UID: "uid-1", SecurityOauthToken: "dt-token"}, nil
	}
	repo := &qoderValidationStore{}
	svc := newQoderValidationAdmin(repo, validator)

	created, err := svc.CreateProvider(context.Background(), &providercore.CreateProviderInput{
		Name:        "qoder-pat",
		Platform:    capability.PlatformQoder,
		Type:        capability.ProviderTypeCosy,
		Credentials: map[string]any{"pat": "pat-123"},
	})

	require.NoError(t, err)
	require.NotEmpty(t, created.GetCredential("machine_id"))
	require.NotEmpty(t, created.GetCredential("machine_token"))
	require.NotEmpty(t, created.GetCredential("machine_type"))
}

func TestCreateQoderCNPATProviderUsesOfficialMachineIdentity(t *testing.T) {
	validator := &qoderCredentialValidator{}
	validator.validateCNPAT = func(_ context.Context, _ *providercore.Record, _ string, machine *qoder.MachineIdentity, _ qoder.RequestDoer) (*qoder.AuthIdentity, error) {
		parsedMachineID, err := uuid.Parse(machine.MachineID)
		require.NoError(t, err)
		require.Equal(t, uuid.Version(4), parsedMachineID.Version())
		require.Empty(t, machine.MachineToken)
		require.Empty(t, machine.MachineType)
		return &qoder.AuthIdentity{UID: "uid-cn", SecurityOauthToken: "dt-cn"}, nil
	}
	repo := &qoderValidationStore{}
	svc := newQoderValidationAdmin(repo, validator)

	created, err := svc.CreateProvider(context.Background(), &providercore.CreateProviderInput{
		Name:     "qoder-cn-pat",
		Platform: capability.PlatformQoder,
		Type:     capability.ProviderTypeCosy,
		Credentials: map[string]any{
			"site": "cn",
			"pat":  "pat-cn",
		},
	})

	require.NoError(t, err)
	require.Len(t, created.GetCredential("machine_id"), 36)
	require.Empty(t, created.GetCredential("machine_token"))
	require.Empty(t, created.GetCredential("machine_type"))
}

func TestUpdateQoderDirectTokenProviderPreservesLegacyMachineFallback(t *testing.T) {
	validator := &qoderCredentialValidator{}
	const providerID int64 = 1202
	repo := &qoderValidationStore{providers: map[int64]*providercore.Record{
		providerID: {
			ID:       providerID,
			Name:     "legacy-qoder",
			Platform: capability.PlatformQoder,
			Type:     capability.ProviderTypeCosy,
			Status:   billing.StatusActive,
			Credentials: map[string]any{
				"security_oauth_token": "dt-token",
				"machine_id":           "legacy-machine",
				"uid":                  "uid-1",
			},
		},
	}}
	svc := newQoderValidationAdmin(repo, validator)
	updatedName := "renamed-qoder"

	updated, err := svc.UpdateProvider(context.Background(), providerID, &providercore.UpdateProviderInput{Name: updatedName})

	require.NoError(t, err)
	require.Equal(t, updatedName, updated.Name)
	require.Equal(t, "legacy-machine", updated.GetCredential("machine_id"))
	require.Empty(t, updated.GetCredential("machine_token"))
	require.Empty(t, updated.GetCredential("machine_type"))
}

func TestValidateQoderCosyCredentialsAcceptsDirectTokenWithAID(t *testing.T) {
	validator := &qoderCredentialValidator{}
	provider := &providercore.Record{
		Platform: capability.PlatformQoder,
		Type:     capability.ProviderTypeCosy,
		Credentials: map[string]any{
			"security_oauth_token": "dt-token",
			"machine_id":           "machine-1",
			"aid":                  "aid-1",
		},
	}

	require.NoError(t, validator.validate(context.Background(), provider))
}

func TestValidateQoderCosyCredentialsRejectsDirectTokenWithoutIdentity(t *testing.T) {
	validator := &qoderCredentialValidator{}
	provider := &providercore.Record{
		Platform: capability.PlatformQoder,
		Type:     capability.ProviderTypeCosy,
		Credentials: map[string]any{
			"security_oauth_token": "dt-token",
			"machine_id":           "machine-1",
		},
	}

	require.ErrorContains(t, validator.validate(context.Background(), provider), "uid or aid")
}

func TestValidateQoderCosyCredentialsRejectsUnknownSiteAndRefreshMode(t *testing.T) {
	validator := &qoderCredentialValidator{}
	baseCredentials := map[string]any{
		"security_oauth_token": "dt-token",
		"machine_id":           "machine-1",
		"uid":                  "uid-1",
	}
	provider := &providercore.Record{Platform: capability.PlatformQoder, Type: capability.ProviderTypeCosy, Credentials: baseCredentials}
	provider.Credentials["site"] = "unknown"
	require.ErrorContains(t, validator.validate(context.Background(), provider), "unsupported site")

	provider.Credentials["site"] = "cn"
	provider.Credentials["refresh_mode"] = "unknown"
	require.ErrorContains(t, validator.validate(context.Background(), provider), "unsupported refresh_mode")
}

func TestValidateQoderCosyCredentialsRejectsMachineIDOnly(t *testing.T) {
	validator := &qoderCredentialValidator{}
	provider := &providercore.Record{
		Platform:    capability.PlatformQoder,
		Type:        capability.ProviderTypeCosy,
		Credentials: map[string]any{"machine_id": "machine-1"},
	}

	require.ErrorContains(t, validator.validate(context.Background(), provider), "pat or security_oauth_token")
}

func TestValidateQoderCosyCredentialsRejectsNonCosyQoderProviderType(t *testing.T) {
	validator := &qoderCredentialValidator{}
	provider := &providercore.Record{
		Platform:    capability.PlatformQoder,
		Type:        capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"api_key": "key"},
	}

	require.ErrorContains(t, validator.validate(context.Background(), provider), "require cosy")
}

func TestValidateQoderCosyCredentialsRejectsCosyNonQoderPlatform(t *testing.T) {
	validator := &qoderCredentialValidator{}
	provider := &providercore.Record{
		Platform:    capability.PlatformAnthropic,
		Type:        capability.ProviderTypeCosy,
		Credentials: map[string]any{"pat": "pat"},
	}

	require.ErrorContains(t, validator.validate(context.Background(), provider), "requires qoder platform")
}

func TestValidateQoderCosyCredentialsRejectsMachineIDWithAuthDir(t *testing.T) {
	validator := &qoderCredentialValidator{}
	provider := &providercore.Record{
		Platform: capability.PlatformQoder,
		Type:     capability.ProviderTypeCosy,
		Credentials: map[string]any{
			"machine_id": "machine-1",
			"auth_dir":   "/tmp/qoder-auth",
		},
	}

	require.ErrorContains(t, validator.validate(context.Background(), provider), "pat or security_oauth_token")
}

func TestValidateQoderCosyCredentialsExchangesPAT(t *testing.T) {
	validator := &qoderCredentialValidator{}
	calls := 0
	validator.validatePAT = func(ctx context.Context, provider *providercore.Record, pat string, machine *qoder.MachineIdentity) (*qoder.AuthIdentity, error) {
		calls++
		require.Equal(t, "pat-123", pat)
		return &qoder.AuthIdentity{UID: "uid", SecurityOauthToken: "dt-token"}, nil
	}

	provider := &providercore.Record{
		Platform:    capability.PlatformQoder,
		Type:        capability.ProviderTypeCosy,
		Credentials: map[string]any{"pat": "pat-123"},
	}

	require.NoError(t, validator.validate(context.Background(), provider))
	require.Equal(t, 1, calls)
	require.Empty(t, provider.GetCredential("machine_id"))
	require.Empty(t, provider.GetCredential("machine_token"))
	require.Empty(t, provider.GetCredential("machine_type"))
}

func TestValidateQoderCosyCredentialsDefersUnchangedPATExchangeForSiteEdit(t *testing.T) {
	validator := &qoderCredentialValidator{}
	validator.validatePAT = func(context.Context, *providercore.Record, string, *qoder.MachineIdentity) (*qoder.AuthIdentity, error) {
		t.Fatal("站点编辑不应在保存阶段重新交换未变化的 PAT")
		return nil, nil
	}
	provider := &providercore.Record{
		Platform:    capability.PlatformQoder,
		Type:        capability.ProviderTypeCosy,
		Credentials: map[string]any{"site": "cn", "pat": "pat-123"},
	}

	err := validator.validateEdit(context.Background(), provider, true)

	require.NoError(t, err)
	require.Empty(t, provider.GetCredential("machine_id"))
	require.Empty(t, provider.GetCredential("machine_token"))
	require.Empty(t, provider.GetCredential("machine_type"))
}

func TestUpdateQoderPATProviderDefersCompatibilityCheckWhenSiteChanges(t *testing.T) {
	validator := &qoderCredentialValidator{}
	validator.validatePAT = func(context.Context, *providercore.Record, string, *qoder.MachineIdentity) (*qoder.AuthIdentity, error) {
		t.Fatal("切换站点应保存原 PAT，并由后续连接测试判断兼容性")
		return nil, nil
	}
	const providerID int64 = 1201
	baseRepo := &qoderValidationStore{providers: map[int64]*providercore.Record{
		providerID: {
			ID:       providerID,
			Platform: capability.PlatformQoder,
			Type:     capability.ProviderTypeCosy,
			Status:   billing.StatusActive,
			Credentials: map[string]any{
				"site": "global",
				"pat":  "pat-123",
			},
		},
	}}
	svc := newQoderValidationAdmin(baseRepo, validator)

	updated, err := svc.UpdateProvider(context.Background(), providerID, &providercore.UpdateProviderInput{
		Credentials: map[string]any{"site": "cn"},
	})

	require.NoError(t, err)
	require.Equal(t, "cn", updated.GetCredential("site"))
	require.Equal(t, "pat-123", updated.GetCredential("pat"))
	require.Empty(t, updated.GetCredential("machine_id"))
}

func TestValidateQoderCosyCredentialsRejectsBadPAT(t *testing.T) {
	validator := &qoderCredentialValidator{}
	validator.validatePAT = func(ctx context.Context, provider *providercore.Record, pat string, machine *qoder.MachineIdentity) (*qoder.AuthIdentity, error) {
		return nil, errors.New("bad pat")
	}

	provider := &providercore.Record{
		Platform:    capability.PlatformQoder,
		Type:        capability.ProviderTypeCosy,
		Credentials: map[string]any{"pat": "pat-123"},
	}

	require.ErrorContains(t, validator.validate(context.Background(), provider), "bad pat")
}

func TestValidateQoderCosyCredentialsPATUsesProviderDoer(t *testing.T) {
	validator := &qoderCredentialValidator{}
	upstream := &qoderValidationHTTPUpstreamStub{}
	proxyID := int64(9)
	provider := &providercore.Record{
		ID:          109,
		Platform:    capability.PlatformQoder,
		Type:        capability.ProviderTypeCosy,
		Concurrency: 5,
		ProxyID:     &proxyID,
		Proxy:       &egress.Proxy{Protocol: "http", Host: "proxy.example.com", Port: 8080},
		Credentials: map[string]any{"pat": "pat-123"},
	}

	validator.transport = upstream
	err := validator.validate(context.Background(), provider)

	require.NoError(t, err)
	require.Equal(t, "http://proxy.example.com:8080", upstream.proxyURL)
	require.Equal(t, int64(109), upstream.providerID)
	require.Equal(t, 5, upstream.providerConcurrency)
}

func (s *qoderValidationHTTPUpstreamStub) Do(req *http.Request, proxyURL string, providerID int64, providerConcurrency int) (*http.Response, error) {
	return s.DoWithTLS(req, proxyURL, providerID, providerConcurrency, nil)
}

func (s *qoderValidationHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, providerID int64, providerConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	s.proxyURL = proxyURL
	s.providerID = providerID
	s.providerConcurrency = providerConcurrency
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{
			"id":"user-1",
			"name":"User",
			"userType":"personal_standard",
			"securityOauthToken":"dt-from-center",
			"refreshToken":"rt-from-center"
		}`)),
		Request: req,
	}, nil
}

func (s *qoderValidationStore) Create(_ context.Context, value *providercore.Record) error {
	if s.providers == nil {
		s.providers = make(map[int64]*providercore.Record)
	}
	if value.ID == 0 {
		value.ID = int64(len(s.providers) + 1)
	}
	s.providers[value.ID] = providercore.CloneRecord(value)
	return nil
}

func (s *qoderValidationStore) GetByID(_ context.Context, id int64) (*providercore.Record, error) {
	return providercore.CloneRecord(s.providers[id]), nil
}

func (s *qoderValidationStore) Update(_ context.Context, value *providercore.Record) error {
	s.providers[value.ID] = providercore.CloneRecord(value)
	return nil
}

func newQoderValidationAdmin(store *qoderValidationStore, validator *qoderCredentialValidator) *providercore.Admin {
	return providercore.NewAdmin(store, providercore.AdminOptions{
		Creation:    providercore.CreationOptions{Now: time.Now, LoadLocation: time.LoadLocation, NewSeed: uuid.NewString},
		Credentials: validator.hooks(),
	})
}
