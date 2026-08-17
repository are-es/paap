package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// grok-cli OAuth constants
const (
	grokClientID         = "b1a00492-073a-47ea-816f-4c329264a828"
	grokDeviceCodeURL    = "https://auth.x.ai/oauth2/device/code"
	grokTokenURL         = "https://auth.x.ai/oauth2/token"
	grokUserURL          = "https://cli-chat-proxy.grok.com/v1/user"
	grokChatURL          = "https://cli-chat-proxy.grok.com/v1"
	grokScope            = "openid profile email offline_access grok-cli:access api:access conversations:read conversations:write"
	grokUserAgent        = "grok-pager/0.2.93 grok-shell/0.2.93 (linux; x86_64)"
	grokClientIdentifier = "grok-pager"
	grokClientVersion    = "0.2.93"
)

// OpenAI Codex OAuth constants
const (
	codexClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexIssuer        = "https://auth.openai.com"
	codexDeviceCodeURL = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	codexPollURL       = "https://auth.openai.com/api/accounts/deviceauth/token"
	codexTokenURL      = "https://auth.openai.com/oauth/token"
	codexDeviceURL     = "https://auth.openai.com/codex/device"
	codexRedirectURI   = "https://auth.openai.com/deviceauth/callback"
	codexBaseURL       = "https://chatgpt.com/backend-api/codex"
)

func marshalCodexOAuthData(deviceAuthID, userCode, expiresAt string) (string, error) {
	data, err := json.Marshal(struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		ExpiresAt    string `json:"expires_at"`
	}{deviceAuthID, userCode, expiresAt})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Anigravity Google OAuth constants
const (
	antigravityAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	antigravityTokenURL    = "https://oauth2.googleapis.com/token"
	antigravityUserInfoURL = "https://www.googleapis.com/oauth2/v1/userinfo"
	antigravityScopes      = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs"
	antigravityLoadURL     = "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	antigravityOnboardURL  = "https://daily-cloudcode-pa.googleapis.com/v1internal:onboardUser"
	antigravityUserAgent   = "antigravity/ide/2.1.1 linux/amd64"
	antigravityApiClient   = "google-cloud-sdk vscode_cloudshelleditor/0.1"
)

func xorDecode(h string, k byte) string {
	b, _ := hex.DecodeString(h)
	for i := range b {
		b[i] ^= k
	}
	return string(b)
}

func getAnigravityClientID() string {
	if v := os.Getenv("PAAP_ANTIGRAVITY_CLIENT_ID"); v != "" {
		return v
	}
	return xorDecode("6d6c6b6d6c6c6a6c6a6c69656d712831342f2f35326e346e6d303f2e396e6f692a283330333634683b686c6f392c723d2c2c2f723b33333b3039292f392e3f333228393228723f3331", 0x5c)
}

func getAnigravityClientSecret() string {
	if v := os.Getenv("PAAP_ANTIGRAVITY_SECRET"); v != "" {
		return v
	}
	return xorDecode("1b131f0f0c04711769641a0b0e68646a103810166d31101e642f041f68266a2d181d3a", 0x5c)
}

// ── Device Code Flow ──────────────────────────────────────────

func oauthDeviceCodeStart(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/oauth/")
	providerID := strings.TrimSuffix(trimmed, "/device-code")

	if !strings.HasSuffix(providerID, "grok-cli") {
		writeError(w, 400, "unsupported OAuth provider: "+providerID)
		return
	}

	// Generate PKCE code verifier
	verifierBytes := make([]byte, 32)
	rand.Read(verifierBytes)
	codeVerifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	// S256 code challenge
	hash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(hash[:])

	// Request device code from xAI
	form := url.Values{
		"client_id":             {grokClientID},
		"scope":                 {grokScope},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}

	resp, err := http.PostForm(grokDeviceCodeURL, form)
	if err != nil {
		writeError(w, 502, "failed to request device code: "+err.Error())
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		writeError(w, resp.StatusCode, fmt.Sprintf("xAI returned %d: %s", resp.StatusCode, string(body)))
		return
	}

	var deviceResp struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &deviceResp); err != nil {
		writeError(w, 502, "invalid response from xAI")
		return
	}

	// Store device_code + code_verifier temporarily
	db.DB.Exec(`UPDATE providers SET oauth_data = ? WHERE id = ?`,
		fmt.Sprintf(`{"device_code":"%s","code_verifier":"%s","expires_at":"%s"}`,
			deviceResp.DeviceCode,
			codeVerifier,
			time.Now().Add(time.Duration(deviceResp.ExpiresIn)*time.Second).UTC().Format(time.RFC3339)),
		providerID,
	)

	writeJSON(w, map[string]interface{}{
		"device_code":               deviceResp.DeviceCode,
		"user_code":                 deviceResp.UserCode,
		"verification_uri":          deviceResp.VerificationURI,
		"verification_uri_complete": deviceResp.VerificationURIComplete,
		"expires_in":                deviceResp.ExpiresIn,
		"interval":                  deviceResp.Interval,
	})
}

