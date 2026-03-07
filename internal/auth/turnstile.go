package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type TurnstileVerifier struct {
	secretKey  string
	httpClient *http.Client
}

func NewTurnstileVerifier(secretKey string) *TurnstileVerifier {
	return &TurnstileVerifier{
		secretKey: secretKey,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

type turnstileResponse struct {
	Success     bool     `json:"success"`
	ErrorCodes  []string `json:"error-codes"`
	Hostname    string   `json:"hostname"`
	ChallengeTS string   `json:"challenge_ts"`
}

// Verify valida el token Turnstile del cliente contra la API de Cloudflare.
// remoteIP es opcional — si se pasa, Cloudflare lo valida también.
func (t *TurnstileVerifier) Verify(token, remoteIP string) error {
	if token == "" {
		return errors.New("turnstile token is required")
	}

	form := url.Values{
		"secret":   {t.secretKey},
		"response": {token},
	}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	resp, err := t.httpClient.Post(
		turnstileVerifyURL,
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("turnstile request failed: %w", err)
	}
	defer resp.Body.Close()

	var result turnstileResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("turnstile response decode failed: %w", err)
	}

	if !result.Success {
		if len(result.ErrorCodes) > 0 {
			return fmt.Errorf("turnstile failed: %s", strings.Join(result.ErrorCodes, ", "))
		}
		return errors.New("turnstile validation failed")
	}

	return nil
}
