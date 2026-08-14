package httpapi

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/release"
	"github.com/stretchr/testify/require"
)

func TestReleaseLogContextAcceptsOnlyPositiveIDsAndEnumeratedResult(t *testing.T) {
	context, _ := gin.CreateTestContext(nil)
	setReleaseLogContext(context, 7, 12, 41, release.JobBuilding)

	releaseID, ok := ReleaseIDFromLogContext(context)
	require.True(t, ok)
	require.Equal(t, int64(7), releaseID)
	publishJobID, ok := PublishJobIDFromLogContext(context)
	require.True(t, ok)
	require.Equal(t, int64(12), publishJobID)
	buildNumber, ok := JenkinsBuildNumberFromLogContext(context)
	require.True(t, ok)
	require.Equal(t, int64(41), buildNumber)
	result, ok := ResultFromLogContext(context)
	require.True(t, ok)
	require.Equal(t, "building", result)

	invalid, _ := gin.CreateTestContext(nil)
	setReleaseLogContext(invalid, -7, 0, -41, release.JobStatus("callback-error-secret"))
	_, releaseOK := ReleaseIDFromLogContext(invalid)
	_, jobOK := PublishJobIDFromLogContext(invalid)
	_, buildOK := JenkinsBuildNumberFromLogContext(invalid)
	_, resultOK := ResultFromLogContext(invalid)
	require.False(t, releaseOK)
	require.False(t, jobOK)
	require.False(t, buildOK)
	require.False(t, resultOK)
}
