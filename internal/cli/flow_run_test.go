package cli

import (
	"strings"
	"testing"
)

func TestValidateMailflowFlagsRequiresAtLeastOneSink(t *testing.T) {
	t.Parallel()

	err := validateMailflowFlags(false, false, false, false, "")
	if err == nil || !strings.Contains(err.Error(), "至少需要一个消费目标") {
		t.Fatalf("validateMailflowFlags() error = %v, want sink validation", err)
	}
}

func TestValidateMailflowFlagsRejectsDeleteSourceWithoutWriteBack(t *testing.T) {
	t.Parallel()

	err := validateMailflowFlags(true, false, false, true, "")
	if err == nil || !strings.Contains(err.Error(), "--delete-source 依赖 --write-back") {
		t.Fatalf("validateMailflowFlags() error = %v, want delete-source validation", err)
	}
}

func TestBuildMailflowPlanAddsConfiguredTargets(t *testing.T) {
	t.Parallel()

	plan, err := buildMailflowPlan(true, true, true)
	if err != nil {
		t.Fatalf("buildMailflowPlan() error = %v", err)
	}
	if len(plan.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(plan.Targets))
	}
	if !plan.DeleteSource.Enabled {
		t.Fatalf("DeleteSource.Enabled = false, want true")
	}
	if got := plan.DeleteSource.EligibleConsumers; len(got) != 1 || got[0] != "write-back" {
		t.Fatalf("EligibleConsumers = %+v, want [write-back]", got)
	}
}
