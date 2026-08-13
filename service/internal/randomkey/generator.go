package randomkey

import (
	"errors"
	"io"
	"reflect"
	"sync"
)

const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789-_abcdefghijklmnopqrstuvwxyz"

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
	var randomByte [1]byte
	for index := range length {
		if _, err := io.ReadFull(g.reader, randomByte[:]); err != nil {
			return "", ErrRandomSource
		}
		value[len(prefix)+index] = alphabet[randomByte[0]&63]
	}
	return string(value), nil
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
