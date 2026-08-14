.PHONY: version-check install generate test build

version-check:
	npm run check:node

install:
	npm ci

generate:
	npm run generate:api

test:
	npm run typecheck
	npm run test:run
	node --test scripts/require-node.test.mjs scripts/verify-dist.test.mjs

build:
	npm run check:node
	npm run generate:api
	git diff --exit-code -- src/api/generated/admin.ts
	vite build
	npm run verify:dist
