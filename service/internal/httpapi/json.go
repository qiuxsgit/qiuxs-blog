package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	maxAdminJSONBodyBytes     = 64 * 1024
	maxAdminMarkdownBodyBytes = 2 * 1024 * 1024
)

func decodeAdminJSON[T any](_ *gin.Context, request *http.Request, writer http.ResponseWriter, limit int64) (T, error) {
	var zero T
	contentTypes := request.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		return zero, ErrInvalidRequest
	}
	mediaType, params, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || !strings.EqualFold(mediaType, "application/json") || !validJSONMediaTypeParams(params) {
		return zero, ErrInvalidRequest
	}

	request.Body = http.MaxBytesReader(writer, request.Body, limit)
	body, err := io.ReadAll(request.Body)
	if err != nil || len(body) == 0 {
		return zero, ErrInvalidRequest
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) < 2 || trimmed[0] != '{' {
		return zero, ErrInvalidRequest
	}
	shapeDecoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := validateAdminJSONValue(shapeDecoder, reflect.TypeFor[T](), nil, false); err != nil {
		return zero, ErrInvalidRequest
	}
	if token, err := shapeDecoder.Token(); err == nil || !errors.Is(err, io.EOF) || token != nil {
		return zero, ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		return zero, ErrInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return zero, ErrInvalidRequest
	}
	return value, nil
}

func validateAdminJSONValue(decoder *json.Decoder, target reflect.Type, first json.Token, hasFirst bool) error {
	nullable := false
	for target.Kind() == reflect.Pointer {
		nullable = true
		target = target.Elem()
	}
	token := first
	var err error
	if !hasFirst {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
	}
	if token == nil {
		if !nullable {
			return ErrInvalidRequest
		}
		return nil
	}

	switch target.Kind() {
	case reflect.Struct:
		if token != json.Delim('{') {
			return ErrInvalidRequest
		}
		fields := make(map[string]reflect.Type, target.NumField())
		for index := range target.NumField() {
			field := target.Field(index)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				fields[name] = field.Type
			}
		}
		seen := make(map[string]bool, len(fields))
		for decoder.More() {
			propertyToken, tokenErr := decoder.Token()
			if tokenErr != nil {
				return tokenErr
			}
			property, ok := propertyToken.(string)
			fieldType, known := fields[property]
			if !ok || !known || seen[property] {
				return ErrInvalidRequest
			}
			seen[property] = true
			if err := validateAdminJSONValue(decoder, fieldType, nil, false); err != nil {
				return err
			}
		}
		closing, closingErr := decoder.Token()
		if closingErr != nil || closing != json.Delim('}') || len(seen) != len(fields) {
			return ErrInvalidRequest
		}
	case reflect.Slice, reflect.Array:
		if token != json.Delim('[') {
			return ErrInvalidRequest
		}
		for decoder.More() {
			if err := validateAdminJSONValue(decoder, target.Elem(), nil, false); err != nil {
				return err
			}
		}
		closing, closingErr := decoder.Token()
		if closingErr != nil || closing != json.Delim(']') {
			return ErrInvalidRequest
		}
	default:
		if _, isContainer := token.(json.Delim); isContainer {
			return ErrInvalidRequest
		}
	}
	return nil
}
