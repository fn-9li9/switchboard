package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 2
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("password must be at least 8 characters")
	}
	if len(password) > 72 {
		return "", errors.New("password must be at most 72 characters")
	}

	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	// RawURLEncoding: sin padding, sin + ni / — seguro en cualquier contexto
	saltB64 := base64.RawURLEncoding.EncodeToString(salt)
	hashB64 := base64.RawURLEncoding.EncodeToString(hash)

	return fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, saltB64, hashB64,
	), nil
}

func VerifyPassword(password, encodedHash string) error {
	salt, hash, params, err := parseHash(encodedHash)
	if err != nil {
		return fmt.Errorf("parsing hash: %w", err)
	}

	candidate := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(hash)))

	if !constantTimeCompare(hash, candidate) {
		return errors.New("invalid password")
	}
	return nil
}

type argonParams struct {
	time    uint32
	memory  uint32
	threads uint8
}

func parseHash(encoded string) (salt, hash []byte, params argonParams, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 {
		err = fmt.Errorf("invalid hash format: expected 5 parts got %d (hash=%q)", len(parts), encoded)
		return
	}
	// parts[0] = "argon2id"
	// parts[1] = "v=19"
	// parts[2] = "m=65536,t=2,p=4"
	// parts[3] = salt_b64
	// parts[4] = hash_b64

	if parts[0] != "argon2id" {
		err = fmt.Errorf("unsupported algorithm: %q", parts[0])
		return
	}

	_, scanErr := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &params.memory, &params.time, &params.threads)
	if scanErr != nil {
		err = fmt.Errorf("parsing params %q: %w", parts[2], scanErr)
		return
	}

	salt, err = base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		salt, err = base64.RawStdEncoding.DecodeString(parts[3])
		if err != nil {
			err = fmt.Errorf("decoding salt: %w", err)
			return
		}
	}

	hash, err = base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		hash, err = base64.RawStdEncoding.DecodeString(parts[4])
		if err != nil {
			err = fmt.Errorf("decoding hash: %w", err)
			return
		}
	}

	return
}

func constantTimeCompare(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
