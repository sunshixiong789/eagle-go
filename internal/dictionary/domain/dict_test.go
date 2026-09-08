package domain

import "testing"

func TestStatusEnabled(t *testing.T) {
	if !StatusEnabled.Enabled() {
		t.Fatal("enabled status reported disabled")
	}
	for _, status := range []Status{StatusDisabled, -1, 2} {
		if status.Enabled() {
			t.Errorf("status %d reported enabled", status)
		}
	}
}
