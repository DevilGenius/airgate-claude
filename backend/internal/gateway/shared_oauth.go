package gateway

import (
	"context"
	"encoding/json"
	"time"
)

func (s *oauthSessionStore) consume(ctx context.Context, state string) (*OAuthSession, bool, error) {
	if s.shared != nil {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		raw, found, err := s.shared.Take(ctx, "oauth:"+state)
		if err != nil || !found {
			return nil, false, err
		}
		var session OAuthSession
		if err := json.Unmarshal([]byte(raw), &session); err != nil {
			return nil, false, err
		}
		return &session, time.Since(session.CreatedAt) <= oauthSessionTTL, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, found := s.sessions[state]
	delete(s.sessions, state)
	return session, found, nil
}
