package store

import "testing"

func TestDefaultRetryPolicy(t *testing.T) {
	policy := DefaultRetryPolicy()
	if !policy.Enabled || policy.MaxAttempts != 5 || policy.InitialDelaySeconds != 5 || policy.MaxDelaySeconds != 300 || policy.JitterPercent != 20 {
		t.Fatalf("unexpected default retry policy: %#v", policy)
	}
}

func TestRetryPolicyValidation(t *testing.T) {
	valid := DefaultRetryPolicy()
	if err := valid.Validate(); err != nil {
		t.Fatalf("default policy should be valid: %v", err)
	}
	invalid := []RetryPolicy{
		{Enabled: true, MaxAttempts: 0, InitialDelaySeconds: 5, MaxDelaySeconds: 300},
		{Enabled: true, MaxAttempts: 21, InitialDelaySeconds: 5, MaxDelaySeconds: 300},
		{Enabled: true, MaxAttempts: 5, InitialDelaySeconds: 10, MaxDelaySeconds: 5},
		{Enabled: true, MaxAttempts: 5, InitialDelaySeconds: 5, MaxDelaySeconds: 300, JitterPercent: 51},
	}
	for index, policy := range invalid {
		if err := policy.Validate(); err == nil {
			t.Errorf("policy %d should be rejected: %#v", index, policy)
		}
	}
}
