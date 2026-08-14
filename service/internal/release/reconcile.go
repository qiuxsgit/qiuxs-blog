package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"
	"unicode/utf8"
)

const maxArtifactBytes = 4 * 1024

type ArtifactReader func() (io.ReadCloser, error)

func ReadArtifact(reader io.Reader) (Artifact, error) {
	if nilReleaseInterface(reader) {
		return Artifact{}, releaseDomain("read deployed release artifact", ErrReconciliationRequired)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxArtifactBytes+1))
	if err != nil {
		return Artifact{}, releaseDependency("read deployed release artifact", err)
	}
	if len(raw) == 0 || len(raw) > maxArtifactBytes || !utf8.Valid(raw) {
		return Artifact{}, releaseDomain("decode deployed release artifact", ErrReconciliationRequired)
	}
	artifact, err := decodeArtifact(raw)
	if err != nil {
		return Artifact{}, releaseDomain("decode deployed release artifact", ErrReconciliationRequired)
	}
	return artifact, nil
}

func decodeArtifact(raw []byte) (Artifact, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return Artifact{}, errors.New("artifact must be an object")
	}
	var artifact Artifact
	seen := make(map[string]struct{}, 4)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return Artifact{}, errors.New("artifact field is invalid")
		}
		name, ok := token.(string)
		if !ok {
			return Artifact{}, errors.New("artifact field is invalid")
		}
		if _, duplicate := seen[name]; duplicate {
			return Artifact{}, errors.New("artifact field is duplicated")
		}
		seen[name] = struct{}{}
		switch name {
		case "releaseId":
			artifact.ReleaseID, err = decodeArtifactID(decoder)
		case "checksum":
			err = decoder.Decode(&artifact.Checksum)
		case "buildNumber":
			artifact.BuildNumber, err = decodeArtifactID(decoder)
		case "deployedAt":
			var encoded string
			if err = decoder.Decode(&encoded); err == nil {
				artifact.DeployedAt, err = time.Parse(time.RFC3339Nano, encoded)
				artifact.DeployedAt = artifact.DeployedAt.UTC()
			}
		default:
			return Artifact{}, errors.New("artifact field is unknown")
		}
		if err != nil {
			return Artifact{}, errors.New("artifact field is invalid")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seen) != 4 {
		return Artifact{}, errors.New("artifact is incomplete")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Artifact{}, errors.New("artifact has trailing data")
	}
	if artifact.ReleaseID <= 0 || artifact.BuildNumber <= 0 || artifact.DeployedAt.IsZero() ||
		!releaseChecksumPattern.MatchString(artifact.Checksum) {
		return Artifact{}, errors.New("artifact values are invalid")
	}
	return artifact, nil
}

func decodeArtifactID(decoder *json.Decoder) (int64, error) {
	token, err := decoder.Token()
	if err != nil {
		return 0, err
	}
	number, ok := token.(json.Number)
	if !ok {
		return 0, errors.New("artifact ID must be a JSON number")
	}
	encoded := number.String()
	if len(encoded) == 0 || encoded[0] < '1' || encoded[0] > '9' {
		return 0, errors.New("artifact ID must be a positive canonical integer")
	}
	for index := 1; index < len(encoded); index++ {
		if encoded[index] < '0' || encoded[index] > '9' {
			return 0, errors.New("artifact ID must be a positive canonical integer")
		}
	}
	return strconv.ParseInt(encoded, 10, 64)
}
