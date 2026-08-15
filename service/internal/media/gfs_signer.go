package media

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
)

const (
	gfsExpireSeconds = "60"
	gfsSavePath      = "blog/{{year}}/{{month}}/{{uuid}}.{{fileExt}}"
)

var ErrInvalidGFSConfiguration = errors.New("GFS adapter is not configured")

type Media struct {
	ID           int64
	PublicKey    string
	GFSFileID    int64
	OriginalName string
	MIMEType     string
	FileSize     int64
	Width        int
	Height       int
	State        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UploadPolicy struct {
	UploadURL string
	AppID     string
	Policy    string
	Signature string
	Timestamp string
	Expire    string
	Nonce     string
	FileField string
}

type ReadURLSigner interface {
	ReadURL(Media, time.Time) (string, error)
}

type GFSSigner struct {
	baseURL          url.URL
	appDomain        string
	appID            string
	uploadSecret     string
	publicReadSecret string
	keys             *randomkey.Generator
}

func NewGFSSigner(baseURL, appID, rawAppSecret, publicReadSecret string, keys *randomkey.Generator) (*GFSSigner, error) {
	return NewGFSSignerWithAppDomain(baseURL, "", appID, rawAppSecret, publicReadSecret, keys)
}

// NewGFSSignerWithAppDomain configures the account-book-compatible vanity
// domain used only for public read URLs. Uploads and metadata still use baseURL.
func NewGFSSignerWithAppDomain(baseURL, appDomain, appID, rawAppSecret, publicReadSecret string, keys *randomkey.Generator) (*GFSSigner, error) {
	parsedBaseURL, err := parseCanonicalBaseURL(baseURL)
	if err != nil || !validGFSAppDomain(appDomain) || strings.TrimSpace(appID) == "" || strings.TrimSpace(rawAppSecret) == "" || strings.TrimSpace(publicReadSecret) == "" || keys == nil {
		return nil, ErrInvalidGFSConfiguration
	}
	return &GFSSigner{
		baseURL:          parsedBaseURL,
		appDomain:        appDomain,
		appID:            appID,
		uploadSecret:     md5Hex(rawAppSecret),
		publicReadSecret: publicReadSecret,
		keys:             keys,
	}, nil
}

func (s *GFSSigner) UploadPolicy(now time.Time) (UploadPolicy, error) {
	if !s.valid() {
		return UploadPolicy{}, ErrInvalidGFSConfiguration
	}
	nonce, err := s.keys.Nonce()
	if err != nil {
		return UploadPolicy{}, ErrInvalidGFSConfiguration
	}
	policyJSON, err := json.Marshal(struct {
		SavePath string `json:"savePath"`
	}{SavePath: gfsSavePath})
	if err != nil {
		return UploadPolicy{}, ErrInvalidGFSConfiguration
	}
	policy := base64.StdEncoding.EncodeToString(policyJSON)
	timestamp := strconv.FormatInt(now.Unix(), 10)
	signature := md5Hex(strings.Join([]string{s.appID, policy, timestamp, gfsExpireSeconds, nonce, s.uploadSecret}, "_"))

	uploadURL := s.baseURL
	uploadURL.Path = "/v1/upload"
	return UploadPolicy{
		UploadURL: uploadURL.String(),
		AppID:     s.appID,
		Policy:    policy,
		Signature: signature,
		Timestamp: timestamp,
		Expire:    gfsExpireSeconds,
		Nonce:     nonce,
		FileField: "file",
	}, nil
}

func (s *GFSSigner) ReadURL(item Media, now time.Time) (string, error) {
	if !s.valid() || item.GFSFileID <= 0 {
		return "", ErrInvalidGFSConfiguration
	}
	policyJSON, err := json.Marshal(struct {
		UserID       string `json:"userId"`
		FileID       int64  `json:"fileId"`
		ImageWidth   int    `json:"imageWidth"`
		ImageHeight  int    `json:"imageHeight"`
		InternalFlag int    `json:"internalFlag"`
	}{FileID: item.GFSFileID})
	if err != nil {
		return "", ErrInvalidGFSConfiguration
	}
	policy := base64.StdEncoding.EncodeToString(policyJSON)
	timestamp := strconv.FormatInt(now.Unix(), 10)
	signature := md5Hex(strings.Join([]string{s.publicReadSecret, policy, timestamp, gfsExpireSeconds, s.publicReadSecret}, "_"))

	readURL := s.baseURL
	if s.appDomain != "" {
		readURL.Scheme = "https"
		readURL.Host = s.appDomain + ".r.img-bed.top"
	}
	readURL.Path = "/read/" + policy
	readURL.RawPath = "/read/" + url.PathEscape(policy)
	query := url.Values{}
	query.Set("signature", signature)
	query.Set("timestamp", timestamp)
	query.Set("expire", gfsExpireSeconds)
	readURL.RawQuery = query.Encode()
	return readURL.String(), nil
}

func validGFSAppDomain(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func (s *GFSSigner) valid() bool {
	return s != nil && s.baseURL.Scheme != "" && s.baseURL.Host != "" && s.appID != "" && s.uploadSecret != "" && s.publicReadSecret != "" && s.keys != nil
}

func parseCanonicalBaseURL(raw string) (url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Path != "" || parsed.RawPath != "" || parsed.Opaque != "" {
		return url.URL{}, ErrInvalidGFSConfiguration
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host != strings.ToLower(parsed.Host) || parsed.String() != raw {
		return url.URL{}, ErrInvalidGFSConfiguration
	}
	return *parsed, nil
}

func md5Hex(value string) string {
	digest := md5.Sum([]byte(value))
	return hex.EncodeToString(digest[:])
}
