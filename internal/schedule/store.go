package schedule

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type store interface {
	get(ctx context.Context, key string) ([]byte, bool, error)
	set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

const storePrefix = "cesi-schedule:"

func newStoreFromEnv(ctx context.Context) (store, error) {
	u := firstNonEmpty(os.Getenv("UPSTASH_REDIS_REST_URL"), os.Getenv("KV_REST_API_URL"))
	t := firstNonEmpty(os.Getenv("UPSTASH_REDIS_REST_TOKEN"), os.Getenv("KV_REST_API_TOKEN"))
	if u == "" || t == "" {
		return nil, errors.New("UPSTASH_REDIS_REST_URL and UPSTASH_REDIS_REST_TOKEN must be set")
	}

	s := &upstashStore{
		url:    strings.TrimRight(u, "/"),
		token:  t,
		client: &http.Client{Timeout: 3 * time.Second},
	}
	if _, err := s.do(ctx, "PING"); err != nil {
		return nil, fmt.Errorf("cannot reach Redis: %w", err)
	}
	return s, nil
}

type upstashStore struct {
	url, token string
	client     *http.Client
}

type upstashResult struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func (s *upstashStore) call(ctx context.Context, path string, cmd, out any) error {
	b, _ := json.Marshal(cmd)
	req, err := http.NewRequestWithContext(ctx, "POST", s.url+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("upstash: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("upstash: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstash: HTTP %d: %.200s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

func (s *upstashStore) do(ctx context.Context, args ...string) (json.RawMessage, error) {
	var r upstashResult
	if err := s.call(ctx, "", args, &r); err != nil {
		return nil, err
	}
	if r.Error != "" {
		return nil, fmt.Errorf("upstash: %s", r.Error)
	}
	return r.Result, nil
}

func (s *upstashStore) get(ctx context.Context, k string) ([]byte, bool, error) {
	res, err := s.do(ctx, "GET", storePrefix+k)
	if err != nil || string(res) == "null" {
		return nil, false, err
	}

	var str string
	if err := json.Unmarshal(res, &str); err != nil {
		return nil, false, err
	}
	v, err := base64.StdEncoding.DecodeString(str)
	return v, err == nil, err
}

func (s *upstashStore) set(ctx context.Context, k string, v []byte, ttl time.Duration) error {
	_, err := s.do(ctx, "SET", storePrefix+k, base64.StdEncoding.EncodeToString(v),
		"PX", strconv.FormatInt(ttl.Milliseconds(), 10))
	return err
}

func (s *upstashStore) incr(ctx context.Context, k string, ttl time.Duration) (int64, error) {
	key := storePrefix + k
	var rs []upstashResult
	err := s.call(ctx, "/pipeline", [][]string{
		{"INCR", key},
		{"PEXPIRE", key, strconv.FormatInt(ttl.Milliseconds(), 10), "NX"},
	}, &rs)
	if err != nil {
		return 0, err
	}
	if len(rs) == 0 || rs[0].Error != "" {
		return 0, fmt.Errorf("upstash: incr failed")
	}

	var n int64
	err = json.Unmarshal(rs[0].Result, &n)
	return n, err
}
