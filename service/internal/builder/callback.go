package builder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/release"
	"github.com/redis/go-redis/v9"
)

const (
	maxCallbackBodyBytes = 16 * 1024
	callbackReplayTTL    = 5 * time.Minute
	callbackClockSkew    = 5 * time.Minute
	callbackNoncePrefix  = "qiuxs-blog:jenkins:nonce:"
)

var (
	ErrInvalidCallback      = errors.New("Jenkins callback is invalid")
	ErrCallbackUnauthorized = errors.New("Jenkins callback is unauthorized")
	ErrCallbackReplay       = errors.New("Jenkins callback replay conflict")

	callbackNoncePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
)

type CallbackPayload struct {
	ReleaseID    int64             `json:"releaseId"`
	PublishJobID int64             `json:"publishJobId"`
	BuildNumber  int64             `json:"buildNumber"`
	Stage        string            `json:"stage"`
	Status       release.JobStatus `json:"status"`
	ErrorSummary string            `json:"errorSummary"`
	Timestamp    time.Time         `json:"timestamp"`
	Nonce        string            `json:"nonce"`
}

func (p CallbackPayload) Event() release.CallbackEvent {
	return release.CallbackEvent{
		ReleaseID: p.ReleaseID, PublishJobID: p.PublishJobID, BuildNumber: p.BuildNumber,
		Stage: p.Stage, Status: p.Status, ErrorSummary: p.ErrorSummary,
		Timestamp: p.Timestamp, Nonce: p.Nonce,
	}
}

type CallbackVerifier struct {
	key   []byte
	redis *redis.Client
	now   func() time.Time
}

type JenkinsTargetProvider struct {
	configs ConfigRepository
	client  *Client
	box     *platform.SecretBox
}

func NewCallbackVerifier(key []byte, client *redis.Client, now func() time.Time) (*CallbackVerifier, error) {
	if len(key) < 32 || len(key) > 128 || !callbackRedisConfigured(client) || now == nil {
		return nil, errors.New("Jenkins callback verifier is not configured")
	}
	return &CallbackVerifier{key: append([]byte(nil), key...), redis: client, now: now}, nil
}

func NewJenkinsTargetProvider(configs ConfigRepository, client *Client, box *platform.SecretBox) (*JenkinsTargetProvider, error) {
	if nilBuilderInterface(configs) || client == nil || box == nil {
		return nil, errors.New("Jenkins target provider dependencies are required")
	}
	return &JenkinsTargetProvider{configs: configs, client: client, box: box}, nil
}

func (p *JenkinsTargetProvider) Prepare(ctx context.Context) (release.BuilderTarget, error) {
	if p == nil || nilBuilderInterface(p.configs) || p.client == nil || p.box == nil {
		return release.BuilderTarget{}, errors.New("Jenkins target provider is not configured")
	}
	if nilBuilderInterface(ctx) {
		return release.BuilderTarget{}, builderDomain("prepare Jenkins target", ErrInvalidConfig)
	}
	config, err := p.configs.Load(ctx)
	if err != nil {
		return release.BuilderTarget{}, builderDependency("load Jenkins configuration", errors.New("builder configuration is unavailable"))
	}
	if err := p.client.validate(ctx, config, p.box); err != nil {
		return release.BuilderTarget{}, err
	}
	return release.BuilderTarget{
		BuilderID: config.ID,
		Snapshot: release.BuilderTargetSnapshot{
			Name: config.Name, BaseURL: config.BaseURL, Username: config.Username, JobName: config.JobName,
		},
		Trigger: func(triggerContext context.Context, releaseID, publishJobID int64) (int64, error) {
			return p.client.Trigger(triggerContext, config, p.box, releaseID, publishJobID)
		},
	}, nil
}

