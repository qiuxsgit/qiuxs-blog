.PHONY: version-check generate test build

version-check:
	@test "$$(go env GOVERSION)" = "go1.25.7" || (echo "Go 1.25.7 required, got $$(go env GOVERSION)" && exit 1)

generate:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 --config oapi-codegen.yaml ../contracts/openapi/admin-v1.yaml

test:
	go test ./...

build: version-check test
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=$${GOARCH:?GOARCH must be set} go build -trimpath -ldflags="-s -w" -o build/blog-service ./cmd/blog-service
