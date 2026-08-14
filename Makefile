.PHONY: test-service test-admin test-site test-deploy verify

test-service:
	$(MAKE) -C service test

test-admin:
	$(MAKE) -C admin test

test-site:
	cd site && npm test && npm run check

test-deploy:
	bash deploy/tests/deploy_scripts.sh
	bash deploy/tests/nginx_test.sh
	bash deploy/tests/repository_gate_test.sh

verify: test-service test-admin test-site test-deploy
