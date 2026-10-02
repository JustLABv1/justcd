package api

import (
	"net"
	"net/http"
	"time"
)

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Bounded per-process limits. Forwarded headers are intentionally not trusted.
func (s *Server) allowAgentRequest(key string, limit int, window time.Duration) bool {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	return s.allowAgentRequestLocked(key, limit, window)
}
func (s *Server) allowAgentRequestLocked(key string, limit int, window time.Duration) bool {
	now := time.Now()
	cutoff := now.Add(-window)
	if s.agentRequests == nil {
		s.agentRequests = map[string][]time.Time{}
	}
	for k, v := range s.agentRequests {
		if len(v) == 0 || !v[len(v)-1].After(now.Add(-time.Minute)) {
			delete(s.agentRequests, k)
		}
	}
	attempts := s.agentRequests[key][:0]
	for _, v := range s.agentRequests[key] {
		if v.After(cutoff) {
			attempts = append(attempts, v)
		}
	}
	if len(attempts) >= limit {
		s.agentRequests[key] = attempts
		return false
	}
	if len(s.agentRequests) >= 10000 && s.agentRequests[key] == nil {
		return false
	}
	s.agentRequests[key] = append(attempts, now)
	return true
}
func (s *Server) beginAgentPoll(id string) bool {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if s.agentPolls == nil {
		s.agentPolls = map[string]bool{}
	}
	if s.agentPolls[id] || !s.allowAgentRequestLocked("poll:"+id, 120, time.Minute) {
		return false
	}
	s.agentPolls[id] = true
	return true
}
func (s *Server) endAgentPoll(id string) {
	s.agentMu.Lock()
	delete(s.agentPolls, id)
	s.agentMu.Unlock()
}
