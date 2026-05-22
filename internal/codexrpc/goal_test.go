package codexrpc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestThreadGoalSetParamsTokenBudgetOptionalNullableJSON(t *testing.T) {
	status := ThreadGoalStatusActive
	objective := "improve coverage"
	params := ThreadGoalSetParams{
		ThreadID:  "thread-1",
		Objective: &objective,
		Status:    &status,
	}
	b, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal(omitted budget) error = %v", err)
	}
	if strings.Contains(string(b), "tokenBudget") {
		t.Fatalf("omitted tokenBudget JSON = %s", b)
	}

	params.TokenBudget = NewNullableInt64(nil)
	b, err = json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal(null budget) error = %v", err)
	}
	if !strings.Contains(string(b), `"tokenBudget":null`) {
		t.Fatalf("null tokenBudget JSON = %s", b)
	}

	budget := int64(12345)
	params.TokenBudget = NewNullableInt64(&budget)
	b, err = json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal(numeric budget) error = %v", err)
	}
	if !strings.Contains(string(b), `"tokenBudget":12345`) {
		t.Fatalf("numeric tokenBudget JSON = %s", b)
	}
}
