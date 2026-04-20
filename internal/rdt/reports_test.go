package rdt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsRestrictedReportType(t *testing.T) {
	restricted := []string{
		"GET_FLAT_FILE_ORDER_REPORT_DATA_SHIPPING",
		"GET_FLAT_FILE_ORDER_REPORT_DATA_INVOICING",
		"GET_FLAT_FILE_ORDER_REPORT_DATA_TAX",
		"GET_ORDER_REPORT_DATA_SHIPPING",
		"GET_ORDER_REPORT_DATA_INVOICING",
		"GET_ORDER_REPORT_DATA_TAX",
		"GET_FLAT_FILE_ACTIONABLE_ORDER_DATA_SHIPPING",
		"GET_FLAT_FILE_ACTIONABLE_ORDER_DATA_INVOICING",
		"GET_FLAT_FILE_ACTIONABLE_ORDER_DATA_TAX",
		"GET_CONVERGED_FLAT_FILE_ORDER_REPORT_DATA",
		"GET_AMAZON_FULFILLED_SHIPMENTS_DATA_INVOICING",
		"GET_AMAZON_FULFILLED_SHIPMENTS_DATA_TAX",
		"GET_EASYSHIP_DOCUMENTS",
		"GET_GST_MTR_B2B_CUSTOM",
		"GET_VAT_TRANSACTION_DATA",
		"SC_VAT_TAX_REPORT",
	}

	for _, rt := range restricted {
		t.Run(rt, func(t *testing.T) {
			assert.True(t, IsRestrictedReportType(rt), "%s should be restricted", rt)
		})
	}
}

func TestIsRestrictedReportType_NonRestricted(t *testing.T) {
	nonRestricted := []string{
		"GET_FLAT_FILE_OPEN_LISTINGS_DATA",
		"GET_MERCHANT_LISTINGS_ALL_DATA",
		"GET_FBA_MYI_UNSUPPRESSED_INVENTORY_DATA",
		"GET_SALES_AND_TRAFFIC_REPORT",
		"GET_FLAT_FILE_ORDER_REPORT_DATA", // no suffix - not restricted
		"",
	}

	for _, rt := range nonRestricted {
		t.Run(rt, func(t *testing.T) {
			assert.False(t, IsRestrictedReportType(rt), "%s should NOT be restricted", rt)
		})
	}
}

func TestReportTracker_TrackAndLookup(t *testing.T) {
	tracker := NewReportTracker(10 * time.Minute)

	// Step 1: POST /reports -> track reportId -> reportType
	tracker.TrackReportCreation("report-123", "GET_FLAT_FILE_ORDER_REPORT_DATA_SHIPPING")

	// Step 2: GET /reports/{reportId} -> track documentId -> reportType
	tracker.TrackReportDocument("report-123", "doc-456")

	// Step 3: GET /documents/{docId} -> lookup
	reportType, ok := tracker.LookupDocumentReportType("doc-456")
	require.True(t, ok)
	assert.Equal(t, "GET_FLAT_FILE_ORDER_REPORT_DATA_SHIPPING", reportType)
}

func TestReportTracker_UnknownDocument(t *testing.T) {
	tracker := NewReportTracker(10 * time.Minute)

	_, ok := tracker.LookupDocumentReportType("unknown-doc")
	assert.False(t, ok)
}

func TestReportTracker_UnknownReportIdOnDocumentTrack(t *testing.T) {
	tracker := NewReportTracker(10 * time.Minute)

	// Track document for a reportId we never saw in POST /reports
	// This can happen if proxy missed the POST. Should still track
	// but without a reportType.
	tracker.TrackReportDocument("unknown-report", "doc-789")

	_, ok := tracker.LookupDocumentReportType("doc-789")
	assert.False(t, ok, "should not find reportType if POST was missed")
}

func TestReportTracker_Expiry(t *testing.T) {
	tracker := NewReportTracker(1 * time.Millisecond)

	tracker.TrackReportCreation("report-1", "GET_VAT_TRANSACTION_DATA")
	tracker.TrackReportDocument("report-1", "doc-1")

	// Wait for expiry
	time.Sleep(5 * time.Millisecond)

	_, ok := tracker.LookupDocumentReportType("doc-1")
	assert.False(t, ok, "expired entries should not be returned")
}

func TestReportTracker_NonRestrictedReportType(t *testing.T) {
	tracker := NewReportTracker(10 * time.Minute)

	tracker.TrackReportCreation("report-1", "GET_FLAT_FILE_OPEN_LISTINGS_DATA")
	tracker.TrackReportDocument("report-1", "doc-1")

	reportType, ok := tracker.LookupDocumentReportType("doc-1")
	require.True(t, ok)
	assert.Equal(t, "GET_FLAT_FILE_OPEN_LISTINGS_DATA", reportType)
	// The tracker returns the type; the caller decides whether to mint.
	assert.False(t, IsRestrictedReportType(reportType))
}

func TestReportTracker_MultipleDocumentsForSameReport(t *testing.T) {
	tracker := NewReportTracker(10 * time.Minute)

	tracker.TrackReportCreation("report-1", "GET_EASYSHIP_DOCUMENTS")
	tracker.TrackReportDocument("report-1", "doc-A")
	tracker.TrackReportDocument("report-1", "doc-B")

	rtA, ok := tracker.LookupDocumentReportType("doc-A")
	require.True(t, ok)
	assert.Equal(t, "GET_EASYSHIP_DOCUMENTS", rtA)

	rtB, ok := tracker.LookupDocumentReportType("doc-B")
	require.True(t, ok)
	assert.Equal(t, "GET_EASYSHIP_DOCUMENTS", rtB)
}
