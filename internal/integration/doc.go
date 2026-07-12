// Package integration holds build-tagged (`integration`) tests that exercise
// Skubell's real API/execution path against a live MediaWiki, provisioned via
// docker-compose.test.yml and scripts/provision-test-wiki.sh.
//
// They are skipped unless SKUBELL_TEST_API points at a wiki's api.php:
//
//	SKUBELL_TEST_API=http://localhost:8081/api.php go test -tags integration ./internal/integration/
//	SKUBELL_TEST_API=http://localhost:8082/api.php go test -tags integration ./internal/integration/
//
// The tests create and delete throwaway pages (prefixed "SkubellIT_") and clean
// up after themselves.
package integration
