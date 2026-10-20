package schedule

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	requestBudget = 28 * time.Second
	linkAttempts  = 5
	linkWindow    = 15 * time.Minute
)

type handler struct {
	svc *service
}

func NewHandler(ctx context.Context) (http.Handler, error) {
	k, err := newKeys(os.Getenv("CESI_SECRET"))
	if err != nil {
		return nil, err
	}

	st, err := newStoreFromEnv(ctx)
	if err != nil {
		return nil, err
	}

	h := &handler{svc: newService(k, st)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { renderPage(w, http.StatusOK, pageData{}) })
	mux.HandleFunc("POST /{$}", h.createLink)
	mux.HandleFunc("GET /calendar.ics", h.calendar)
	return mux, nil
}

func (h *handler) createLink(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	c := creds{Username: strings.TrimSpace(r.PostFormValue("username")), Password: r.PostFormValue("password")}
	data := pageData{Username: c.Username}

	fail := func(status int, msg string) {
		data.Error = msg
		renderPage(w, status, data)
	}

	if c.Username == "" || c.Password == "" {
		fail(http.StatusBadRequest, "Email and password are required.")
		return
	}

	n, err := h.svc.store.incr(r.Context(), "rl:link:"+h.svc.keys.mac(clientIP(r)), linkWindow)
	if err != nil {
		fail(errStatus(storeErr(err)))
		return
	}
	if n > linkAttempts {
		fail(http.StatusTooManyRequests, "Too many attempts, try again in 15 minutes.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestBudget)
	defer cancel()

	if c.Code, err = h.svc.verify(ctx, c); err != nil {
		fail(errStatus(err))
		return
	}

	renderPage(w, http.StatusOK, pageData{Link: baseURL(r) + "/calendar.ics?token=" + h.svc.keys.token(c)})
}

func (h *handler) calendar(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.keys.parseToken(r.URL.Query().Get("token"))
	if err != nil {
		http.Error(w, "Invalid link.", http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestBudget)
	defer cancel()

	body, err := h.svc.calendar(ctx, c)
	if err != nil {
		code, msg := errStatus(err)
		http.Error(w, msg, code)
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=900")
	w.Write(body)
}

func errStatus(err error) (int, string) {
	switch {
	case errors.Is(err, ErrAuth):
		return http.StatusForbidden, "CESI rejected these credentials. Generate a new link with your current password."
	case errors.Is(err, ErrNoCode):
		return http.StatusUnprocessableEntity, "Timetable not found on the CESI portal."
	case errors.Is(err, ErrRateLimited):
		return http.StatusTooManyRequests, "Too many requests, try again later."
	case errors.Is(err, ErrStore):
		log.Print(err)
		return http.StatusServiceUnavailable, "Service unavailable, try again later."
	default:
		log.Print(err)
		return http.StatusBadGateway, "CESI is unavailable, try again later."
	}
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func baseURL(r *http.Request) string {
	proto := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		proto = "https"
	}
	return proto + "://" + r.Host
}
