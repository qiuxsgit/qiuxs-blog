package platform

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretBoxUsesNoncePrefixedCanonicalRawStdAndCopiesCallerKey(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	random := bytes.NewReader(append(bytes.Repeat([]byte{3}, 12), bytes.Repeat([]byte{4}, 12)...))
	box, err := NewSecretBox(key, random)
	require.NoError(t, err)
	key[0] = 99

	first, err := box.Seal([]byte("jenkins-token"))
	require.NoError(t, err)
	second, err := box.Seal([]byte("jenkins-token"))
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.NotContains(t, first, "=")

	decoded, err := base64.RawStdEncoding.Strict().DecodeString(first)
	require.NoError(t, err)
	require.Equal(t, first, base64.RawStdEncoding.EncodeToString(decoded))
	require.Len(t, decoded, 12+len("jenkins-token")+16)
	require.Equal(t, bytes.Repeat([]byte{3}, 12), decoded[:12])

	plaintext, err := box.Open(first)
	require.NoError(t, err)
	require.Equal(t, []byte("jenkins-token"), plaintext)

	block, err := aes.NewCipher(bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)
	aead, err := cipher.NewGCM(block)
	require.NoError(t, err)
	want := aead.Seal(bytes.Repeat([]byte{3}, 12), bytes.Repeat([]byte{3}, 12), []byte("jenkins-token"), nil)
	require.Equal(t, base64.RawStdEncoding.EncodeToString(want), first)
}

func TestSecretBoxRejectsTamperingWrongKeyAndNoncanonicalCiphertextWithoutLeaks(t *testing.T) {
	key := bytes.Repeat([]byte{8}, 32)
	box, err := NewSecretBox(key, bytes.NewReader(bytes.Repeat([]byte{4}, 12)))
	require.NoError(t, err)
	sealed, err := box.Seal([]byte("token-plaintext-secret"))
	require.NoError(t, err)

	tampered, err := base64.RawStdEncoding.DecodeString(sealed)
	require.NoError(t, err)
	tampered[len(tampered)-1] ^= 1
	wrong, err := NewSecretBox(bytes.Repeat([]byte{9}, 32), bytes.NewReader(bytes.Repeat([]byte{1}, 12)))
	require.NoError(t, err)
	noncanonical := sealed[:len(sealed)-1] + "B"

	for name, input := range map[string]string{
		"malformed":    "ciphertext-secret!",
		"padded":       sealed + "=",
		"noncanonical": noncanonical,
		"too short":    base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 28)),
		"tampered":     base64.RawStdEncoding.EncodeToString(tampered),
	} {
		t.Run(name, func(t *testing.T) {
			_, openErr := box.Open(input)
			require.EqualError(t, openErr, "secret ciphertext is invalid")
			require.NotContains(t, openErr.Error(), input)
			require.NotContains(t, openErr.Error(), "token-plaintext-secret")
		})
	}
	_, err = wrong.Open(sealed)
	require.EqualError(t, err, "secret ciphertext is invalid")
}

func TestSecretBoxRejectsInvalidConfigurationEmptyPlaintextAndRandomFailures(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	typedNil := (*typedNilReader)(nil)
	for name, candidate := range map[string]struct {
		key    []byte
		random reader
	}{
		"empty key":  {key: nil, random: bytes.NewReader(nil)},
		"short key":  {key: []byte("key-secret"), random: bytes.NewReader(nil)},
		"nil random": {key: key, random: nil},
		"typed nil":  {key: key, random: typedNil},
	} {
		t.Run(name, func(t *testing.T) {
			box, err := NewSecretBox(candidate.key, candidate.random)
			require.Error(t, err)
			if len(candidate.key) > 0 {
				require.NotContains(t, err.Error(), string(candidate.key))
			}
			_, sealErr := box.Seal([]byte("token-secret"))
			require.EqualError(t, sealErr, "secret box is not configured")
		})
	}

	box, err := NewSecretBox(key, bytes.NewReader(bytes.Repeat([]byte{2}, 12)))
	require.NoError(t, err)
	_, err = box.Seal(nil)
	require.EqualError(t, err, "secret plaintext is required")

	failing, err := NewSecretBox(key, errorReader{err: errors.New("entropy-source-secret")})
	require.NoError(t, err)
	_, err = failing.Seal([]byte("token-secret"))
	require.EqualError(t, err, "read secret nonce")
	require.NotContains(t, err.Error(), "entropy-source-secret")

	short, err := NewSecretBox(key, bytes.NewReader(bytes.Repeat([]byte{3}, 11)))
	require.NoError(t, err)
	_, err = short.Seal([]byte("token-secret"))
	require.EqualError(t, err, "read secret nonce")
}

func TestSecretBoxNilAndZeroValuesNeverPanic(t *testing.T) {
	var nilBox *SecretBox
	var zero SecretBox
	for name, box := range map[string]*SecretBox{"nil": nilBox, "zero": &zero} {
		t.Run(name, func(t *testing.T) {
			_, err := box.Seal([]byte("token-secret"))
			require.EqualError(t, err, "secret box is not configured")
			_, err = box.Open("ciphertext-secret")
			require.EqualError(t, err, "secret box is not configured")
		})
	}
}

func TestSecretBoxSerializesEntropyReadsAcrossConcurrentValueCopies(t *testing.T) {
	reader := &concurrentRejectingReader{}
	box, err := NewSecretBox(bytes.Repeat([]byte{5}, 32), reader)
	require.NoError(t, err)
	copyOfBox := box

	const workers = 64
	results := make(chan string, workers)
	errorsSeen := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			selected := &box
			if index%2 == 1 {
				selected = &copyOfBox
			}
			sealed, sealErr := selected.Seal([]byte("token"))
			if sealErr != nil {
				errorsSeen <- sealErr
				return
			}
			results <- sealed
		}(index)
	}
	wait.Wait()
	close(results)
	close(errorsSeen)
	require.Empty(t, errorsSeen)
	require.False(t, reader.concurrent.Load())
	unique := map[string]struct{}{}
	for sealed := range results {
		unique[sealed] = struct{}{}
	}
	require.Len(t, unique, workers)
}

func TestHMACUsesExactLowercaseHexVectorAndStrictVerification(t *testing.T) {
	const want = "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8"
	key := []byte("key")
	message := []byte("The quick brown fox jumps over the lazy dog")
	require.Equal(t, want, ComputeHMAC(key, message))
	require.True(t, VerifyHMAC(key, message, want))

	for name, provided := range map[string]string{
		"uppercase": strings.ToUpper(want),
		"prefix":    "sha256=" + want,
		"short":     want[:62],
		"long":      want + "00",
		"non-hex":   want[:63] + "g",
		"wrong":     strings.Repeat("0", 64),
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, VerifyHMAC(key, message, provided))
		})
	}
	require.Empty(t, ComputeHMAC(nil, message))
	require.False(t, VerifyHMAC(nil, message, want))
}

type reader interface {
	Read([]byte) (int, error)
}

type typedNilReader struct{}

func (*typedNilReader) Read([]byte) (int, error) { return 0, nil }

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type concurrentRejectingReader struct {
	active     atomic.Bool
	concurrent atomic.Bool
	next       byte
}

func (r *concurrentRejectingReader) Read(target []byte) (int, error) {
	if !r.active.CompareAndSwap(false, true) {
		r.concurrent.Store(true)
		return 0, errors.New("concurrent read")
	}
	defer r.active.Store(false)
	runtime.Gosched()
	for index := range target {
		r.next++
		target[index] = r.next
	}
	return len(target), nil
}
