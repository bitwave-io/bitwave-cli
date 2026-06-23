package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"math/big"
)

// pkceRawBytes is the number of random bytes used to generate the verifier.
// 32 bytes → 43-char base64url string, which satisfies the RFC 7636 minimum of 43.
const pkceRawBytes = 32

// GeneratePKCE returns a code_verifier (base64url-encoded random bytes) and its
// S256 code_challenge. The challenge is computed by hashing the raw bytes (not the
// base64url string) to match the auth server's verification, which decodes the
// verifier from base64url before hashing.
func GeneratePKCE() (verifier, challenge string, err error) {
	raw := make([]byte, pkceRawBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	v := base64.RawURLEncoding.EncodeToString(raw)
	h := sha256.Sum256(raw)
	c := base64.RawURLEncoding.EncodeToString(h[:])
	return v, c, nil
}

// stateAlphabet is the set of characters used for the OAuth state parameter.
const stateAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// GenerateState returns a random string suitable for the OAuth state parameter.
func GenerateState() (string, error) {
	return randomString(32, stateAlphabet)
}

func randomString(length int, alphabet string) (string, error) {
	max := big.NewInt(int64(len(alphabet)))
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b), nil
}
