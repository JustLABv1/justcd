package api

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginLimitsAreAccountScoped(t *testing.T) {
	s := &Server{}
	for i := 0; i < 8; i++ {
		if !s.allowLogin("account:attacker@example.invalid") {
			t.Fatal("early limit")
		}
	}
	if s.allowLogin("account:attacker@example.invalid") {
		t.Fatal("limit bypass")
	}
	if !s.allowLogin("account:other@example.invalid") {
		t.Fatal("other account blocked")
	}
	s.clearLogin("account:attacker@example.invalid")
	if !s.allowLogin("account:attacker@example.invalid") {
		t.Fatal("reset failed")
	}
}
func TestAgentPollLimits(t *testing.T) {
	s := &Server{}
	if !s.beginAgentPoll("a") || s.beginAgentPoll("a") {
		t.Fatal("parallel poll not bounded")
	}
	if !s.beginAgentPoll("b") {
		t.Fatal("other agent blocked")
	}
	s.endAgentPoll("a")
	if !s.beginAgentPoll("a") {
		t.Fatal("released poll blocked")
	}
	s.endAgentPoll("a")
	for i := 0; i < 118; i++ {
		if !s.beginAgentPoll("a") {
			t.Fatal("early rate limit")
		}
		s.endAgentPoll("a")
	}
	if s.beginAgentPoll("a") {
		t.Fatal("rate limit bypass")
	}
	s.agentRequests["poll:a"] = []time.Time{time.Now().Add(-2 * time.Minute)}
	if !s.beginAgentPoll("a") {
		t.Fatal("expired rate limit retained")
	}
}
func TestEnrollmentLimitsIgnoreSourcePortAndForwardedHeaders(t *testing.T) {
	s := &Server{}
	for i := 0; i < 10; i++ {
		if !s.allowAgentRequest("enroll:192.0.2.1", 10, time.Minute) {
			t.Fatal("early limit")
		}
	}
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.0.2.1:54321"
	r.Header.Set("X-Forwarded-For", "192.0.2.2")
	if s.allowAgentRequest("enroll:"+remoteHost(r), 10, time.Minute) {
		t.Fatal("source port/header bypass")
	}
}
