package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type hashTag struct {
	TagID int64  `json:"tagId"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
}

type hashContent struct {
	Title          string    `json:"title"`
	Summary        string    `json:"summary"`
	CoverPublicKey *string   `json:"coverPublicKey"`
	ContentMD      string    `json:"contentMd"`
	Tags           []hashTag `json:"tags"`
}

func ComputeHash(content PreparedContent) string {
	var coverPublicKey *string
	if content.Cover != nil {
		value := content.Cover.PublicKey
		coverPublicKey = &value
	}
	tags := make([]hashTag, 0, len(content.Tags))
	for _, snapshot := range content.Tags {
		tags = append(tags, hashTag{TagID: snapshot.TagID, Name: snapshot.Name, Slug: snapshot.Slug})
	}
	canonical, _ := json.Marshal(hashContent{
		Title:          content.Title,
		Summary:        content.Summary,
		CoverPublicKey: coverPublicKey,
		ContentMD:      content.ContentMD,
		Tags:           tags,
	})
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}
