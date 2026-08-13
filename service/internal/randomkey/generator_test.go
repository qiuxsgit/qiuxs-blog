package randomkey_test

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
	"github.com/stretchr/testify/require"
)

func TestRandomKeyGeneratorUsesFixedLengthsAlphabetAndVectors(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		call  func(*randomkey.Generator) (string, error)
		want  string
	}{
		{
			name:  "article slug",
			input: byteRange(0, 12),
			call:  (*randomkey.Generator).ArticleSlug,
			want:  "abcdefghijkl",
		},
		{
			name:  "tag slug",
			input: byteRange(0, 12),
			call:  (*randomkey.Generator).TagSlug,
			want:  "t_abcdefghijkl",
		},
		{
			name:  "media public key",
			input: byteRange(0, 22),
			call:  (*randomkey.Generator).MediaPublicKey,
			want:  "m_abcdefghijklmnopqrstuv",
		},
		{
			name:  "nonce includes digits and URL-safe punctuation",
			input: append(byteRange(26, 38), byteRange(38, 48)...),
			call:  (*randomkey.Generator).Nonce,
			want:  "0123456789-_abcdefghij",
		},
	}

	allowed := regexp.MustCompile(`^[a-z0-9_-]+$`)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &oneByteReader{bytes: test.input}
			generator, err := randomkey.New(reader)
			require.NoError(t, err)

			got, err := test.call(generator)

			require.NoError(t, err)
			require.Equal(t, test.want, got)
			require.True(t, allowed.MatchString(got))
			require.Equal(t, len(test.input), reader.calls)
		})
	}
}

func TestRandomKeyGeneratorMapsAcceptedRangeUniformlyIntoTheFixedAlphabet(t *testing.T) {
	input := byteRange(0, 228)
	reader := &oneByteReader{bytes: input}
	generator, err := randomkey.New(reader)
	require.NoError(t, err)
	wantBlock := "abcdefghijklmnopqrstuvwxyz0123456789-_"

	var got string
	for range 6 {
		part, callErr := generator.Nonce()
		require.NoError(t, callErr)
		got += part
	}
	for range 8 {
		part, callErr := generator.ArticleSlug()
		require.NoError(t, callErr)
		got += part
	}

	require.Equal(t, strings.Repeat(wantBlock, 6), got)
	require.Equal(t, 228, reader.calls)
}

func TestRandomKeyGeneratorRejectsBytesAtBoundaryBeforeMapping(t *testing.T) {
	input := append([]byte{228, 255}, byteRange(0, 22)...)
	reader := &oneByteReader{bytes: input}
	generator, err := randomkey.New(reader)
	require.NoError(t, err)

	got, err := generator.Nonce()

	require.NoError(t, err)
	require.Equal(t, "abcdefghijklmnopqrstuv", got)
	require.Equal(t, 24, reader.calls)
}

func TestRandomKeyGeneratorFailsWhenOnlyRejectedBytesRemain(t *testing.T) {
	reader := &oneByteReader{bytes: []byte{228, 255}}
	generator, err := randomkey.New(reader)
	require.NoError(t, err)

	got, err := generator.ArticleSlug()

	require.Empty(t, got)
	require.ErrorIs(t, err, randomkey.ErrRandomSource)
	require.Equal(t, 2, reader.calls)
}

func TestRandomKeyGeneratorBoundsContinuousRejection(t *testing.T) {
	reader := &repeatingByteReader{value: 255}
	generator, err := randomkey.New(reader)
	require.NoError(t, err)

	got, err := generator.ArticleSlug()

	require.Empty(t, got)
	require.ErrorIs(t, err, randomkey.ErrRandomSource)
	require.Equal(t, 256, reader.calls)
}

func TestRandomKeyGeneratorFailsSafelyForInvalidReceiversAndSources(t *testing.T) {
	var nilGenerator *randomkey.Generator
	for _, call := range []struct {
		name string
		fn   func() (string, error)
	}{
		{name: "nil article receiver", fn: nilGenerator.ArticleSlug},
		{name: "nil tag receiver", fn: nilGenerator.TagSlug},
		{name: "nil media receiver", fn: nilGenerator.MediaPublicKey},
		{name: "nil nonce receiver", fn: nilGenerator.Nonce},
	} {
		t.Run(call.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				value, err := call.fn()
				require.Empty(t, value)
				require.ErrorIs(t, err, randomkey.ErrInvalidGenerator)
			})
		})
	}

	_, err := randomkey.New(nil)
	require.ErrorIs(t, err, randomkey.ErrInvalidGenerator)

	var typedNil *bytes.Reader
	_, err = randomkey.New(typedNil)
	require.ErrorIs(t, err, randomkey.ErrInvalidGenerator)

	short, err := randomkey.New(bytes.NewReader([]byte{1}))
	require.NoError(t, err)
	value, err := short.ArticleSlug()
	require.Empty(t, value)
	require.ErrorIs(t, err, randomkey.ErrRandomSource)

	failing, err := randomkey.New(errorReader{})
	require.NoError(t, err)
	value, err = failing.Nonce()
	require.Empty(t, value)
	require.ErrorIs(t, err, randomkey.ErrRandomSource)
	require.NotContains(t, err.Error(), "random-source-secret")
}

func TestRandomKeyGeneratorSerializesConcurrentReads(t *testing.T) {
	generator, err := randomkey.New(bytes.NewReader(bytes.Repeat(byteRange(0, 64), 64)))
	require.NoError(t, err)

	const workers = 64
	errorsSeen := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			value, callErr := generator.Nonce()
			if callErr == nil && len(value) != 22 {
				callErr = errors.New("wrong nonce length")
			}
			errorsSeen <- callErr
		}()
	}
	group.Wait()
	close(errorsSeen)
	for callErr := range errorsSeen {
		require.NoError(t, callErr)
	}
}

type oneByteReader struct {
	bytes []byte
	calls int
}

func (r *oneByteReader) Read(target []byte) (int, error) {
	if len(target) != 1 {
		return 0, errors.New("generator must request one byte")
	}
	if r.calls >= len(r.bytes) {
		return 0, io.EOF
	}
	target[0] = r.bytes[r.calls]
	r.calls++
	return 1, nil
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("random-source-secret")
}

type repeatingByteReader struct {
	value byte
	calls int
}

func (r *repeatingByteReader) Read(target []byte) (int, error) {
	target[0] = r.value
	r.calls++
	return 1, nil
}

func byteRange(start, end byte) []byte {
	values := make([]byte, 0, int(end-start))
	for value := start; value < end; value++ {
		values = append(values, value)
	}
	return values
}
