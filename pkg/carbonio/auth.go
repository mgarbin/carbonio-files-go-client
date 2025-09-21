package carbonio

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"time"
)

type Authenticator interface {
	// Authenticate performs an HTTP POST with email and password,
	// returns the value of the ZM_AUTH_TOKEN cookie if authentication succeeds.
	CarbonioZxAuth(email, password string) (string, error)
}

type HTTPAuthenticator struct {
	Endpoint string
}

func isValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

func (a *HTTPAuthenticator) CarbonioZxAuth(email, password string) (string, error) {

	// Verify if email respect rfc
	if !isValidEmail(email) {
		return "", errors.New("invalid email address format")
	}

	// Create payload to inject to zx auth endpoint
	payload := map[string]string{
		"auth_method": "password",
		"user":        email,
		"password":    password,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// Make the request
	req, err := http.NewRequest("POST", "https://"+a.Endpoint+"/zx/auth/v2/login", bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	// Create HTTP client with SSL verification disabled
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	// Wait for response
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	// Read cookies
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "ZM_AUTH_TOKEN" {
			return cookie.Value, nil
		}
	}

	return "", errors.New("ZM_AUTH_TOKEN cookie not found")

}
