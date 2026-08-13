package revision

import (
	"testing"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
	"github.com/stretchr/testify/require"
)

func TestComputeHashUsesCanonicalEmptyPreparedContent(t *testing.T) {
	require.Equal(t, "9ebca1a33e28c44890c99e46d508488363522c83f17a31056641ad11b11a153f", ComputeHash(PreparedContent{}))
	require.Equal(t, ComputeHash(PreparedContent{}), ComputeHash(PreparedContent{Tags: []tag.Snapshot{}}))
}
