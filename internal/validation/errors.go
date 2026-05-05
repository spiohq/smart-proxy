package validation

import (
	"encoding/json"
	"errors"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
)

const validationDetails = "validated by proxy against SP-API OpenAPI spec"

type spAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details"`
}

type spAPIErrorEnvelope struct {
	Errors []spAPIError `json:"errors"`
}

// FormatValidationErrors converts a kin-openapi validation error into the
// SP-API-compatible {"errors":[...]} JSON envelope.
func FormatValidationErrors(err error) []byte {
	errs := collectErrors(err)
	envelope := spAPIErrorEnvelope{Errors: make([]spAPIError, 0, len(errs))}
	for _, e := range errs {
		envelope.Errors = append(envelope.Errors, spAPIError{
			Code:    "InvalidInput",
			Message: humanMessage(e),
			Details: validationDetails,
		})
	}
	b, _ := json.Marshal(envelope)
	return b
}

func collectErrors(err error) []error {
	var me openapi3.MultiError
	if errors.As(err, &me) {
		out := make([]error, 0, len(me))
		for _, e := range me {
			out = append(out, e)
		}
		return out
	}
	return []error{err}
}

func humanMessage(err error) string {
	var reqErr *openapi3filter.RequestError
	if errors.As(err, &reqErr) {
		return reqErr.Error()
	}
	return err.Error()
}
