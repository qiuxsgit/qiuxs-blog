package platform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"sync"
)

var (
	errSecretBoxNotConfigured = errors.New("secret box is not configured")
	errSecretPlaintext        = errors.New("secret plaintext is required")
	errReadSecretNonce        = errors.New("read secret nonce")
	errSecretCiphertext       = errors.New("secret ciphertext is invalid")
)

type SecretBox struct {
	state *secretBoxState
}

type secretBoxState struct {
	key    [32]byte
	random io.Reader
	mu     sync.Mutex
}

func NewSecretBox(key []byte, random io.Reader) (SecretBox, error) {
	if len(key) != 32 || nilReader(random) {
		return SecretBox{}, errSecretBoxNotConfigured
	}
	state := &secretBoxState{random: random}
	copy(state.key[:], key)
	return SecretBox{state: state}, nil
}

func (b *SecretBox) Seal(plaintext []byte) (string, error) {
	if b == nil || b.state == nil {
		return "", errSecretBoxNotConfigured
	}
	if len(plaintext) == 0 {
		return "", errSecretPlaintext
	}
	aead, err := b.state.aead()
	if err != nil {
		return "", errSecretBoxNotConfigured
	}
	nonce := make([]byte, aead.NonceSize())
	b.state.mu.Lock()
	_, readErr := io.ReadFull(b.state.random, nonce)
	b.state.mu.Unlock()
	if readErr != nil {
		return "", errReadSecretNonce
	}
	sealed := aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (b *SecretBox) Open(encoded string) ([]byte, error) {
	if b == nil || b.state == nil {
		return nil, errSecretBoxNotConfigured
	}
	decoded, err := base64.RawStdEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.RawStdEncoding.EncodeToString(decoded) != encoded {
		return nil, errSecretCiphertext
	}
	aead, err := b.state.aead()
	if err != nil || len(decoded) <= aead.NonceSize()+aead.Overhead() {
		return nil, errSecretCiphertext
	}
	nonce := decoded[:aead.NonceSize()]
	plaintext, err := aead.Open(nil, nonce, decoded[aead.NonceSize():], nil)
	if err != nil || len(plaintext) == 0 {
		return nil, errSecretCiphertext
	}
	return plaintext, nil
}

func (s *secretBoxState) aead() (cipher.AEAD, error) {
	if s == nil {
		return nil, errSecretBoxNotConfigured
	}
	block, err := aes.NewCipher(s.key[:])
	if err != nil {
		return nil, errSecretBoxNotConfigured
	}
	return cipher.NewGCM(block)
}

func ComputeHMAC(key, canonical []byte) string {
	if len(key) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifyHMAC(key, canonical []byte, provided string) bool {
	if len(key) == 0 || len(provided) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(provided)
	if err != nil || hex.EncodeToString(decoded) != provided {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	return hmac.Equal(mac.Sum(nil), decoded)
}

func nilReader(reader io.Reader) bool {
	if reader == nil {
		return true
	}
	value := reflect.ValueOf(reader)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
