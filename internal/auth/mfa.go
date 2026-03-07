package auth

import (
	"bytes"
	"encoding/base64"
	"image/png"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// GenerateTOTPSecret genera un nuevo secret TOTP y devuelve:
// - la key (contiene secret, issuer, account)
// - el QR code como base64 PNG para incrustar en <img src="data:image/png;base64,...">
func GenerateTOTPSecret(issuer, accountName string) (*otp.Key, string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
		SecretSize:  32,
	})
	if err != nil {
		return nil, "", err
	}

	// Generar QR como PNG base64
	img, err := key.Image(256, 256)
	if err != nil {
		return nil, "", err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, "", err
	}

	qrBase64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	return key, qrBase64, nil
}

// ValidateTOTP verifica un código de 6 dígitos contra el secret.
func ValidateTOTP(code, secret string) bool {
	return totp.Validate(code, secret)
}
