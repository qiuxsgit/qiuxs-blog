package auth

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPasswordHasher() PasswordHasher {
	return PasswordHasher{
		memory:      64,
		iterations:  1,
		parallelism: 1,
		saltLength:  16,
		keyLength:   32,
		rand:        bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)),
	}
}

func TestDefaultPasswordHasherUsesProductionParameters(t *testing.T) {
	hasher := DefaultPasswordHasher()

	assert.Equal(t, uint32(64*1024), hasher.memory)
	assert.Equal(t, uint32(3), hasher.iterations)
	assert.Equal(t, uint8(2), hasher.parallelism)
	assert.Equal(t, uint32(16), hasher.saltLength)
	assert.Equal(t, uint32(32), hasher.keyLength)
	assert.NotNil(t, hasher.rand)
}

func TestPasswordHasherHashEncodesArgon2idAndVerifiesPassword(t *testing.T) {
	hasher := testPasswordHasher()

	hash, err := hasher.Hash("correct horse battery staple")

	require.NoError(t, err)
	assert.Regexp(t, `^\$argon2id\$v=19\$m=64,t=1,p=1\$[A-Za-z0-9+/]+\$[A-Za-z0-9+/]+$`, hash)
	verified, err := hasher.Verify("correct horse battery staple", hash)
	require.NoError(t, err)
	assert.True(t, verified)
}

func TestPasswordHasherVerifyReturnsFalseForWrongPassword(t *testing.T) {
	hasher := testPasswordHasher()
	hash, err := hasher.Hash("correct password")
	require.NoError(t, err)

	verified, err := hasher.Verify("wrong password", hash)

	require.NoError(t, err)
	assert.False(t, verified)
}

func TestPasswordHasherRejectsInvalidPasswordLengths(t *testing.T) {
	hasher := testPasswordHasher()

	for _, password := range []string{"", strings.Repeat("a", 257), strings.Repeat("é", 129)} {
		t.Run(fmt.Sprintf("%d bytes", len(password)), func(t *testing.T) {
			hash, err := hasher.Hash(password)

			assert.Empty(t, hash)
			assert.Error(t, err)
		})
	}
}

func TestPasswordHasherVerifyRejectsInvalidPasswordLengths(t *testing.T) {
	hasher := testPasswordHasher()
	hash, err := hasher.Hash("valid password")
	require.NoError(t, err)

	for _, password := range []string{"", strings.Repeat("a", 257), strings.Repeat("é", 129)} {
		t.Run(fmt.Sprintf("%d bytes", len(password)), func(t *testing.T) {
			verified, verifyErr := hasher.Verify(password, hash)

			assert.False(t, verified)
			assert.Error(t, verifyErr)
		})
	}
}

func TestPasswordHasherAcceptsPasswordAtByteLimit(t *testing.T) {
	hasher := testPasswordHasher()
	password := strings.Repeat("é", 128)

	hash, err := hasher.Hash(password)

	require.NoError(t, err)
	verified, err := hasher.Verify(password, hash)
	require.NoError(t, err)
	assert.True(t, verified)
}

func TestPasswordHasherVerifyRejectsMalformedEncoding(t *testing.T) {
	hasher := testPasswordHasher()

	for _, hash := range []string{
		"",
		"$argon2id$v=19$m=64,t=1,p=1$not+raw-base64$also+invalid",
		"$argon2id$v=18$m=64,t=1,p=1$YmJiYmJiYmJiYmJiYmJiYg$YmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmI",
		"$argon2i$v=19$m=64,t=1,p=1$YmJiYmJiYmJiYmJiYmJiYg$YmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmI",
		"$argon2id$v=19$m=4294967295,t=4294967295,p=255$YmJiYmJiYmJiYmJiYmJiYg$YmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmI",
		"$argon2id$v=19$m=64,t=1,p=1$" + strings.Repeat("A", 10000) + "$YmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmI",
	} {
		t.Run("invalid hash", func(t *testing.T) {
			verified, err := hasher.Verify("valid password", hash)

			assert.False(t, verified)
			assert.Error(t, err)
		})
	}
}
