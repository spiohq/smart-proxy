//go:build e2e

package e2e

import (
	"testing"
)

// security_test.go is the single place an operator can grep for "security"
// to find the pentest-remediation regression net. Each test points at the
// existing unit-level coverage that already protects the property; the e2e
// counterparts here are kept as t.Skip tombstones so adding a true
// integration-level proof later only requires fleshing out one body.
//
// All files are pentest-remediation-plan tasks; commit SHAs are listed for
// quick git-blame access.

// TestE2E_F02_PostMessagingBodyRedacted: write-side schema-PII redaction.
// Covered by:
//   - internal/logging/middleware_test.go::TestLoggingMiddleware_PostMessagingRequestBodyRedacted
//   - internal/logging/middleware_test.go::TestLoggingMiddleware_PostMfnRequestBody_OnlyShipFromAddressRedacted
//   - internal/logging/middleware_test.go::TestLoggingMiddleware_FailClosed_UnknownPostBody_FallsBackToFullBodyPlaceholder
//   - internal/pii/registry_test.go (rule + accessor coverage)
//   - internal/pii/engine_test.go (RedactRequestBodyForLogging)
func TestE2E_F02_PostMessagingBodyRedacted(t *testing.T) {
	t.Skip("Captured by internal/logging/middleware_test.go and internal/pii unit tests")
}

// TestE2E_F15_DashboardReadSideRedaction: legacy unredacted JSONL entries
// must be redacted on serve via /api/v1/logs/{id}/body.
// Covered by:
//   - internal/dashboard/handler_test.go::TestHandleLogBody_ReadSideRedaction_LegacyMfnRequestBody
//   - internal/dashboard/handler_test.go::TestHandleLogBody_ReadSideRedaction_LegacyMessagingRequestBody
//   - internal/dashboard/handler_test.go::TestHandleLogBody_ReadSideRedaction_LegacyOrdersV0ResponseBody
//   - internal/dashboard/handler_test.go::TestHandleLogBody_ReadSideRedaction_LegacyFullBodyPIIResponse
//   - internal/dashboard/handler_test.go::TestHandleLogBody_NoEngine_BodyReturnedVerbatim
func TestE2E_F15_DashboardReadSideRedaction(t *testing.T) {
	t.Skip("Captured by internal/dashboard/handler_test.go ReadSide* tests")
}

// TestE2E_F06_SlowlorisHeadersClosed: the http.Server's ReadHeaderTimeout
// closes a slow-header connection within 10s.
// Covered by:
//   - internal/server/server_test.go::TestRegionServer_ReadHeaderTimeout
func TestE2E_F06_SlowlorisHeadersClosed(t *testing.T) {
	t.Skip("Captured by internal/server/server_test.go::TestRegionServer_ReadHeaderTimeout")
}

// TestE2E_F11_ThrottleModeHeaderClamped: the X-SP-Proxy-Throttle-Mode header
// cannot lengthen the wait beyond cfg.QueueTimeout or upgrade past an
// operator-chosen reject ceiling.
// Covered by:
//   - internal/ratelimit/middleware_internal_test.go::TestResolveTimeout_ClampsHeaderToConfigCeiling
//   - internal/ratelimit/middleware_internal_test.go::TestResolveThrottleMode_RejectIsCeiling_*
func TestE2E_F11_ThrottleModeHeaderClamped(t *testing.T) {
	t.Skip("Captured by internal/ratelimit/middleware_internal_test.go")
}

// TestE2E_F13_RDTSingleflight_DifferentTokens: two concurrent callers with
// the same merchant header but different LWA tokens must mint independently.
// Covered by:
//   - internal/rdt/middleware_test.go::TestMiddleware_Singleflight_DifferentTokens_SameMerchant_MintIndependently
func TestE2E_F13_RDTSingleflight_DifferentTokens(t *testing.T) {
	t.Skip("Captured by internal/rdt/middleware_test.go")
}

// TestE2E_F09_GzipBombDefense: decompressIfGzipBounded falls back to the
// compressed bytes when the decompressed output exceeds the cap.
// Covered by:
//   - internal/logging/middleware_test.go::TestDecompressIfGzipBounded_BombDefenseFallsBackToCompressed
func TestE2E_F09_GzipBombDefense(t *testing.T) {
	t.Skip("Captured by internal/logging/middleware_test.go")
}
