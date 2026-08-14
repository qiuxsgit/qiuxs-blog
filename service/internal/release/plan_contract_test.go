package release_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleasePlansCorrelateEveryJenkinsAttemptByReleaseAndJob(t *testing.T) {
	servicePlan := readReleasePlan(t, "2026-08-13-service-release-jenkins.md")
	for _, fragment := range []string{
		"form fields `RELEASE_ID` and `PUBLISH_JOB_ID`",
		"publishJobID int64",
		"PublishJobID int64 `json:\"publishJobId\"`",
		"locks the exact `(PublishJobID, ReleaseID)` job row",
		"FailTriggerLocked(ctx, job.ID",
	} {
		require.Contains(t, servicePlan, fragment)
	}
	require.NotContains(t, servicePlan, "form field `RELEASE_ID`; ")
	require.NotContains(t, servicePlan, "applies a final failed transition to release the lock")

	deploymentPlan := readReleasePlan(t, "2026-08-13-deployment-pipelines.md")
	for _, fragment := range []string{
		"Parameters `RELEASE_ID` and `PUBLISH_JOB_ID`",
		"releaseId` and `publishJobId`",
		"Test RELEASE_ID/PUBLISH_JOB_ID injection rejection",
	} {
		require.Contains(t, deploymentPlan, fragment)
	}
}

func readReleasePlan(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "superpowers", "plans", name))
	require.NoError(t, err)
	return string(raw)
}
