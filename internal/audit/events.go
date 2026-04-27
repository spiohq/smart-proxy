package audit

// EventDPPComplianceWarning is the audit-event type emitted at startup for
// each operator configuration choice that weakens DPP/AUP posture in
// production mode. Defined as a constant so audit-log queries against the
// audit_log table are stable across releases.
const EventDPPComplianceWarning = "dpp_compliance_warning"