func (v *CallbackVerifier) VerifyAndClaim(ctx context.Context, raw []byte, signature string) (CallbackPayload, bool, error) {
	if err := v.validate(ctx); err != nil {
		return CallbackPayload{}, false, err
	}
	if len(raw) == 0 || len(raw) > maxCallbackBodyBytes || !utf8.Valid(raw) {
		return CallbackPayload{}, false, builderDomain("decode Jenkins callback", ErrInvalidCallback)
	}
	payload, err := decodeCallbackPayload(raw)
	if err != nil {
		return CallbackPayload{}, false, builderDomain("decode Jenkins callback", ErrInvalidCallback)
	}
	canonicalRaw, err := marshalCanonicalCallback(payload)
	if err != nil || !bytes.Equal(raw, canonicalRaw) {
		return CallbackPayload{}, false, builderDomain("decode Jenkins callback", ErrInvalidCallback)
	}
	now := v.now()
	if now.IsZero() {
		return CallbackPayload{}, false, builderDependency("read Jenkins callback clock", errors.New("callback clock is invalid"))
	}
	now = now.UTC()
	if payload.Timestamp.Before(now.Add(-callbackClockSkew)) || payload.Timestamp.After(now.Add(callbackClockSkew)) {
		return CallbackPayload{}, false, builderDomain("validate Jenkins callback timestamp", ErrInvalidCallback)
	}
	if !validCallbackSignature(signature) {
		return CallbackPayload{}, false, builderDomain("authenticate Jenkins callback", ErrCallbackUnauthorized)
	}
	canonical := make([]byte, 0, len(raw)+len(payload.Nonce)+32)
	canonical = strconv.AppendInt(canonical, payload.Timestamp.Unix(), 10)
	canonical = append(canonical, '\n')
	canonical = append(canonical, payload.Nonce...)
	canonical = append(canonical, '\n')
	canonical = append(canonical, raw...)
	if !platform.VerifyHMAC(v.key, canonical, strings.TrimPrefix(signature, "sha256=")) {
		return CallbackPayload{}, false, builderDomain("authenticate Jenkins callback", ErrCallbackUnauthorized)
	}

	nonceDigest := sha256.Sum256([]byte(payload.Nonce))
	payloadDigest := sha256.Sum256(raw)
	replayKey := callbackNoncePrefix + hex.EncodeToString(nonceDigest[:])
	digest := hex.EncodeToString(payloadDigest[:])
	claimed, err := v.redis.SetNX(ctx, replayKey, digest, callbackReplayTTL).Result()
	if err != nil {
		return CallbackPayload{}, false, builderDependency("claim Jenkins callback nonce", err)
	}
	if claimed {
		return payload, false, nil
	}
	stored, err := v.redis.Get(ctx, replayKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return CallbackPayload{}, false, builderDependency("read Jenkins callback nonce", err)
	}
	if err == nil && stored == digest {
		return payload, true, nil
	}
	return CallbackPayload{}, false, builderDomain("validate Jenkins callback replay", ErrCallbackReplay)
}

func marshalCanonicalCallback(payload CallbackPayload) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'}), nil
}

func (v *CallbackVerifier) validate(ctx context.Context) error {
	if v == nil || len(v.key) < 32 || len(v.key) > 128 || !callbackRedisConfigured(v.redis) || v.now == nil {
		return errors.New("Jenkins callback verifier is not configured")
	}
	if nilBuilderInterface(ctx) {
		return builderDomain("use Jenkins callback verifier", ErrInvalidCallback)
	}
	return nil
}

func decodeCallbackPayload(raw []byte) (CallbackPayload, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return CallbackPayload{}, errors.New("callback must be an object")
	}
	var payload CallbackPayload
	seen := make(map[string]struct{}, 8)
	for decoder.More() {
		token, tokenErr := decoder.Token()
		name, ok := token.(string)
		if tokenErr != nil || !ok {
			return CallbackPayload{}, errors.New("callback field is invalid")
		}
		if _, duplicate := seen[name]; duplicate {
			return CallbackPayload{}, errors.New("callback field is duplicated")
		}
		seen[name] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return CallbackPayload{}, errors.New("callback field is invalid")
		}
		switch name {
		case "releaseId":
			payload.ReleaseID, err = decodePositiveCallbackInteger(value)
		case "publishJobId":
			payload.PublishJobID, err = decodePositiveCallbackInteger(value)
		case "buildNumber":
			payload.BuildNumber, err = decodePositiveCallbackInteger(value)
		case "stage":
			payload.Stage, err = decodeCallbackString(value)
		case "status":
			var status string
			status, err = decodeCallbackString(value)
			payload.Status = release.JobStatus(status)
		case "errorSummary":
			payload.ErrorSummary, err = decodeCallbackString(value)
		case "timestamp":
			var encoded string
			encoded, err = decodeCallbackString(value)
			if err == nil {
				payload.Timestamp, err = time.Parse(time.RFC3339Nano, encoded)
				payload.Timestamp = payload.Timestamp.UTC()
			}
		case "nonce":
			payload.Nonce, err = decodeCallbackString(value)
		default:
			return CallbackPayload{}, errors.New("callback field is unknown")
		}
		if err != nil {
			return CallbackPayload{}, errors.New("callback field is invalid")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seen) != 8 {
		return CallbackPayload{}, errors.New("callback is incomplete")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return CallbackPayload{}, errors.New("callback has trailing data")
	}
	if !validCallbackPayload(payload) {
		return CallbackPayload{}, errors.New("callback values are invalid")
	}
	return payload, nil
}