func oauthDeviceCodePoll(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/oauth/")
	providerID := strings.TrimSuffix(trimmed, "/poll")

	if !strings.HasSuffix(providerID, "grok-cli") {
		writeError(w, 400, "unsupported OAuth provider: "+providerID)
		return
	}

	// Load stored device_code + code_verifier
	var oauthData string
	err := db.DB.QueryRow("SELECT COALESCE(oauth_data,'') FROM providers WHERE id=?", providerID).Scan(&oauthData)
	if err != nil || oauthData == "" {
		writeError(w, 400, "no pending device code flow. Start one first.")
		return
	}

	var stored struct {
		DeviceCode   string `json:"device_code"`
		CodeVerifier string `json:"code_verifier"`
		ExpiresAt    string `json:"expires_at"`
	}
	json.Unmarshal([]byte(oauthData), &stored)

	if stored.DeviceCode == "" {
		writeError(w, 400, "no pending device code flow")
		return
	}

	// Check expiry
	if exp, err := time.Parse(time.RFC3339, stored.ExpiresAt); err == nil && time.Now().After(exp) {
		db.DB.Exec("UPDATE providers SET oauth_data='' WHERE id=?", providerID)
		writeError(w, 408, "device code expired. Start a new flow.")
		return
	}

	// Poll xAI token endpoint
	form := url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
		"client_id":     {grokClientID},
		"device_code":   {stored.DeviceCode},
		"code_verifier": {stored.CodeVerifier},
	}

	resp, err := http.PostForm(grokTokenURL, form)
	if err != nil {
		writeError(w, 502, "failed to poll token: "+err.Error())
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	json.Unmarshal(body, &tokenResp)

	if tokenResp.Error != "" {
		if tokenResp.Error == "authorization_pending" {
			writeJSON(w, map[string]interface{}{"status": "pending", "error": "waiting for user authorization"})
			return
		}
		if tokenResp.Error == "slow_down" {
			writeJSON(w, map[string]interface{}{"status": "slow_down", "error": tokenResp.ErrorDesc})
			return
		}
		writeError(w, 400, tokenResp.Error+": "+tokenResp.ErrorDesc)
		return
	}

	// Success! Get user info (email)
	email := fetchGrokEmail(tokenResp.AccessToken)
	if email == "" {
		email = fmt.Sprintf("grok-%d", time.Now().Unix())
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)

	// Check if this email already exists as a key for this provider
	var existingID string
	err = db.DB.QueryRow("SELECT id FROM api_keys WHERE provider_id=? AND name=? AND key_type='oauth'", providerID, email).Scan(&existingID)
	if err == nil {
		// Update existing key
		_, err = db.DB.Exec(`UPDATE api_keys SET
			key_encrypted=?, oauth_refresh_token=?, oauth_expires_at=?, is_active=1
			WHERE id=?`,
			tokenResp.AccessToken, tokenResp.RefreshToken, expiresAt, existingID)
	} else {
		// Insert new key
		keyID := genID()
		_, err = db.DB.Exec(`INSERT INTO api_keys
			(id, provider_id, name, key_encrypted, key_type, oauth_refresh_token, oauth_expires_at, is_active)
			VALUES (?, ?, ?, ?, 'oauth', ?, ?, 1)`,
			keyID, providerID, email, tokenResp.AccessToken, tokenResp.RefreshToken, expiresAt)
	}

	// Clear pending device code data
	db.DB.Exec("UPDATE providers SET oauth_data='' WHERE id=?", providerID)

	if err != nil {
		writeError(w, 500, "failed to store key: "+err.Error())
		return
	}

	log.Printf("[PAAP] OAuth: %s connected as %s (expires: %s)", providerID, email, expiresAt)

	writeJSON(w, map[string]interface{}{
		"status":     "connected",
		"email":      email,
		"expires_at": expiresAt,
	})
}

