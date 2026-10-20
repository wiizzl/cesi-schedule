package schedule

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
)

const (
	minSecretLen = 32
	maxTokenLen  = 2048
	tokenV1      = 1
)

var errBadToken = errors.New("invalid token")

type keys struct {
	aead   cipher.AEAD
	macKey []byte
}

func derive(secret []byte, label string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(label))
	return m.Sum(nil)
}

func newKeys(secret string) (*keys, error) {
	if len(secret) < minSecretLen {
		return nil, errors.New("CESI_SECRET must be set to at least 32 random characters (e.g. `openssl rand -base64 32`)")
	}

	block, err := aes.NewCipher(derive([]byte(secret), "cesi-schedule/enc"))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &keys{aead: aead, macKey: derive([]byte(secret), "cesi-schedule/mac")}, nil
}

func (k *keys) seal(plain []byte, ad string) []byte {
	nonce := make([]byte, k.aead.NonceSize(), k.aead.NonceSize()+len(plain)+k.aead.Overhead())
	rand.Read(nonce)
	return k.aead.Seal(nonce, nonce, plain, []byte(ad))
}

func (k *keys) open(b []byte, ad string) ([]byte, error) {
	ns := k.aead.NonceSize()
	if len(b) < ns {
		return nil, errBadToken
	}
	return k.aead.Open(nil, b[:ns], b[ns:], []byte(ad))
}

func (k *keys) mac(parts ...string) string {
	m := hmac.New(sha256.New, k.macKey)
	for _, p := range parts {
		m.Write([]byte(p))
		m.Write([]byte{0})
	}
	return hex.EncodeToString(m.Sum(nil)[:16])
}

func (k *keys) token(c creds) string {
	plain, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(append([]byte{tokenV1}, k.seal(plain, "token/v1")...))
}

func (k *keys) parseToken(t string) (creds, error) {
	if t == "" || len(t) > maxTokenLen {
		return creds{}, errBadToken
	}

	b, err := base64.RawURLEncoding.DecodeString(t)
	if err != nil || len(b) < 1 || b[0] != tokenV1 {
		return creds{}, errBadToken
	}

	plain, err := k.open(b[1:], "token/v1")
	if err != nil {
		return creds{}, errBadToken
	}

	var c creds
	if err := json.Unmarshal(plain, &c); err != nil || c.Username == "" || c.Password == "" {
		return creds{}, errBadToken
	}
	return c, nil
}
