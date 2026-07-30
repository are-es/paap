package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Qoder body encoding - WAF bypass scheme
// Algorithm:
// 1. base64-encode the plaintext bytes
// 2. Rearrange: split into thirds, reorder as [tail][mid][head]
// 3. Substitute each character via a custom alphabet mapping

const (
	qoderStdAlphabet    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	qoderCustomAlphabet = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"
)

var qoderS2C [128]int16

func init() {
	for i := range qoderS2C {
		qoderS2C[i] = -1
	}
	for i := 0; i < 64; i++ {
		qoderS2C[qoderStdAlphabet[i]] = int16(qoderCustomAlphabet[i])
	}
	qoderS2C['='] = '$'
}

// qoderEncodeBody encodes plaintext using Qoder's WAF-bypass scheme
func qoderEncodeBody(plaintext []byte) string {
	std := base64.StdEncoding.EncodeToString(plaintext)
	n := len(std)
	a := n / 3

	// Rearrange: [tail][mid][head]
	rearranged := std[n-a:] + std[a:n-a] + std[:a]

	// Substitute characters
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		c := rearranged[i]
		if c < 128 && qoderS2C[c] >= 0 {
			out[i] = byte(qoderS2C[c])
		} else {
			out[i] = c
		}
	}
	return string(out)
}

// COSY signing for Qoder API requests

// pkcs7Pad adds PKCS7 padding
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := make([]byte, padding)
	for i := range padtext {
		padtext[i] = byte(padding)
	}
	return append(data, padtext...)
}

// aesEncryptCbcBase64 encrypts plaintext with AES-128-CBC and returns base64
func aesEncryptCbcBase64(plaintext string, keyStr string) (string, error) {
	keyBytes := []byte(keyStr)
	if len(keyBytes) != 16 {
		return "", fmt.Errorf("AES key must be 16 bytes, got %d", len(keyBytes))
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", err
	}

	iv := keyBytes[:16]
	padded := pkcs7Pad([]byte(plaintext), aes.BlockSize)

	ciphertext := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// rsaEncryptBase64 encrypts data with RSA public key and returns base64
func rsaEncryptBase64(data string) (string, error) {
	block, _ := pem.Decode([]byte(qoderRSAPublicKey))
	if block == nil {
		return "", fmt.Errorf("failed to parse RSA public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("not an RSA public key")
	}

	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, []byte(data))
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// encryptUserInfo encrypts user info for COSY header
func encryptUserInfo(userInfo map[string]string) (cosyKey, info string, err error) {
	uuid := generateUUID()
	aesKey := uuid[:16]

	plaintext, _ := json.Marshal(userInfo)
	infoB64, err := aesEncryptCbcBase64(string(plaintext), aesKey)
	if err != nil {
		return "", "", err
	}

	cosyKeyB64, err := rsaEncryptBase64(aesKey)
	if err != nil {
		return "", "", err
	}

	return cosyKeyB64, infoB64, nil
}

// md5Hex returns hex-encoded MD5 hash
func md5Hex(input string) string {
	h := md5.Sum([]byte(input))
	return hex.EncodeToString(h[:])
}

// computeSigPath strips /algo prefix from URL path
func computeSigPath(requestURL string) string {
	u, err := url.Parse(requestURL)
	if err != nil {
		return ""
	}
	path := u.Path
	if strings.HasPrefix(path, "/algo") {
		return path[len("/algo"):]
	}
	return path
}

// buildCosyHeaders builds COSY signing headers for Qoder requests
// IMPORTANT: body must be the ENCODED body (after qoderEncodeBody), not raw JSON!
func buildCosyHeaders(body []byte, requestURL string, userID, authToken, machineID string) (map[string]string, error) {
	if userID == "" {
		return nil, fmt.Errorf("cosy: user id is empty")
	}
	if authToken == "" {
		return nil, fmt.Errorf("cosy: auth token is empty")
	}

	cosyKey, info, err := encryptUserInfo(map[string]string{
		"uid":                  userID,
		"security_oauth_token": authToken,
		"name":                 "",
		"aid":                  "",
		"email":                "",
	})
	if err != nil {
		return nil, fmt.Errorf("cosy encrypt failed: %v", err)
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	requestID := generateUUID()

	payloadJSON := fmt.Sprintf(`{"version":"v1","requestId":"%s","info":"%s","cosyVersion":"%s","ideVersion":""}`,
		requestID, info, qoderIDEVersion)
	payloadB64 := base64.StdEncoding.EncodeToString([]byte(payloadJSON))

	sigPath := computeSigPath(requestURL)
	// Signature is computed over: payload + cosyKey + timestamp + ENCODED body + sigPath
	sigInput := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", payloadB64, cosyKey, timestamp, string(body), sigPath)
	sig := md5Hex(sigInput)

	bodyHash := md5Hex(string(body))
	bodyLength := fmt.Sprintf("%d", len(body))

	if machineID == "" {
		machineID = generateUUID()
	}

	return map[string]string{
		"Authorization":          fmt.Sprintf("COSY.%s.%s", payloadB64, sig),
		"Cosy-Key":               cosyKey,
		"Cosy-User":              userID,
		"Cosy-Date":              timestamp,
		"Cosy-Version":           qoderIDEVersion,
		"Cosy-Machineid":         machineID,
		"Cosy-Machinetoken":      machineID,
		"Cosy-Machinetype":       qoderMachineType,
		"Cosy-Machineos":         qoderMachineOS,
		"Cosy-Clienttype":        qoderClientType,
		"Cosy-Clientip":          "127.0.0.1",
		"Cosy-Bodyhash":          bodyHash,
		"Cosy-Bodylength":        bodyLength,
		"Cosy-Sigpath":           sigPath,
		"Cosy-Data-Policy":       qoderDataPolicy,
		"Cosy-Organization-Id":   "",
		"Cosy-Organization-Tags": "",
		"Login-Version":          qoderLoginVersion,
		"X-Request-Id":           generateUUID(),
	}, nil
}
