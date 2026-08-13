package randomkey

import (
	"errors"
	"io"
	"reflect"
	"sync"
)

const (
	alphabet              = "abcdefghijklmnopqrstuvwxyz0123456789-_"
	acceptedByteLimit     = 228
	maximumRandomAttempts = 256
)

var (
	ErrInvalidGenerator = errors.New("random key generator is not configured")
	ErrRandomSource     = errors.New("random key generation failed")
)

type Generator struct {
	reader io.Reader
	mu     sync.Mutex
}

func New(reader io.Reader) (*Generator, error) {
	if nilReader(reader) {
		return nil, ErrInvalidGenerator
	}
	return &Generator{reader: reader}, nil
}

func (g *Generator) ArticleSlug() (string, error) {
	return g.generate("", 12)
}

func (g *Generator) TagSlug() (string, error) {
	return g.generate("t_", 12)
}

func (g *Generator) MediaPublicKey() (string, error) {
	return g.generate("m_", 22)
}

func (g *Generator) Nonce() (string, error) {
	return g.generate("", 22)
}

func (g *Generator) generate(prefix string, length int) (string, error) {
	if g == nil || nilReader(g.reader) {
		return "", ErrInvalidGenerator
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	value := make([]byte, len(prefix)+length)
	copy(value, prefix)
	for index := range length {
		character, err := g.nextCharacter()
		if err != nil {
			return "", ErrRandomSource
		}
		value[len(prefix)+index] = character
	}
	return string(value), nil
}

func (g *Generator) nextCharacter() (byte, error) {
	var randomByte [1]byte
	for range maximumRandomAttempts {
		count, err := g.reader.Read(randomByte[:])
		if count != 1 {
			return 0, ErrRandomSource
		}
		if randomByte[0] < acceptedByteLimit {
			return alphabet[int(randomByte[0])%len(alphabet)], nil
		}
		if err != nil {
			return 0, ErrRandomSource
		}
	}
	return 0, ErrRandomSource
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