// fetchGrokEmail gets the user email from grok-cli user endpoint
func fetchGrokEmail(accessToken string) string {
	req, _ := http.NewRequest("GET", grokUserURL+"?include=subscription", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", grokUserAgent)
	req.Header.Set("x-xai-token-auth", "xai-grok-cli")
	req.Header.Set("x-grok-client-identifier", grokClientIdentifier)
	req.Header.Set("x-grok-client-version", grokClientVersion)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var userResp struct {
		Email string `json:"email"`
	}
	json.Unmarshal(body, &userResp)
	return userResp.Email
}

func oauthStatus(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/oauth/")
	providerID := strings.TrimSuffix(trimmed, "/status")

	// Count OAuth keys for this provider
	var count int
	db.DB.QueryRow("SELECT COUNT(*) FROM api_keys WHERE provider_id=? AND key_type='oauth' AND is_active=1", providerID).Scan(&count)

	// Get provider auth_type
	var authType string
	db.DB.QueryRow("SELECT COALESCE(auth_type,'apikey') FROM providers WHERE id=?", providerID).Scan(&authType)

	writeJSON(w, map[string]interface{}{
		"provider_id":    providerID,
		"auth_type":      authType,
		"connected_keys": count,
	})
}

func oauthDisconnect(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/oauth/")
	parts := strings.SplitN(strings.TrimSuffix(trimmed, "/disconnect"), "/", 2)

	providerID := parts[0]
	var keyID string
	if len(parts) > 1 {
		keyID = parts[1]
	}

	if keyID != "" {
		// Disconnect specific key
		db.DB.Exec("DELETE FROM api_keys WHERE id=? AND provider_id=? AND key_type='oauth'", keyID, providerID)
	} else {
		// Disconnect all OAuth keys for provider
		db.DB.Exec("DELETE FROM api_keys WHERE provider_id=? AND key_type='oauth'", providerID)
	}

	writeJSON(w, map[string]interface{}{"status": "disconnected", "provider": providerID})
}

// ── Token Refresh ──────────────────────────────────────────

// RefreshOAuthKey refreshes a single OAuth key's token.
// Detects provider (Grok vs Codex) and uses correct client_id/URL.
func RefreshOAuthKey(keyID string) (newAccess, newRefresh, newExpires string, err error) {
	var refreshToken, providerID string
	err = db.DB.QueryRow("SELECT oauth_refresh_token, provider_id FROM api_keys WHERE id=? AND key_type='oauth'", keyID).Scan(&refreshToken, &providerID)
	if err != nil || refreshToken == "" {
		return "", "", "", fmt.Errorf("no refresh token for key %s", keyID)
	}

	// Determine client ID and token URL based on provider
	clientID := grokClientID
	tokenURL := grokTokenURL
	if strings.Contains(providerID, "openai-codex") || strings.Contains(providerID, "codex") {
		clientID = codexClientID
		tokenURL = codexTokenURL
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	}

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", "", fmt.Errorf("refresh request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if isCodexOAuthProviderID(providerID) {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "codex_cli_rs/0.0.0")
	}
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("refresh failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	json.Unmarshal(body, &tokenResp)

	if resp.StatusCode == 429 {
		return "", "", "", fmt.Errorf("rate limited (429) on token refresh — retry later")
	}
	if tokenResp.Error != "" {
		return "", "", "", fmt.Errorf("refresh error: %s %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	newRefresh = tokenResp.RefreshToken
	if newRefresh == "" {
		newRefresh = refreshToken
	}
	newExpires = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)

	db.DB.Exec("UPDATE api_keys SET key_encrypted=?, oauth_refresh_token=?, oauth_expires_at=? WHERE id=?",
		tokenResp.AccessToken, newRefresh, newExpires, keyID)

	log.Printf("[PAAP] OAuth: key %s refreshed (expires: %s)", keyID, newExpires)
	return tokenResp.AccessToken, newRefresh, newExpires, nil
}

// GetOAuthKeyValue returns a valid access token for an OAuth key, refreshing if needed
func GetOAuthKeyValue(keyID, currentToken, expiresAt string) (string, error) {
	// Check if expired (5min buffer)
	if expiresAt != "" {
		if exp, err := time.Parse(time.RFC3339, expiresAt); err == nil {
			if time.Now().Add(5 * time.Minute).After(exp) {
				newToken, _, _, err := RefreshOAuthKey(keyID)
				if err != nil {
					return "", fmt.Errorf("token expired and refresh failed: %v", err)
				}
				return newToken, nil
			}
		}
	}
	return currentToken, nil
}

// ── Anigravity Google OAuth ──────────────────────────────────

// oauthAnigravityStart redirects user to Google OAuth
func oauthAnigravityStart(w http.ResponseWriter, r *http.Request) {
	// Generate state parameter for CSRF protection
	stateBytes := make([]byte, 16)
	rand.Read(stateBytes)
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	// Store state in provider oauth_data
	db.DB.Exec(`UPDATE providers SET oauth_data = ? WHERE id = ?`,
		fmt.Sprintf(`{"state":"%s","flow":"google"}`, state), "builtin-anigravity")

	redirectURI := fmt.Sprintf("http://%s/api/oauth/anigravity/callback", r.Host)

	params := url.Values{
		"client_id":     {getAnigravityClientID()},
		"response_type": {"code"},
		"redirect_uri":  {redirectURI},
		"scope":         {antigravityScopes},
		"state":         {state},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
	}

	authURL := fmt.Sprintf("%s?%s", antigravityAuthURL, params.Encode())
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// oauthAnigravityCallback handles Google OAuth callback
func oauthAnigravityCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	if errorParam != "" {
		writeError(w, 400, "Google OAuth error: "+errorParam)
		return
	}

	if code == "" {
		writeError(w, 400, "missing code parameter")
		return
	}

	// Verify state
	var oauthData string
	db.DB.QueryRow("SELECT COALESCE(oauth_data,'') FROM providers WHERE id=?", "builtin-anigravity").Scan(&oauthData)
	var stored struct {
		State string `json:"state"`
	}
	json.Unmarshal([]byte(oauthData), &stored)
	if stored.State != "" && stored.State != state {
		writeError(w, 403, "state mismatch — possible CSRF")
		return
	}

	// Exchange code for tokens
	redirectURI := fmt.Sprintf("http://%s/api/oauth/anigravity/callback", r.Host)
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {getAnigravityClientID()},
		"client_secret": {getAnigravityClientSecret()},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}

	resp, err := http.PostForm(antigravityTokenURL, form)
	if err != nil {
		writeError(w, 502, "token exchange failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		writeError(w, resp.StatusCode, "Google token error: "+string(body))
		return
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	json.Unmarshal(body, &tokenResp)

	// Get user email
	email := fetchGoogleEmail(tokenResp.AccessToken)
	if email == "" {
		email = fmt.Sprintf("anigravity-%d", time.Now().Unix())
	}

	// Load Code Assist to get project ID
	projectID, tierID := loadCodeAssist(tokenResp.AccessToken)

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Unix()

	// Store as connection
	connID := genID()
	_, err = db.DB.Exec(`INSERT INTO provider_connections
		(id, provider_id, auth_type, name, email, access_token, refresh_token, expires_at, project_id, test_status, is_active, created_at, updated_at)
		VALUES (?, ?, 'oauth', ?, ?, ?, ?, ?, ?, 'connected', 1, ?, ?)`,
		connID, "builtin-anigravity", email, email,
		tokenResp.AccessToken, tokenResp.RefreshToken, expiresAt, projectID,
		time.Now().Unix(), time.Now().Unix())

	if err != nil {
		writeError(w, 500, "failed to store connection: "+err.Error())
		return
	}

	// Clear state
	db.DB.Exec("UPDATE providers SET oauth_data='' WHERE id=?", "builtin-anigravity")

	log.Printf("[PAAP] Anigravity connected as %s (project: %s, tier: %s)", email, projectID, tierID)

	// Redirect back to provider setup page
	http.Redirect(w, r, "/providers/setup?id=builtin-anigravity", http.StatusTemporaryRedirect)
}

// fetchGoogleEmail gets user email from Google userinfo endpoint
func fetchGoogleEmail(accessToken string) string {
	req, _ := http.NewRequest("GET", antigravityUserInfoURL+"?alt=json", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var userResp struct {
		Email string `json:"email"`
	}
	json.Unmarshal(body, &userResp)
	return userResp.Email
}

// loadCodeAssist loads code assist project and tier, calling onboardUser if needed
func loadCodeAssist(accessToken string) (projectID, tierID string) {
	tierID = "legacy-tier"

	metadata := `{"ideType":9,"platform":2,"pluginType":2}`
	req, _ := http.NewRequest("POST", "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist", strings.NewReader(`{"metadata":`+metadata+`}`))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravityUserAgent)
	req.Header.Set("X-Goog-Api-Client", antigravityApiClient)
	req.Header.Set("Client-Metadata", metadata)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PAAP] Anigravity loadCodeAssist failed: %v", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		CloudAICompanionProject interface{} `json:"cloudaicompanionProject"`
		AllowedTiers            []struct {
			ID        string `json:"id"`
			IsDefault bool   `json:"isDefault"`
		} `json:"allowedTiers"`
	}
	json.Unmarshal(body, &result)

	if result.CloudAICompanionProject != nil {
		switch v := result.CloudAICompanionProject.(type) {
		case string:
			projectID = v
		case map[string]interface{}:
			if id, ok := v["id"]; ok && id != nil {
				projectID = fmt.Sprintf("%v", id)
			}
		default:
			projectID = fmt.Sprintf("%v", v)
		}
	}

	for _, tier := range result.AllowedTiers {
		if tier.IsDefault && tier.ID != "" {
			tierID = tier.ID
			break
		}
	}
	if tierID == "legacy-tier" && len(result.AllowedTiers) > 0 && result.AllowedTiers[0].ID != "" {
		tierID = result.AllowedTiers[0].ID
	}

	// If projectID is still empty, call onboardUser to provision project
	if projectID == "" && tierID != "" && tierID != "legacy-tier" {
		log.Printf("[PAAP] Anigravity projectID empty, attempting onboardUser with tier: %s", tierID)
		onboardPayload := fmt.Sprintf(`{"metadata":%s,"tierId":"%s"}`, metadata, tierID)
		onboardReq, _ := http.NewRequest("POST", antigravityOnboardURL, strings.NewReader(onboardPayload))
		onboardReq.Header.Set("Authorization", "Bearer "+accessToken)
		onboardReq.Header.Set("Content-Type", "application/json")
		onboardReq.Header.Set("User-Agent", antigravityUserAgent)
		onboardReq.Header.Set("X-Goog-Api-Client", antigravityApiClient)
		onboardReq.Header.Set("Client-Metadata", metadata)

		onboardResp, err := client.Do(onboardReq)
		if err == nil {
			defer onboardResp.Body.Close()
			onboardBody, _ := io.ReadAll(onboardResp.Body)
			log.Printf("[PAAP] Anigravity onboardUser response: %s", string(onboardBody))

			var onboardResult struct {
				Response struct {
					CloudAICompanionProject struct {
						ID interface{} `json:"id"`
					} `json:"cloudaicompanionProject"`
				} `json:"response"`
				CloudAICompanionProject struct {
					ID interface{} `json:"id"`
				} `json:"cloudaicompanionProject"`
			}
			json.Unmarshal(onboardBody, &onboardResult)

			if onboardResult.Response.CloudAICompanionProject.ID != nil {
				projectID = fmt.Sprintf("%v", onboardResult.Response.CloudAICompanionProject.ID)
			} else if onboardResult.CloudAICompanionProject.ID != nil {
				projectID = fmt.Sprintf("%v", onboardResult.CloudAICompanionProject.ID)
			}

			// If still empty after onboard, query loadCodeAssist once more
			if projectID == "" {
				req2, _ := http.NewRequest("POST", antigravityLoadURL, strings.NewReader(`{"metadata":`+metadata+`}`))
				req2.Header.Set("Authorization", "Bearer "+accessToken)
				req2.Header.Set("Content-Type", "application/json")
				req2.Header.Set("User-Agent", antigravityUserAgent)
				req2.Header.Set("X-Goog-Api-Client", antigravityApiClient)
				req2.Header.Set("Client-Metadata", metadata)

				resp2, err2 := client.Do(req2)
				if err2 == nil {
					defer resp2.Body.Close()
					body2, _ := io.ReadAll(resp2.Body)
					var result2 struct {
						CloudAICompanionProject struct {
							ID interface{} `json:"id"`
						} `json:"cloudaicompanionProject"`
					}
					json.Unmarshal(body2, &result2)
					if result2.CloudAICompanionProject.ID != nil {
						projectID = fmt.Sprintf("%v", result2.CloudAICompanionProject.ID)
					}
				}
			}
		} else {
			log.Printf("[PAAP] Anigravity onboardUser error: %v", err)
		}
	}

	return
}

// RefreshAnigravityToken refreshes a Google OAuth token
func RefreshAnigravityToken(refreshToken string) (newAccess, newRefresh string, expiresIn int, err error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {getAnigravityClientID()},
		"client_secret": {getAnigravityClientSecret()},
		"refresh_token": {refreshToken},
	}

	resp, err := http.PostForm(antigravityTokenURL, form)
	if err != nil {
		return "", "", 0, fmt.Errorf("refresh failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", "", 0, fmt.Errorf("refresh error %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	json.Unmarshal(body, &tokenResp)

	newRefresh = tokenResp.RefreshToken
	if newRefresh == "" {
		newRefresh = refreshToken
	}

	return tokenResp.AccessToken, newRefresh, tokenResp.ExpiresIn, nil
}

// ensureAnigravityToken returns a valid access token, automatically refreshing it with Google if expired
func ensureAnigravityToken(connID, connToken, refreshToken string, expiresAt int64) (string, error) {
	// If token is still valid with a 60-second safety margin, use it directly
	if connToken != "" && expiresAt > time.Now().Unix()+60 {
		return connToken, nil
	}
	if refreshToken == "" {
		if connToken != "" {
			return connToken, nil
		}
		return "", fmt.Errorf("no refresh token available — please reconnect via Google OAuth")
	}
	log.Printf("[PAAP] Anigravity access token expired for connection %s — refreshing with Google OAuth...", connID[:min(8, len(connID))])
	newAccess, newRefresh, expiresIn, err := RefreshAnigravityToken(refreshToken)
	if err != nil {
		db.DB.Exec("UPDATE provider_connections SET is_active=0 WHERE id=?", connID)
		return "", fmt.Errorf("refresh token failed: %v", err)
	}
	newExpires := time.Now().Add(time.Duration(expiresIn) * time.Second).Unix()
	db.DB.Exec("UPDATE provider_connections SET access_token=?, refresh_token=?, expires_at=?, updated_at=? WHERE id=?",
		newAccess, newRefresh, newExpires, time.Now().Unix(), connID)
	log.Printf("[PAAP] Anigravity access token refreshed successfully for %s (valid for %ds)", connID[:min(8, len(connID))], expiresIn)
	return newAccess, nil
}

// ── OpenAI Codex Device Code Flow ──────────────────────────────────────────

func oauthCodexDeviceCodeStart(w http.ResponseWriter, r *http.Request) {
	// Request device code from OpenAI
	resp, err := http.Post(codexDeviceCodeURL, "application/json",
		strings.NewReader(fmt.Sprintf(`{"client_id":"%s"}`, codexClientID)))
	if err != nil {
		writeError(w, 502, "failed to request device code: "+err.Error())
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 429 {
		writeError(w, 429, "OpenAI is rate-limiting Codex login. Wait a minute and try again.")
		return
	}
	if resp.StatusCode != 200 {
		writeError(w, resp.StatusCode, fmt.Sprintf("OpenAI returned %d: %s", resp.StatusCode, string(body)))
		return
	}

	var deviceResp struct {
		UserCode     string `json:"user_code"`
		DeviceAuthID string `json:"device_auth_id"`
		Interval     string `json:"interval"`
		ExpiresAt    string `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &deviceResp); err != nil {
		writeError(w, 502, "invalid response from OpenAI: "+err.Error())
		return
	}

	interval := 5
	fmt.Sscanf(deviceResp.Interval, "%d", &interval)

	// Store device_auth_id temporarily.
	oauthData, err := marshalCodexOAuthData(deviceResp.DeviceAuthID, deviceResp.UserCode, deviceResp.ExpiresAt)
	if err != nil {
		writeError(w, 500, "failed to encode device code state")
		return
	}
	if _, err := db.DB.Exec(`UPDATE providers SET oauth_data = ? WHERE id = ?`, oauthData, "builtin-openai-codex"); err != nil {
		writeError(w, 500, "failed to store device code state")
		return
	}

	writeJSON(w, map[string]interface{}{
		"device_auth_id":   deviceResp.DeviceAuthID,
		"user_code":        deviceResp.UserCode,
		"verification_uri": codexDeviceURL,
		"interval":         interval,
	})
}

func oauthCodexDeviceCodePoll(w http.ResponseWriter, r *http.Request) {
	// Read stored device_auth_id
	var oauthData string
	db.DB.QueryRow("SELECT COALESCE(oauth_data,'') FROM providers WHERE id=?", "builtin-openai-codex").Scan(&oauthData)
	var stored struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		ExpiresAt    string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(oauthData), &stored); err != nil {
		writeError(w, 500, "invalid stored device code state")
		return
	}

	if stored.DeviceAuthID == "" {
		writeError(w, 400, "no pending device code flow")
		return
	}

	// Check expiry
	if stored.ExpiresAt != "" {
		if exp, err := time.Parse(time.RFC3339, stored.ExpiresAt); err == nil && time.Now().After(exp) {
			db.DB.Exec("UPDATE providers SET oauth_data='' WHERE id=?", "builtin-openai-codex")
			writeError(w, 408, "device code expired. Start a new flow.")
			return
		}
	}

	// Poll for authorization code.
	pollBody, err := json.Marshal(map[string]string{
		"device_auth_id": stored.DeviceAuthID,
		"user_code":      stored.UserCode,
	})
	if err != nil {
		writeError(w, 500, "failed to encode poll request")
		return
	}
	pollResp, err := http.Post(codexPollURL, "application/json", bytes.NewReader(pollBody))
	if err != nil {
		writeError(w, 502, "poll failed: "+err.Error())
		return
	}
	defer pollResp.Body.Close()

	body, _ := io.ReadAll(pollResp.Body)

	if pollResp.StatusCode == 403 || pollResp.StatusCode == 404 {
		writeJSON(w, map[string]interface{}{"status": "pending"})
		return
	}
	if pollResp.StatusCode != 200 {
		writeError(w, pollResp.StatusCode, fmt.Sprintf("OpenAI poll returned %d: %s", pollResp.StatusCode, string(body)))
		return
	}

	var pollData struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if err := json.Unmarshal(body, &pollData); err != nil {
		writeError(w, 502, "invalid poll response")
		return
	}

	if pollData.AuthorizationCode == "" {
		writeJSON(w, map[string]interface{}{"status": "pending"})
		return
	}
	if pollData.CodeVerifier == "" {
		writeError(w, 502, "OpenAI poll response missing code_verifier")
		return
	}

	// Exchange authorization code for tokens
	tokenReq, err := newCodexTokenExchangeRequest(pollData.AuthorizationCode, pollData.CodeVerifier)
	if err != nil {
		writeError(w, 500, "failed to create token exchange request: "+err.Error())
		return
	}
	tokenClient := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	tokenResp, err := tokenClient.Do(tokenReq)
	if err != nil {
		writeError(w, 502, "token exchange failed: "+err.Error())
		return
	}
	defer tokenResp.Body.Close()

	tokenBody, _ := io.ReadAll(tokenResp.Body)
	if tokenResp.StatusCode != 200 {
		writeError(w, tokenResp.StatusCode, fmt.Sprintf("token exchange returned %d: %s", tokenResp.StatusCode, string(tokenBody)))
		return
	}

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
	}
	json.Unmarshal(tokenBody, &tokenData)

	if tokenData.AccessToken == "" {
		writeError(w, 500, "no access_token in response")
		return
	}

	// Compute expiry time
	expiresAt := ""
	if tokenData.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenData.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}

	// Store in provider_connections — always INSERT (multi-account support)
	connName := extractCodexEmail(tokenData.AccessToken)
	if connName == "" {
		connName = "openai-codex"
	}
	connID := genID()
	now := time.Now().Unix()
	db.DB.Exec(`INSERT INTO provider_connections
		(id, provider_id, auth_type, name, email, access_token, refresh_token, expires_at, test_status, is_active, created_at, updated_at)
		VALUES (?, ?, 'oauth', ?, ?, ?, ?, ?, 'connected', 1, ?, ?)`,
		connID, "builtin-openai-codex", connName, connName, tokenData.AccessToken, tokenData.RefreshToken, expiresAt, now, now)

	db.DB.Exec("UPDATE providers SET oauth_data='' WHERE id=?", "builtin-openai-codex")

	log.Printf("[PAAP] OpenAI Codex OAuth: connected (token len=%d)", len(tokenData.AccessToken))

	writeJSON(w, map[string]interface{}{
		"status":     "connected",
		"expires_in": tokenData.ExpiresIn,
	})
}

func newCodexTokenExchangeRequest(authorizationCode, codeVerifier string) (*http.Request, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authorizationCode},
		"redirect_uri":  {codexRedirectURI},
		"client_id":     {codexClientID},
		"code_verifier": {codeVerifier},
	}
	req, err := http.NewRequest(http.MethodPost, codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codex_cli_rs/0.0.0")
	return req, nil
}

// ── Route Registration ──────────────────────────────────────

func isCodexOAuthProviderID(providerID string) bool {
	return providerID == "openai-codex" || providerID == "builtin-openai-codex"
}

// refreshCodexConnection refreshes a Codex OAuth connection if the token is expired.
// Returns the (possibly refreshed) access token. If not expired, returns currentToken unchanged.
func refreshCodexConnection(connID, currentToken string, expiresAtUnix int64) string {
	if expiresAtUnix == 0 {
		return currentToken
	}
	// 5-minute buffer
	if time.Now().Unix() < expiresAtUnix-300 {
		return currentToken
	}
	var refreshToken string
	db.DB.QueryRow("SELECT COALESCE(refresh_token,'') FROM provider_connections WHERE id=?", connID).Scan(&refreshToken)
	if refreshToken == "" {
		return currentToken // can't refresh, use as-is
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {codexClientID},
	}
	req, err := http.NewRequest(http.MethodPost, codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return currentToken
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codex_cli_rs/0.0.0")
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return currentToken
	}
	defer resp.Body.Close()
	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	json.NewDecoder(resp.Body).Decode(&tokenData)
	if tokenData.AccessToken == "" {
		return currentToken
	}
	newRefresh := tokenData.RefreshToken
	if newRefresh == "" {
		newRefresh = refreshToken
	}
	newExpires := time.Now().Add(time.Duration(tokenData.ExpiresIn) * time.Second).Unix()
	db.DB.Exec("UPDATE provider_connections SET access_token=?, refresh_token=?, expires_at=?, updated_at=? WHERE id=?",
		tokenData.AccessToken, newRefresh, newExpires, time.Now().Unix(), connID)
	log.Printf("[PAAP] Codex connection %s refreshed (expires: %d)", connID[:8], newExpires)
	return tokenData.AccessToken
}

func oauthRoutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/oauth/")

	// Anigravity Google OAuth
	if trimmed == "anigravity/start" && r.Method == "GET" {
		oauthAnigravityStart(w, r)
		return
	}
	if trimmed == "anigravity/callback" && r.Method == "GET" {
		oauthAnigravityCallback(w, r)
		return
	}

	// OpenAI Codex device code flow
	providerID := strings.TrimSuffix(trimmed, "/device-code")
	if isCodexOAuthProviderID(providerID) && r.Method == "POST" {
		oauthCodexDeviceCodeStart(w, r)
		return
	}
	providerID = strings.TrimSuffix(trimmed, "/poll")
	if isCodexOAuthProviderID(providerID) && r.Method == "POST" {
		oauthCodexDeviceCodePoll(w, r)
		return
	}

	// Grok CLI device code flow
	if strings.HasSuffix(trimmed, "/device-code") && r.Method == "POST" {
		oauthDeviceCodeStart(w, r)
		return
	}
	if strings.HasSuffix(trimmed, "/poll") && r.Method == "POST" {
		oauthDeviceCodePoll(w, r)
		return
	}
	if strings.HasSuffix(trimmed, "/status") && r.Method == "GET" {
		oauthStatus(w, r)
		return
	}
	if strings.Contains(trimmed, "/disconnect") && r.Method == "POST" {
		oauthDisconnect(w, r)
		return
	}

	writeError(w, 404, "not found")
}
