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

func TestPasswordHasherVerifyRejectsNonCanonicalEncoding(t *testing.T) {
	hasher := testPasswordHasher()
	hash, err := hasher.Hash("valid password")
	require.NoError(t, err)

	parts := strings.Split(hash, "$")
	for name, encodedHash := range map[string]string{
		"leading-zero parameter": strings.Replace(hash, "m=64,", "m=064,", 1),
		"signed parameter":       strings.Replace(hash, "m=64,", "m=+64,", 1),
		"salt whitespace":        replacePasswordHashField(hash, 4, parts[4][:4]+"\n"+parts[4][4:]),
		"salt trailing bits":     replacePasswordHashField(hash, 4, nonCanonicalRawBase64(parts[4])),
		"key trailing bits":      replacePasswordHashField(hash, 5, nonCanonicalRawBase64(parts[5])),
	} {
		t.Run(name, func(t *testing.T) {
			verified, verifyErr := hasher.Verify("valid password", encodedHash)

			assert.False(t, verified)
			assert.Error(t, verifyErr)
		})
	}
}

func TestPasswordHasherVerifyRejectsParametersAboveProductionCeilings(t *testing.T) {
	hasher := testPasswordHasher()
	hash, err := hasher.Hash("valid password")
	require.NoError(t, err)

	for _, encodedHash := range []string{
		strings.Replace(hash, "m=64,", "m=65537,", 1),
		strings.Replace(hash, "t=1,", "t=4,", 1),
		strings.Replace(hash, "p=1$", "p=3$", 1),
	} {
		t.Run("parameter exceeds production ceiling", func(t *testing.T) {
			verified, verifyErr := hasher.Verify("valid password", encodedHash)

			assert.False(t, verified)
			assert.Error(t, verifyErr)
		})
	}
}

func TestPasswordHasherHashFailsClosedForShortEntropy(t *testing.T) {
	hasher := testPasswordHasher()
	hasher.rand = bytes.NewReader([]byte{1})

	hash, err := hasher.Hash("valid password")

	assert.Empty(t, hash)
	require.EqualError(t, err, "read password salt")
	assert.NotContains(t, err.Error(), "valid password")
}

func replacePasswordHashField(hash string, index int, value string) string {
	parts := strings.Split(hash, "$")
	parts[index] = value
	return strings.Join(parts, "$")
}

func nonCanonicalRawBase64(value string) string {
	last := value[len(value)-1]
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	lastIndex := strings.IndexByte(alphabet, last)
	return value[:len(value)-1] + string(alphabet[lastIndex^1])
}
