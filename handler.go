package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	usage      = "GET /?username=<email>&password=<pwd>[&weeks=4][&start=YYYY-MM-DD&end=YYYY-MM-DD][&codePersonne=...]\n"
	dateLayout = "2006-01-02"
)

func errBadParam(msg string) error { return errors.New(msg) }

func resolveRange(q url.Values) (start, end string, err error) {
	now := time.Now()
	offset := (int(now.Weekday()) + 6) % 7
	startT := now.AddDate(0, 0, -offset)

	if s := q.Get("start"); s != "" {
		if startT, err = time.Parse(dateLayout, s); err != nil {
			return "", "", errBadParam("start must be a valid date in YYYY-MM-DD format")
		}
	}
	start = startT.Format(dateLayout)

	if e := q.Get("end"); e != "" {
		endT, perr := time.Parse(dateLayout, e)
		if perr != nil {
			return "", "", errBadParam("end must be a valid date in YYYY-MM-DD format")
		}
		return start, endT.Format(dateLayout), nil
	}

	weeks := 4
	if w := q.Get("weeks"); w != "" {
		n, convErr := strconv.Atoi(w)
		if convErr != nil || n < 1 || n > 52 {
			return "", "", errBadParam("weeks must be an integer between 1 and 52")
		}
		weeks = n
	}
	end = startT.AddDate(0, 0, weeks*7-1).Format(dateLayout)

	return start, end, nil
}

func HandleICS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	q := r.URL.Query()
	username, password := q.Get("username"), q.Get("password")
	if username == "" || password == "" {
		http.Error(w, "missing required query parameters 'username' and 'password'\n\n"+usage, http.StatusBadRequest)
		return
	}

	start, end, err := resolveRange(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	seances, err := getSeances(username, password, q.Get("codePersonne"), start, end)
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="edt-cesi.ics"`)
	w.Header().Set("Cache-Control", "private, max-age=900")
	io.WriteString(w, toICS(seances))
}

func statusFor(err error) int {
	switch msg := err.Error(); {
	case strings.Contains(msg, "ADFS"), strings.Contains(msg, "SAMLRequest"):
		return http.StatusUnauthorized
	case strings.Contains(msg, "codePersonne"):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusBadGateway
	}
}
