package httpapi_test

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestAdminContractContainsAuthenticationOperations(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../../contracts/openapi/admin-v1.yaml")
	require.NoError(t, err)
	require.NoError(t, doc.Validate(context.Background()))
	require.Equal(t, "loginAdmin", doc.Paths.Find("/api/admin/v1/session").Post.OperationID)
	require.NotNil(t, doc.Paths.Find("/api/admin/v1/session").Delete)
	require.NotNil(t, doc.Paths.Find("/api/admin/v1/me").Get)
}

func TestAdminContractDocumentsPasswordByteLimit(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../../contracts/openapi/admin-v1.yaml")
	require.NoError(t, err)
	require.NoError(t, doc.Validate(context.Background()))

	password := doc.Components.Schemas["LoginRequest"].Value.Properties["password"].Value
	require.Equal(t, "UTF-8 encoded value must be at most 256 bytes.", password.Description)
}
