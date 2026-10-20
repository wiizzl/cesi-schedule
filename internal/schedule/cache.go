package schedule

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
	_ "time/tzdata"
)

const (
	sessionTTL  = 15 * time.Minute
	cacheTTL    = 15 * time.Minute
	authFailTTL = 6 * time.Hour

	maxLogins           = 8
	maxRefreshesPerHour = 30

	weeksBefore = 1
	weeksAfter  = 7
	chunkDays   = 28
)

var (
	ErrRateLimited = errors.New("too many requests")
	ErrStore       = errors.New("storage unavailable")

	paris, _ = time.LoadLocation("Europe/Paris")
)

func storeErr(err error) error { return fmt.Errorf("%w: %v", ErrStore, err) }

type creds struct {
	Username string `json:"u"`
	Password string `json:"p"`
	Code     string `json:"c,omitempty"`
}

type session struct {
	client *http.Client
	code   string
	at     time.Time
}

type service struct {
	keys     *keys
	store    store
	logins   chan struct{}
	mu       sync.Mutex
	sessions map[string]*session
}

func newService(k *keys, st store) *service {
	return &service{keys: k, store: st, logins: make(chan struct{}, maxLogins), sessions: map[string]*session{}}
}

func (s *service) userKey(c creds) string { return s.keys.mac(c.Username, c.Password) }

func (s *service) checkAuth(ctx context.Context, uk string) error {
	_, failed, err := s.store.get(ctx, "authfail:"+uk)
	if err != nil {
		return storeErr(err)
	}
	if failed {
		return ErrAuth
	}
	return nil
}

func (s *service) verify(ctx context.Context, c creds) (string, error) {
	uk := s.userKey(c)
	if err := s.checkAuth(ctx, uk); err != nil {
		return "", err
	}

	sess, err := s.login(ctx, uk, c)
	if err != nil {
		return "", err
	}
	return sess.code, nil
}

func (s *service) calendar(ctx context.Context, c creds) ([]byte, error) {
	uk := s.userKey(c)
	if err := s.checkAuth(ctx, uk); err != nil {
		return nil, err
	}

	if b, ok, err := s.load(ctx, "ics:"+uk); err != nil || ok {
		return b, err
	}

	body, err := s.refresh(ctx, uk, c)
	if err != nil {
		return nil, err
	}
	s.save(ctx, "ics:"+uk, body, cacheTTL)
	return body, nil
}

func (s *service) refresh(ctx context.Context, uk string, c creds) ([]byte, error) {
	n, err := s.store.incr(ctx, "rl:"+uk, time.Hour)
	if err != nil {
		return nil, storeErr(err)
	}
	if n > maxRefreshesPerHour {
		return nil, ErrRateLimited
	}

	sess := s.session(uk)
	if sess == nil {
		if sess, err = s.login(ctx, uk, c); err != nil {
			return nil, err
		}
	}

	var all []seance
	for _, r := range window(time.Now()) {
		out, err := fetchSeances(ctx, sess.client, sess.code, r[0], r[1])
		if errors.Is(err, errUnauth) {
			if sess, err = s.login(ctx, uk, c); err != nil {
				return nil, err
			}
			out, err = fetchSeances(ctx, sess.client, sess.code, r[0], r[1])
		}
		if err != nil {
			return nil, err
		}
		all = append(all, out...)
	}

	return []byte(toICS(all)), nil
}

func window(now time.Time) [][2]string {
	now = now.In(paris)
	start := time.Date(now.Year(), now.Month(), now.Day()-(int(now.Weekday())+6)%7-7*weeksBefore, 0, 0, 0, 0, paris)
	end := start.AddDate(0, 0, 7*(weeksBefore+weeksAfter)-1)

	var out [][2]string
	for from := start; !from.After(end); from = from.AddDate(0, 0, chunkDays) {
		to := from.AddDate(0, 0, chunkDays-1)
		if to.After(end) {
			to = end
		}
		out = append(out, [2]string{from.Format(time.DateOnly), to.Format(time.DateOnly)})
	}
	return out
}

func (s *service) login(ctx context.Context, uk string, c creds) (*session, error) {
	select {
	case s.logins <- struct{}{}:
		defer func() { <-s.logins }()
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %v", ErrUpstream, ctx.Err())
	}

	client, code, err := login(ctx, c.Username, c.Password)
	if errors.Is(err, ErrAuth) {
		if err := s.store.set(ctx, "authfail:"+uk, []byte{1}, authFailTTL); err != nil {
			log.Print(err)
		}
	}
	if err != nil {
		return nil, err
	}

	if code == "" {
		code = c.Code
	}
	if code == "" {
		return nil, ErrNoCode
	}

	sess := &session{client: client, code: code, at: time.Now()}
	s.mu.Lock()
	for k, v := range s.sessions {
		if time.Since(v.at) > sessionTTL {
			delete(s.sessions, k)
		}
	}
	s.sessions[uk] = sess
	s.mu.Unlock()
	return sess, nil
}

func (s *service) session(uk string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess := s.sessions[uk]; sess != nil && time.Since(sess.at) < sessionTTL {
		return sess
	}
	return nil
}

func (s *service) load(ctx context.Context, key string) ([]byte, bool, error) {
	b, ok, err := s.store.get(ctx, key)
	if err != nil {
		return nil, false, storeErr(err)
	}
	if !ok {
		return nil, false, nil
	}
	plain, err := s.keys.open(b, key)
	return plain, err == nil, nil
}

func (s *service) save(ctx context.Context, key string, plain []byte, ttl time.Duration) {
	if err := s.store.set(ctx, key, s.keys.seal(plain, key), ttl); err != nil {
		log.Print(err)
	}
}
