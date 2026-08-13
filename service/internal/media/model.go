package media

import "errors"

type NewMedia struct {
	PublicKey    string
	GFSFileID    int64
	OriginalName string
	MIMEType     string
	FileSize     int64
	Width        int
	Height       int
}

type Reference struct {
	MediaID   int64
	PublicKey string
	Purpose   string
	Position  int
}

var (
	ErrNotFound          = errors.New("media not found")
	ErrInvalidMetadata   = errors.New("invalid media metadata")
	ErrPublicKeyConflict = errors.New("media public key conflict")
	ErrGFSFileIDConflict = errors.New("GFS file ID conflict")
)
