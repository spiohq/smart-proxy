package pii

import (
	"strconv"
	"strings"
)

// parsePath splits a JSONPath string into segments.
// "$.payload.Orders[*].BuyerInfo.BuyerEmail" → ["payload", "Orders", "[*]", "BuyerInfo", "BuyerEmail"]
func parsePath(path string) []string {
	// Strip leading "$." or "$"
	if strings.HasPrefix(path, "$.") {
		path = path[2:]
	} else if strings.HasPrefix(path, "$") {
		path = path[1:]
	}

	if path == "" {
		return []string{}
	}

	// Split on "."
	dotParts := strings.Split(path, ".")

	var segments []string
	for _, part := range dotParts {
		if part == "" {
			continue
		}
		// Check if this part contains a bracket expression
		if idx := strings.Index(part, "["); idx != -1 {
			// Field name before the bracket
			fieldName := part[:idx]
			if fieldName != "" {
				segments = append(segments, fieldName)
			}
			// Bracket expression (e.g. "[*]" or "[0]")
			bracket := part[idx:]
			segments = append(segments, bracket)
		} else {
			segments = append(segments, part)
		}
	}

	return segments
}

// WalkAndApply traverses the JSON data at the given path and applies fn to each matched value.
// Returns true if any values were found and the function returned true.
func WalkAndApply(data interface{}, path string, fn func(parent map[string]interface{}, key string) bool) bool {
	segments := parsePath(path)
	return walkSegments(data, segments, fn)
}

// walkSegments is the recursive helper for WalkAndApply.
func walkSegments(data interface{}, segments []string, fn func(parent map[string]interface{}, key string) bool) bool {
	if len(segments) == 0 {
		return false
	}

	seg := segments[0]
	rest := segments[1:]

	// Array wildcard segment
	if seg == "[*]" {
		arr, ok := data.([]interface{})
		if !ok {
			return false
		}
		found := false
		for _, elem := range arr {
			if walkSegments(elem, rest, fn) {
				found = true
			}
		}
		return found
	}

	// Array index segment
	if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
		indexStr := seg[1 : len(seg)-1]
		n, err := strconv.Atoi(indexStr)
		if err != nil {
			return false
		}
		arr, ok := data.([]interface{})
		if !ok {
			return false
		}
		if n < 0 || n >= len(arr) {
			return false
		}
		return walkSegments(arr[n], rest, fn)
	}

	// Field segment
	obj, ok := data.(map[string]interface{})
	if !ok {
		return false
	}

	// Last segment: call fn if key exists
	if len(rest) == 0 {
		if _, exists := obj[seg]; exists {
			return fn(obj, seg)
		}
		return false
	}

	// Not last segment: descend into nested field
	child, exists := obj[seg]
	if !exists {
		return false
	}
	return walkSegments(child, rest, fn)
}
