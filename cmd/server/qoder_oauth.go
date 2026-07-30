package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// Qoder OAuth constants
const (
	qoderDeviceTokenURL = "https://openapi.qoder.sh/api/v1/deviceToken/poll"
	qoderUserInfoURL    = "https://openapi.qoder.sh/api/v1/userinfo"
	qoderLoginURL       = "https://qoder.com/device/selectAccounts"
	qoderChatURL        = "https://api3.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation"
	qoderModelListURL   = "https://api3.qoder.sh/algo/api/v2/model/list"

	// RSA public key for COSY signing (from qodercli)
	qoderRSAPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`

	// Qoder IDE version for COSY headers
	qoderIDEVersion  = "1.1.5"
	qoderClientType  = "IDE"
	qoderMachineOS   = "linux"
	qoderMachineType = "x86_64"
	qoderDataPolicy  = "default"
	qoderLoginVersion = "1.0.0"
)

// generatePKCEPair generates PKCE verifier + S256 challenge
func generatePKCEPair() (verifier, challenge string) {
	b := make([]byte, 32)
	rand.Read(b)
	verifier = base64.RawURLEncoding.EncodeToString(b)
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])
	return
}


// qoderDeviceCodeStart starts the Qoder device flow
func qoderDeviceCodeStart(w http.ResponseWriter, r *http.Request) {
	verifier, challenge := generatePKCEPair()
	nonce := generateUUID()
	machineID := generateUUID()

	// Store in provider oauth_data
	oauthData := fmt.Sprintf(`{"verifier":"%s","nonce":"%s","machine_id":"%s","flow":"qoder"}`,
		verifier, nonce, machineID)
	db.DB.Exec("UPDATE providers SET oauth_data=? WHERE id=?", oauthData, "builtin-qoder")

	// Build verification URL
	params := url.Values{
		"challenge":        {challenge},
		"challenge_method": {"S256"},
		"machine_id":       {machineID},
		"nonce":            {nonce},
	}

	verificationURL := fmt.Sprintf("%s?%s", qoderLoginURL, params.Encode())

	writeJSON(w, map[string]interface{}{
		"verification_uri":         qoderLoginURL,
		"verification_uri_complete": verificationURL,
		"nonce":                    nonce,
		"code_verifier":            verifier,
		"expires_in":               300,
		"interval":                 2,
	})
}

// qoderDeviceCodePoll polls for the device token
func qoderDeviceCodePoll(w http.ResponseWriter, r *http.Request) {
	// Get stored oauth_data
	var oauthData string
	db.DB.QueryRow("SELECT COALESCE(oauth_data,'') FROM providers WHERE id=?", "builtin-qoder").Scan(&oauthData)

	var stored struct {
		Verifier  string `json:"verifier"`
		Nonce     string `json:"nonce"`
		MachineID string `json:"machine_id"`
	}
	json.Unmarshal([]byte(oauthData), &stored)

	if stored.Verifier == "" || stored.Nonce == "" {
		writeError(w, 400, "no pending device flow")
		return
	}

	// Poll Qoder API
	pollURL := fmt.Sprintf("%s?nonce=%s&verifier=%s&challenge_method=S256",
		qoderDeviceTokenURL,
		url.QueryEscape(stored.Nonce),
		url.QueryEscape(stored.Verifier))

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", pollURL, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Go-http-client/2.0")

	resp, err := client.Do(req)
	if err != nil {
		writeError(w, 502, "poll failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// 202/404 = still pending
	if resp.StatusCode == 202 || resp.StatusCode == 404 {
		writeJSON(w, map[string]interface{}{"status": "pending"})
		return
	}

	if resp.StatusCode != 200 {
		writeError(w, resp.StatusCode, "qoder poll error: "+string(body))
		return
	}

	var tokenResp struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
		UserID       string `json:"user_id"`
		ExpiresAt    int64  `json:"expires_at"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	json.Unmarshal(body, &tokenResp)

	if tokenResp.Token == "" {
		writeError(w, 502, "no token in response")
		return
	}

	// Calculate expiry
	expireMs := tokenResp.ExpiresAt
	if expireMs == 0 && tokenResp.ExpiresIn > 0 {
		expireMs = time.Now().UnixMilli() + tokenResp.ExpiresIn*1000
	}
	if expireMs == 0 {
		expireMs = time.Now().Add(30 * 24 * time.Hour).UnixMilli()
	}

	// Fetch user info
	userInfo := fetchQoderUserInfo(tokenResp.Token)

	// Store as connection
	connID := genID()
	_, err = db.DB.Exec(`INSERT OR REPLACE INTO provider_connections
		(id, provider_id, auth_type, name, email, access_token, refresh_token, expires_at, machine_id, test_status, is_active, created_at, updated_at)
		VALUES (?, ?, 'oauth', ?, ?, ?, ?, ?, ?, 'connected', 1, ?, ?)`,
		connID, "builtin-qoder",
		userInfo.Email, userInfo.Email,
		tokenResp.Token, tokenResp.RefreshToken,
		expireMs/1000, stored.MachineID,
		time.Now().Unix(), time.Now().Unix())

	if err != nil {
		writeError(w, 500, "failed to store connection: "+err.Error())
		return
	}

	// Clear oauth_data
	db.DB.Exec("UPDATE providers SET oauth_data='' WHERE id=?", "builtin-qoder")

	log.Printf("[PAAP] Qoder connected as %s (user: %s)", userInfo.Email, tokenResp.UserID)

	writeJSON(w, map[string]interface{}{
		"status":  "connected",
		"email":   userInfo.Email,
		"user_id": tokenResp.UserID,
	})
}

