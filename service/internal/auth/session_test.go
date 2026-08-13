package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sessionStoreFake struct {
	setDigest    string
	setSession   Session
	setTTL       time.Duration
	getDigest    string
	getSession   Session
	getErr       error
	deleteDigest string
	deleteErr    error
}

func (s *sessionStoreFake) Set(_ context.Context, digest string, session Session, ttl time.Duration) error {
	s.setDigest = digest
	s.setSession = session
	s.setTTL = ttl
	return nil
}

func (s *sessionStoreFake) Get(_ context.Context, digest string) (Session, error) {
	s.getDigest = digest
	return s.getSession, s.getErr
}

func (s *sessionStoreFake) Delete(_ context.Context, digest string) error {
	s.deleteDigest = digest
	return s.deleteErr
}

func TestSessionManagerCreateStoresOnlyTokenDigest(t *testing.T) {
	const (
		wantToken  = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
		wantDigest = "ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0"
	)
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	store := &sessionStoreFake{}
	manager := NewSessionManager(store, 2*time.Hour, strings.NewReader(string(bytesFromZeroTo31())), func() time.Time { return now })

	token, session, err := manager.Create(context.Background(), Admin{ID: 42, Username: "qiuxs"})

	require.NoError(t, err)
	require.Equal(t, wantToken, token)
	require.Equal(t, wantDigest, store.setDigest)
	require.NotEqual(t, token, store.setDigest)
	require.Equal(t, Session{AdminID: 42, Username: "qiuxs", ExpiresAt: now.Add(2 * time.Hour)}, session)
	require.Equal(t, session, store.setSession)
	require.Equal(t, 2*time.Hour, store.setTTL)
}

func TestSessionManagerGetRejectsNonCanonicalTokensWithoutStoreAccess(t *testing.T) {
	store := &sessionStoreFake{}
	manager := NewSessionManager(store, time.Hour, strings.NewReader("unused"), time.Now)

	for _, token := range []string{"", "not-base64", "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=", "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8A"} {
		_, err := manager.Get(context.Background(), token)
		require.ErrorIs(t, err, ErrSessionNotFound)
		require.Empty(t, store.getDigest)
	}
}

func TestSessionManagerGetRejectsExpiredStoreSession(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	store := &sessionStoreFake{getSession: Session{AdminID: 42, Username: "qiuxs", ExpiresAt: now}}
	manager := NewSessionManager(store, time.Hour, strings.NewReader("unused"), func() time.Time { return now })

	_, err := manager.Get(context.Background(), "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")

	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManagerDeleteTreatsMissingSessionAsSuccess(t *testing.T) {
	store := &sessionStoreFake{deleteErr: ErrSessionNotFound}
	manager := NewSessionManager(store, time.Hour, strings.NewReader("unused"), time.Now)

	err := manager.Delete(context.Background(), "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")

	require.NoError(t, err)
	require.Equal(t, "ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0", store.deleteDigest)
}

func TestSessionManagerCreateRejectsInvalidAdmin(t *testing.T) {
	manager := NewSessionManager(&sessionStoreFake{}, time.Hour, strings.NewReader("unused"), time.Now)

	for _, admin := range []Admin{{}, {ID: -1, Username: "qiuxs"}, {ID: 1}} {
		_, _, err := manager.Create(context.Background(), admin)
		require.Error(t, err)
		require.False(t, errors.Is(err, ErrSessionNotFound))
	}
}

func TestSessionManagerInvalidConfigurationFailsSafely(t *testing.T) {
	var nilStore *sessionStoreFake
	var nilRandom *strings.Reader
	var nilClock func() time.Time

	for _, manager := range []SessionManager{
		NewSessionManager(&sessionStoreFake{}, 0, strings.NewReader(string(bytesFromZeroTo31())), time.Now),
		NewSessionManager(nilStore, time.Hour, strings.NewReader(string(bytesFromZeroTo31())), time.Now),
		NewSessionManager(&sessionStoreFake{}, time.Hour, nilRandom, time.Now),
		NewSessionManager(&sessionStoreFake{}, time.Hour, strings.NewReader(string(bytesFromZeroTo31())), nilClock),
	} {
		_, _, err := manager.Create(context.Background(), Admin{ID: 1, Username: "qiuxs"})
		require.Error(t, err)
	}
}

func bytesFromZeroTo31() []byte {
	bytes := make([]byte, 32)
	for i := range bytes {
		bytes[i] = byte(i)
	}
	return bytes
}
