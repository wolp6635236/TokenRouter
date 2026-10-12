package anthropic

// OAuthTokenResponse represents the token response from OAuth provider.
type OAuthTokenResponse struct {
	AccessToken  string             `json:"access_token"`
	TokenType    string             `json:"token_type"`
	ExpiresIn    int64              `json:"expires_in"`
	RefreshToken string             `json:"refresh_token,omitempty"`
	Scope        string             `json:"scope,omitempty"`
	Organization *OAuthOrgInfo      `json:"organization,omitempty"`
	Provider     *OAuthProviderInfo `json:"account,omitempty"`
}

// OAuthOrgInfo represents organization info from OAuth response.
type OAuthOrgInfo struct {
	UUID string `json:"uuid"`
}

// OAuthProviderInfo represents provider info from OAuth response.
type OAuthProviderInfo struct {
	UUID         string `json:"uuid"`
	EmailAddress string `json:"email_address"`
}
