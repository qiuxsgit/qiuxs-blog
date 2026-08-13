package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	minPasswordLength = 1
	maxPasswordLength = 256

	maxMemoryKiB     = 256 * 1024
	maxIterations    = 10
	maxParallelism   = 16
	minSaltLength    = 16
	maxSaltLength    = 64
	minKeyLength     = 16
	maxKeyLength     = 64
	maxEncodedLength = 512
)

var (
	errInvalidPassword = errors.New("invalid password")
	errInvalidHash     = errors.New("invalid password hash")
	errInvalidHasher   = errors.New("invalid password hasher")
)

// PasswordHasher hashes and verifies passwords with Argon2id.
// Its fields are intentionally private; tests in this package may use lower
// costs while callers use DefaultPasswordHasher.
type PasswordHasher struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
	rand        io.Reader
}

// DefaultPasswordHasher returns the production Argon2id configuration.
func DefaultPasswordHasher() PasswordHasher {
	return PasswordHasher{
		memory:      64 * 1024,
		iterations:  3,
		parallelism: 2,
		saltLength:  16,
		keyLength:   32,
		rand:        rand.Reader,
	}
}

// Hash returns a PHC-like Argon2id password hash.
func (h PasswordHasher) Hash(password string) (string, error) {
	if !validPassword(password) {
		return "", errInvalidPassword
	}
	if !h.valid() {
		return "", errInvalidHasher
	}

	salt := make([]byte, h.saltLength)
	if _, err := io.ReadFull(h.rand, salt); err != nil {
		return "", errors.New("read password salt")
	}

	key := argon2.IDKey([]byte(password), salt, h.iterations, h.memory, h.parallelism, h.keyLength)
	return "$argon2id$v=" + strconv.Itoa(argon2.Version) + "$m=" + strconv.FormatUint(uint64(h.memory), 10) + ",t=" + strconv.FormatUint(uint64(h.iterations), 10) + ",p=" + strconv.FormatUint(uint64(h.parallelism), 10) + "$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}

// Verify reports whether password matches encodedHash.
func (h PasswordHasher) Verify(password, encodedHash string) (bool, error) {
	if !validPassword(password) {
		return false, errInvalidPassword
	}

	params, salt, expectedKey, err := parsePasswordHash(encodedHash)
	if err != nil {
		return false, err
	}

	actualKey := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, uint32(len(expectedKey)))
	return subtle.ConstantTimeCompare(actualKey, expectedKey) == 1, nil
}

type passwordHashParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func validPassword(password string) bool {
	return len(password) >= minPasswordLength && len(password) <= maxPasswordLength
}

func (h PasswordHasher) valid() bool {
	return h.rand != nil && validParams(h.memory, h.iterations, h.parallelism) &&
		h.saltLength >= minSaltLength && h.saltLength <= maxSaltLength &&
		h.keyLength >= minKeyLength && h.keyLength <= maxKeyLength
}

func validParams(memory, iterations uint32, parallelism uint8) bool {
	return memory >= 8 && memory <= maxMemoryKiB &&
		iterations >= 1 && iterations <= maxIterations &&
		parallelism >= 1 && parallelism <= maxParallelism &&
		memory >= 8*uint32(parallelism)
}

func parsePasswordHash(encodedHash string) (passwordHashParams, []byte, []byte, error) {
	if len(encodedHash) == 0 || len(encodedHash) > maxEncodedLength {
		return passwordHashParams{}, nil, nil, errInvalidHash
	}

	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return passwordHashParams{}, nil, nil, errInvalidHash
	}

	params, err := parseParams(parts[3])
	if err != nil {
		return passwordHashParams{}, nil, nil, errInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < minSaltLength || len(salt) > maxSaltLength {
		return passwordHashParams{}, nil, nil, errInvalidHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) < minKeyLength || len(key) > maxKeyLength {
		return passwordHashParams{}, nil, nil, errInvalidHash
	}

	return params, salt, key, nil
}

func parseParams(value string) (passwordHashParams, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return passwordHashParams{}, errInvalidHash
	}

	memory, ok := parseUint32(parts[0], "m=", maxMemoryKiB)
	if !ok {
		return passwordHashParams{}, errInvalidHash
	}
	iterations, ok := parseUint32(parts[1], "t=", maxIterations)
	if !ok {
		return passwordHashParams{}, errInvalidHash
	}
	parallelism, ok := parseUint8(parts[2], "p=", maxParallelism)
	if !ok || !validParams(memory, iterations, parallelism) {
		return passwordHashParams{}, errInvalidHash
	}

	return passwordHashParams{memory: memory, iterations: iterations, parallelism: parallelism}, nil
}

func parseUint32(value, prefix string, maximum uint32) (uint32, bool) {
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return 0, false
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 32)
	if err != nil || parsed == 0 || parsed > uint64(maximum) {
		return 0, false
	}
	return uint32(parsed), true
}

func parseUint8(value, prefix string, maximum uint8) (uint8, bool) {
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return 0, false
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 8)
	if err != nil || parsed == 0 || parsed > uint64(maximum) {
		return 0, false
	}
	return uint8(parsed), true
}
