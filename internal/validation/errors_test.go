package validation_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spiohq/smart-proxy/internal/validation"
)

func TestFormatValidationErrors_Single(t *testing.T) {
	err := &openapi3filter.RequestError{
		Input: &openapi3filter.RequestValidationInput{},
		Err:   fmt.Errorf("missing required query parameter: marketplaceIds"),
	}
	body := validation.FormatValidationErrors(err)
	var envelope struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details string `json:"details"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	require.Len(t, envelope.Errors, 1)
	assert.Equal(t, "InvalidInput", envelope.Errors[0].Code)
	assert.Contains(t, envelope.Errors[0].Message, "marketplaceIds")
	assert.Equal(t, "validated by proxy against SP-API OpenAPI spec", envelope.Errors[0].Details)
}

func TestFormatValidationErrors_Multi(t *testing.T) {
	me := openapi3.MultiError{
		&openapi3filter.RequestError{
			Input: &openapi3filter.RequestValidationInput{},
			Err:   fmt.Errorf("missing required query parameter: marketplaceIds"),
		},
		&openapi3filter.RequestError{
			Input: &openapi3filter.RequestValidationInput{},
			Err:   fmt.Errorf("missing required query parameter: createdAfter"),
		},
	}
	body := validation.FormatValidationErrors(me)
	var envelope struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details string `json:"details"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	assert.Len(t, envelope.Errors, 2)
	assert.Equal(t, "InvalidInput", envelope.Errors[0].Code)
	assert.Equal(t, "InvalidInput", envelope.Errors[1].Code)
	assert.Equal(t, "validated by proxy against SP-API OpenAPI spec", envelope.Errors[0].Details)
}

func TestFormatValidationErrors_MessageIsHumanReadable(t *testing.T) {
	err := &openapi3filter.RequestError{
		Input: &openapi3filter.RequestValidationInput{},
		Err:   fmt.Errorf("parameter \"pageSize\" in query has an error: value is not a valid integer"),
	}
	body := validation.FormatValidationErrors(err)
	var envelope struct {
		Errors []struct{ Message string `json:"message"` } `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	require.Len(t, envelope.Errors, 1)
	assert.NotContains(t, envelope.Errors[0].Message, "RequestError")
	assert.NotContains(t, envelope.Errors[0].Message, "&{")
}
