package ui

import (
	"testing"

	"github.com/gcg/agenttree/internal/state"
)

func TestValidateNewFeatureName(t *testing.T) {
	existing := []state.Feature{{Name: "auth-flow", Branch: "auth-flow"}}
	if err := ValidateFeatureName("", "", existing); err == nil {
		t.Fatal("expected empty name to fail")
	}
	if err := ValidateFeatureName("auth-flow", "", existing); err == nil {
		t.Fatal("expected duplicate feature name to fail")
	}
	if err := ValidateFeatureName("payments api", "feat/", existing); err != nil {
		t.Fatalf("expected valid name to pass, got %v", err)
	}
}
