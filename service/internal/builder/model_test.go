package builder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateConfigAcceptsCanonicalRootAndSafeFolderJob(t *testing.T) {
	for _, input := range []ConfigInput{
		{Name: "Production", BaseURL: "https://jenkins.example.com", Username: "ci", Token: "token", JobName: "site/build", Enabled: true},
		{Name: "Local", BaseURL: "https://127.0.0.1:8443", Username: "ci-user", JobName: "folder/.visible-name_1", Enabled: false},
	} {
		require.NoError(t, ValidateConfig(input))
	}
}

func TestValidateConfigUsesDocumentedRuneLimitForOpaqueToken(t *testing.T) {
	input := ConfigInput{Name: "Production", BaseURL: "https://jenkins.example.com", Username: "ci", Token: strings.Repeat("密", 4096), JobName: "site/build", Enabled: true}
	require.NoError(t, ValidateConfig(input))
	input.Token += "钥"
	require.ErrorIs(t, ValidateConfig(input), ErrInvalidConfig)
}

func TestValidateConfigRejectsNoncanonicalOrUnsafeValuesWithoutLeakingThem(t *testing.T) {
	valid := ConfigInput{Name: "Production", BaseURL: "https://jenkins.example.com", Username: "ci", Token: "token", JobName: "site/build", Enabled: true}
	tests := map[string]func(*ConfigInput){
		"blank name":         func(input *ConfigInput) { input.Name = " " },
		"trimmed name":       func(input *ConfigInput) { input.Name = " Production" },
		"name too long":      func(input *ConfigInput) { input.Name = strings.Repeat("n", 101) },
		"invalid name utf8":  func(input *ConfigInput) { input.Name = string([]byte{0xff}) },
		"http":               func(input *ConfigInput) { input.BaseURL = "http://jenkins-secret.example.com" },
		"uppercase scheme":   func(input *ConfigInput) { input.BaseURL = "HTTPS://jenkins-secret.example.com" },
		"uppercase host":     func(input *ConfigInput) { input.BaseURL = "https://JENKINS-SECRET.example.com" },
		"userinfo":           func(input *ConfigInput) { input.BaseURL = "https://user-secret@jenkins.example.com" },
		"root slash":         func(input *ConfigInput) { input.BaseURL = "https://jenkins.example.com/" },
		"path":               func(input *ConfigInput) { input.BaseURL = "https://jenkins.example.com/secret" },
		"empty query":        func(input *ConfigInput) { input.BaseURL = "https://jenkins.example.com?" },
		"query":              func(input *ConfigInput) { input.BaseURL = "https://jenkins.example.com?token=secret" },
		"empty fragment":     func(input *ConfigInput) { input.BaseURL = "https://jenkins.example.com#" },
		"fragment":           func(input *ConfigInput) { input.BaseURL = "https://jenkins.example.com#secret" },
		"default port":       func(input *ConfigInput) { input.BaseURL = "https://jenkins.example.com:443" },
		"zero padded port":   func(input *ConfigInput) { input.BaseURL = "https://jenkins.example.com:08443" },
		"invalid port":       func(input *ConfigInput) { input.BaseURL = "https://jenkins.example.com:65536" },
		"trailing dot":       func(input *ConfigInput) { input.BaseURL = "https://jenkins.example.com." },
		"unicode host":       func(input *ConfigInput) { input.BaseURL = "https://jenkins例.example.com" },
		"noncanonical ipv4":  func(input *ConfigInput) { input.BaseURL = "https://127.0.00.1" },
		"ipv6 outside ddl":   func(input *ConfigInput) { input.BaseURL = "https://[::1]:8443" },
		"blank username":     func(input *ConfigInput) { input.Username = " " },
		"trimmed username":   func(input *ConfigInput) { input.Username = " ci" },
		"ambiguous username": func(input *ConfigInput) { input.Username = "ci:admin" },
		"username too long":  func(input *ConfigInput) { input.Username = strings.Repeat("u", 256) },
		"token too long":     func(input *ConfigInput) { input.Token = strings.Repeat("t", 4097) },
		"job double slash":   func(input *ConfigInput) { input.JobName = "site//build" },
		"job dot segment":    func(input *ConfigInput) { input.JobName = "site/../build" },
		"job empty segment":  func(input *ConfigInput) { input.JobName = "/site" },
		"job unsafe":         func(input *ConfigInput) { input.JobName = "site/build?token=secret" },
		"job too long":       func(input *ConfigInput) { input.JobName = "a" + strings.Repeat("b", 128) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			err := ValidateConfig(input)
			require.ErrorIs(t, err, ErrInvalidConfig)
			for _, secret := range []string{"jenkins-secret", "user-secret", "token=secret"} {
				require.NotContains(t, err.Error(), secret)
			}
		})
	}
}

func TestConfigViewsNeverContainPlaintextToken(t *testing.T) {
	view := ConfigView{ID: 7, Name: "Production", BaseURL: "https://jenkins.example.com", Username: "ci", JobName: "site/build", Enabled: true, TokenConfigured: true}
	require.NotContains(t, strings.ToLower(strings.Join([]string{view.Name, view.BaseURL, view.Username, view.JobName}, "|")), "private-token")
	stored := StoredConfig{ConfigView: view, EncryptedToken: "ciphertext"}
	require.Equal(t, view, stored.ConfigView)
	require.Equal(t, "ciphertext", stored.EncryptedToken)
}