// qoderUserInfo holds user profile info
type qoderUserInfo struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// fetchQoderUserInfo fetches user profile from Qoder API
func fetchQoderUserInfo(accessToken string) qoderUserInfo {
	req, _ := http.NewRequest("GET", qoderUserInfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Go-http-client/2.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return qoderUserInfo{}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var info struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	json.Unmarshal(body, &info)

	return qoderUserInfo{
		Name:  strings.TrimSpace(info.Name + " " + info.Username),
		Email: strings.TrimSpace(info.Email),
	}
}

// qoderStatus returns current Qoder connection status
func qoderStatus(w http.ResponseWriter, r *http.Request) {
	var connID, email string
	var isActive int
	err := db.DB.QueryRow(`SELECT id, email, is_active FROM provider_connections 
		WHERE provider_id='builtin-qoder' AND is_active=1 ORDER BY created_at DESC LIMIT 1`).Scan(&connID, &email, &isActive)

	if err != nil {
		writeJSON(w, map[string]interface{}{"connected": false})
		return
	}

	writeJSON(w, map[string]interface{}{
		"connected":   true,
		"connection_id": connID,
		"email":       email,
	})
}

// qoderDisconnect disconnects Qoder
func qoderDisconnect(w http.ResponseWriter, r *http.Request) {
	db.DB.Exec("UPDATE provider_connections SET is_active=0 WHERE provider_id='builtin-qoder'")
	writeJSON(w, map[string]interface{}{"status": "disconnected"})
}

// getQoderToken gets a valid Qoder token, refreshing if needed
func getQoderToken() (string, error) {
	var token string
	var expiresAt int64
	var refreshToken string
	var connID string

	err := db.DB.QueryRow(`SELECT id, access_token, COALESCE(refresh_token,''), COALESCE(expires_at,0) 
		FROM provider_connections WHERE provider_id='builtin-qoder' AND is_active=1 
		ORDER BY created_at DESC LIMIT 1`).Scan(&connID, &token, &refreshToken, &expiresAt)

	if err != nil {
		return "", fmt.Errorf("no qoder connection")
	}

	// Check if expired (with 5 min buffer)
	if expiresAt > 0 && time.Now().Unix() > expiresAt-300 {
		// Try to refresh
		if refreshToken != "" {
			newToken, newRefresh, newExpiry, err := refreshQoderToken(refreshToken)
			if err == nil {
				db.DB.Exec("UPDATE provider_connections SET access_token=?, refresh_token=?, expires_at=? WHERE id=?",
					newToken, newRefresh, newExpiry, connID)
				return newToken, nil
			}
		}
		return "", fmt.Errorf("qoder token expired, re-login required")
	}

	return token, nil
}

// refreshQoderToken refreshes a Qoder token
func refreshQoderToken(refreshToken string) (newAccess, newRefresh string, expiresAt int64, err error) {
	// Qoder refresh endpoint returns 403 for most flows
	// Token valid ~30 days, user needs to re-login when expired
	return "", "", 0, fmt.Errorf("refresh not supported")
}
