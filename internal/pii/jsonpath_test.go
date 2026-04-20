package pii

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePath(t *testing.T) {
	t.Run("nested array wildcard", func(t *testing.T) {
		segments := parsePath("$.payload.Orders[*].BuyerInfo.BuyerEmail")
		assert.Equal(t, []string{"payload", "Orders", "[*]", "BuyerInfo", "BuyerEmail"}, segments)
	})

	t.Run("array index", func(t *testing.T) {
		segments := parsePath("$.payload.OrderItems[0].Name")
		assert.Equal(t, []string{"payload", "OrderItems", "[0]", "Name"}, segments)
	})

	t.Run("simple field", func(t *testing.T) {
		segments := parsePath("$.payload.Name")
		assert.Equal(t, []string{"payload", "Name"}, segments)
	})

	t.Run("strip dollar only", func(t *testing.T) {
		segments := parsePath("$payload.Name")
		assert.Equal(t, []string{"payload", "Name"}, segments)
	})

	t.Run("empty path", func(t *testing.T) {
		segments := parsePath("$")
		assert.Equal(t, []string{}, segments)
	})
}

func TestWalkAndApply_SimpleField(t *testing.T) {
	data := map[string]interface{}{
		"payload": map[string]interface{}{
			"Name": "John Doe",
			"Age":  30,
		},
	}

	result := WalkAndApply(data, "$.payload.Name", func(parent map[string]interface{}, key string) bool {
		parent[key] = "[REDACTED]"
		return true
	})

	require.True(t, result)
	payload := data["payload"].(map[string]interface{})
	assert.Equal(t, "[REDACTED]", payload["Name"])
	assert.Equal(t, 30, payload["Age"])
}

func TestWalkAndApply_ArrayWildcard(t *testing.T) {
	data := map[string]interface{}{
		"payload": map[string]interface{}{
			"Orders": []interface{}{
				map[string]interface{}{
					"BuyerInfo": map[string]interface{}{
						"BuyerEmail": "a@example.com",
					},
				},
				map[string]interface{}{
					"BuyerInfo": map[string]interface{}{
						"BuyerEmail": "b@example.com",
					},
				},
			},
		},
	}

	result := WalkAndApply(data, "$.payload.Orders[*].BuyerInfo.BuyerEmail", func(parent map[string]interface{}, key string) bool {
		parent[key] = "[REDACTED]"
		return true
	})

	require.True(t, result)

	payload := data["payload"].(map[string]interface{})
	orders := payload["Orders"].([]interface{})

	order0 := orders[0].(map[string]interface{})
	buyerInfo0 := order0["BuyerInfo"].(map[string]interface{})
	assert.Equal(t, "[REDACTED]", buyerInfo0["BuyerEmail"])

	order1 := orders[1].(map[string]interface{})
	buyerInfo1 := order1["BuyerInfo"].(map[string]interface{})
	assert.Equal(t, "[REDACTED]", buyerInfo1["BuyerEmail"])
}

func TestWalkAndApply_ArrayIndex(t *testing.T) {
	data := map[string]interface{}{
		"payload": map[string]interface{}{
			"Items": []interface{}{
				map[string]interface{}{"Name": "First"},
				map[string]interface{}{"Name": "Second"},
				map[string]interface{}{"Name": "Third"},
			},
		},
	}

	result := WalkAndApply(data, "$.payload.Items[1].Name", func(parent map[string]interface{}, key string) bool {
		parent[key] = "[REDACTED]"
		return true
	})

	require.True(t, result)

	payload := data["payload"].(map[string]interface{})
	items := payload["Items"].([]interface{})

	item0 := items[0].(map[string]interface{})
	assert.Equal(t, "First", item0["Name"])

	item1 := items[1].(map[string]interface{})
	assert.Equal(t, "[REDACTED]", item1["Name"])

	item2 := items[2].(map[string]interface{})
	assert.Equal(t, "Third", item2["Name"])
}

func TestWalkAndApply_DeleteField(t *testing.T) {
	data := map[string]interface{}{
		"payload": map[string]interface{}{
			"Secret": "sensitive-data",
			"Public": "visible-data",
		},
	}

	result := WalkAndApply(data, "$.payload.Secret", func(parent map[string]interface{}, key string) bool {
		delete(parent, key)
		return true
	})

	require.True(t, result)

	payload := data["payload"].(map[string]interface{})
	_, secretExists := payload["Secret"]
	assert.False(t, secretExists, "Secret should have been deleted")
	assert.Equal(t, "visible-data", payload["Public"])
}

func TestWalkAndApply_NonexistentPath_ReturnsFalse(t *testing.T) {
	data := map[string]interface{}{
		"payload": map[string]interface{}{},
	}

	result := WalkAndApply(data, "$.payload.Orders[*].BuyerEmail", func(parent map[string]interface{}, key string) bool {
		parent[key] = "[REDACTED]"
		return true
	})

	assert.False(t, result)
}

func TestWalkAndApply_EmptyArray_ReturnsFalse(t *testing.T) {
	data := map[string]interface{}{
		"payload": map[string]interface{}{
			"Orders": []interface{}{},
		},
	}

	result := WalkAndApply(data, "$.payload.Orders[*].Name", func(parent map[string]interface{}, key string) bool {
		parent[key] = "[REDACTED]"
		return true
	})

	assert.False(t, result)
}