func decodePositiveCallbackInteger(raw json.RawMessage) (int64, error) {
	encoded := string(bytes.TrimSpace(raw))
	if len(encoded) == 0 || encoded[0] < '1' || encoded[0] > '9' {
		return 0, errors.New("callback integer is invalid")
	}
	for index := 1; index < len(encoded); index++ {
		if encoded[index] < '0' || encoded[index] > '9' {
			return 0, errors.New("callback integer is invalid")
		}
	}
	return strconv.ParseInt(encoded, 10, 64)
}

func decodeCallbackString(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' || !validCallbackStringEscapes(raw) {
		return "", errors.New("callback string is invalid")
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil || !utf8.ValidString(decoded) {
		return "", errors.New("callback string is invalid")
	}
	return decoded, nil
}

func validCallbackStringEscapes(raw []byte) bool {
	for index := 1; index < len(raw)-1; index++ {
		if raw[index] != '\\' {
			continue
		}
		index++
		if index >= len(raw)-1 || raw[index] != 'u' {
			continue
		}
		first, ok := callbackHexRune(raw, index+1)
		if !ok {
			return false
		}
		index += 4
		if first >= 0xdc00 && first <= 0xdfff {
			return false
		}
		if first < 0xd800 || first > 0xdbff {
			continue
		}
		if index+6 >= len(raw) || raw[index+1] != '\\' || raw[index+2] != 'u' {
			return false
		}
		second, ok := callbackHexRune(raw, index+3)
		if !ok || second < 0xdc00 || second > 0xdfff || utf16.DecodeRune(rune(first), rune(second)) == utf8.RuneError {
			return false
		}
		index += 6
	}
	return true
}

func callbackHexRune(raw []byte, start int) (int, bool) {
	if start+4 > len(raw)-1 {
		return 0, false
	}
	value := 0
	for _, character := range raw[start : start+4] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += int(character - '0')
		case character >= 'a' && character <= 'f':
			value += int(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += int(character-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func validCallbackPayload(payload CallbackPayload) bool {
	if payload.ReleaseID <= 0 || payload.PublishJobID <= 0 || payload.BuildNumber <= 0 || payload.Timestamp.IsZero() ||
		!callbackNoncePattern.MatchString(payload.Nonce) || !utf8.ValidString(payload.Stage) || !utf8.ValidString(payload.ErrorSummary) ||
		utf8.RuneCountInString(payload.ErrorSummary) > 512 {
		return false
	}
	switch payload.Status {
	case release.JobQueued:
		return payload.Stage == "queue" && payload.ErrorSummary == ""
	case release.JobBuilding:
		return payload.Stage == "build" && payload.ErrorSummary == ""
	case release.JobDeploying, release.JobSuccess:
		return payload.Stage == "deploy" && payload.ErrorSummary == ""
	case release.JobFailed:
		return payload.Stage == "queue" || payload.Stage == "build" || payload.Stage == "deploy"
	default:
		return false
	}
}

func validCallbackSignature(signature string) bool {
	if len(signature) != len("sha256=")+sha256.Size*2 || !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	digest := strings.TrimPrefix(signature, "sha256=")
	decoded, err := hex.DecodeString(digest)
	return err == nil && hex.EncodeToString(decoded) == digest
}

func callbackRedisConfigured(client *redis.Client) (configured bool) {
	if client == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			configured = false
		}
	}()
	return client.Options() != nil
}
