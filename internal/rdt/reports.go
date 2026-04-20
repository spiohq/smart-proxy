package rdt

import (
	"sync"
	"time"
)

// restrictedReportTypes is the set of reportType values whose document
// downloads require an RDT. These contain PII such as shipping addresses,
// tax info, or customer invoicing data.
var restrictedReportTypes = map[string]bool{
	// Order reports with PII suffixes
	"GET_FLAT_FILE_ORDER_REPORT_DATA_SHIPPING":            true,
	"GET_FLAT_FILE_ORDER_REPORT_DATA_INVOICING":           true,
	"GET_FLAT_FILE_ORDER_REPORT_DATA_TAX":                 true,
	"GET_ORDER_REPORT_DATA_SHIPPING":                      true,
	"GET_ORDER_REPORT_DATA_INVOICING":                     true,
	"GET_ORDER_REPORT_DATA_TAX":                           true,
	"GET_FLAT_FILE_ACTIONABLE_ORDER_DATA_SHIPPING":        true,
	"GET_FLAT_FILE_ACTIONABLE_ORDER_DATA_INVOICING":       true,
	"GET_FLAT_FILE_ACTIONABLE_ORDER_DATA_TAX":             true,
	"GET_CONVERGED_FLAT_FILE_ORDER_REPORT_DATA":           true,
	// FBA shipments with PII
	"GET_AMAZON_FULFILLED_SHIPMENTS_DATA_INVOICING": true,
	"GET_AMAZON_FULFILLED_SHIPMENTS_DATA_TAX":       true,
	// Tax and invoicing reports
	"GET_EASYSHIP_DOCUMENTS":  true,
	"GET_GST_MTR_B2B_CUSTOM":  true,
	"GET_VAT_TRANSACTION_DATA": true,
	"SC_VAT_TAX_REPORT":       true,
}

// IsRestrictedReportType returns true if the given reportType requires an RDT
// for document download.
func IsRestrictedReportType(reportType string) bool {
	return restrictedReportTypes[reportType]
}

// reportEntry tracks a report's type with an expiry time.
type reportEntry struct {
	reportType string
	expiresAt  time.Time
}

// documentEntry tracks which reportType a document belongs to.
type documentEntry struct {
	reportType string
	expiresAt  time.Time
}

// ReportTracker correlates reportIds to reportTypes and documentIds to
// reportTypes. This enables the middleware to know whether a
// GET /documents/{docId} call requires an RDT.
//
// The tracker is populated by sniffing:
//   - POST /reports request bodies (reportType -> reportId mapping)
//   - GET /reports/{reportId} response bodies (reportId -> documentId mapping)
type ReportTracker struct {
	mu        sync.RWMutex
	reports   map[string]reportEntry   // reportId -> entry
	documents map[string]documentEntry // documentId -> entry
	ttl       time.Duration
}

// NewReportTracker creates a tracker with the given entry TTL.
func NewReportTracker(ttl time.Duration) *ReportTracker {
	return &ReportTracker{
		reports:   make(map[string]reportEntry),
		documents: make(map[string]documentEntry),
		ttl:       ttl,
	}
}

// TrackReportCreation records the reportType for a reportId, learned from
// sniffing a POST /reports request/response.
func (rt *ReportTracker) TrackReportCreation(reportID, reportType string) {
	rt.mu.Lock()
	rt.reports[reportID] = reportEntry{
		reportType: reportType,
		expiresAt:  time.Now().Add(rt.ttl),
	}
	rt.mu.Unlock()
}

// TrackReportDocument links a documentId to its reportId's reportType,
// learned from sniffing a GET /reports/{reportId} response.
// If the reportId was never tracked (e.g. proxy missed the POST), the
// document is tracked without a reportType.
func (rt *ReportTracker) TrackReportDocument(reportID, documentID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	entry, ok := rt.reports[reportID]
	if !ok || time.Now().After(entry.expiresAt) {
		// Unknown or expired report. Track document without type.
		return
	}

	rt.documents[documentID] = documentEntry{
		reportType: entry.reportType,
		expiresAt:  time.Now().Add(rt.ttl),
	}
}

// LookupDocumentReportType returns the reportType for a documentId, if known
// and not expired. The caller should use IsRestrictedReportType to decide
// whether to mint an RDT.
func (rt *ReportTracker) LookupDocumentReportType(documentID string) (string, bool) {
	rt.mu.RLock()
	entry, ok := rt.documents[documentID]
	rt.mu.RUnlock()

	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.reportType, true
}
