package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"
)

type session struct {
	client *http.Client
	code   string
	at     time.Time
}

const sessionTTL = 15 * time.Minute

var (
	cacheMu sync.Mutex
	cache   = map[string]*session{}
)

func cacheKey(username, password string) string {
	sum := sha256.Sum256([]byte(username + "\x00" + password))
	return hex.EncodeToString(sum[:])
}

func getSeances(username, password, hint, start, end string) ([]seance, error) {
	k := cacheKey(username, password)

	cacheMu.Lock()
	s := cache[k]
	cacheMu.Unlock()

	ensure := func() (*session, error) {
		c, code, err := login(username, password)

		if err != nil {
			return nil, err
		}
		if code == "" {
			code = hint
		}
		if code == "" {
			return nil, errors.New("codePersonne not found; add &codePersonne=... to the URL")
		}

		ns := &session{client: c, code: code, at: time.Now()}
		cacheMu.Lock()
		cache[k] = ns
		cacheMu.Unlock()

		return ns, nil
	}

	var err error
	if s == nil || time.Since(s.at) > sessionTTL {
		if s, err = ensure(); err != nil {
			return nil, err
		}
	}

	seances, err := fetchSeances(s.client, s.code, start, end)
	if errors.Is(err, errUnauth) {
		cacheMu.Lock()
		delete(cache, k)
		cacheMu.Unlock()

		if s, err = ensure(); err != nil {
			return nil, err
		}

		seances, err = fetchSeances(s.client, s.code, start, end)
	}

	return seances, err
}
