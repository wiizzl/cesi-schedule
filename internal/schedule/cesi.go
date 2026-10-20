package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	userAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0"
	entHost   = "ent.cesi.fr"
	wayfURL   = "https://ent.cesi.fr/identification/wayf"
	maxBody   = 8 << 20
)

type seance struct {
	Code    string `json:"code"`
	Title   string `json:"title"`
	Matiere string `json:"matiere"`
	Theme   string `json:"theme"`
	Start   string `json:"start"`
	End     string `json:"end"`
	AllDay  bool   `json:"allDay"`
	Salles  []struct {
		NomSalle string `json:"nomSalle"`
	} `json:"salles"`
	Intervenants []struct {
		Nom    string `json:"nom"`
		Prenom string `json:"prenom"`
	} `json:"intervenants"`
	Participants []struct {
		LibelleGroupe string `json:"libelleGroupe"`
	} `json:"participants"`
}

var (
	ErrAuth     = errors.New("CESI rejected the credentials")
	ErrNoCode   = errors.New("codePersonne not found")
	ErrUpstream = errors.New("CESI is unavailable")
	errUnauth   = errors.New("CESI session expired")
)

func upstreamErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrUpstream, fmt.Sprintf(format, a...))
}

var (
	reForm    = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	reAction  = regexp.MustCompile(`(?i)\baction\s*=\s*"([^"]*)"`)
	reInput   = regexp.MustCompile(`(?is)<input\b[^>]*>`)
	reName    = regexp.MustCompile(`(?i)\bname\s*=\s*"([^"]*)"`)
	reValue   = regexp.MustCompile(`(?i)\bvalue\s*=\s*"([^"]*)"`)
	reCodePer = regexp.MustCompile(`(?i)data-code-personne-courante=["']?(\d+)`)
)

type parsedForm struct {
	action *url.URL
	fields url.Values
}

func parseForm(body string, base *url.URL) (parsedForm, error) {
	m := reForm.FindStringSubmatch(body)
	if m == nil {
		return parsedForm{}, errors.New("no form found in response")
	}

	pf := parsedForm{action: base, fields: url.Values{}}
	if a := reAction.FindStringSubmatch(m[1]); a != nil {
		ref, err := url.Parse(html.UnescapeString(a[1]))
		if err != nil {
			return parsedForm{}, err
		}

		pf.action = base.ResolveReference(ref)
	}

	for _, in := range reInput.FindAllString(m[2], -1) {
		n := reName.FindStringSubmatch(in)
		if n == nil {
			continue
		}

		v := ""
		if vv := reValue.FindStringSubmatch(in); vv != nil {
			v = html.UnescapeString(vv[1])
		}

		pf.fields.Set(html.UnescapeString(n[1]), v)
	}

	return pf, nil
}

func newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 15 * time.Second}
}

type response struct {
	body   string
	url    *url.URL
	status int
}

func do(ctx context.Context, c *http.Client, method, u string, form url.Values) (response, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return response{}, upstreamErr("%v", err)
	}

	req.Header.Set("User-Agent", userAgent)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.Do(req)
	if err != nil {
		return response{}, upstreamErr("%s %s: %v", method, req.URL.Host, err)
	}

	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return response{}, upstreamErr("%s %s: %v", method, req.URL.Host, err)
	}

	return response{body: string(b), url: resp.Request.URL, status: resp.StatusCode}, nil
}

func expectOK(step string, res response) error {
	if res.status != http.StatusOK {
		return upstreamErr("%s: HTTP %d", step, res.status)
	}
	return nil
}

func login(ctx context.Context, username, password string) (*http.Client, string, error) {
	c := newClient()

	if _, err := do(ctx, c, "GET", wayfURL, nil); err != nil {
		return nil, "", err
	}

	res, err := do(ctx, c, "POST", wayfURL+"/", url.Values{"login": {username}})
	if err != nil {
		return nil, "", err
	}
	if err := expectOK("WAYF", res); err != nil {
		return nil, "", err
	}
	toADFS, err := parseForm(res.body, res.url)
	if err != nil {
		return nil, "", upstreamErr("WAYF: %v", err)
	}
	if toADFS.fields.Get("SAMLRequest") == "" {
		return nil, "", ErrAuth
	}

	res, err = do(ctx, c, "POST", toADFS.action.String(), toADFS.fields)
	if err != nil {
		return nil, "", err
	}
	if err := expectOK("ADFS", res); err != nil {
		return nil, "", err
	}
	form, err := parseForm(res.body, res.url)
	if err != nil {
		return nil, "", upstreamErr("ADFS: %v", err)
	}
	if form.action.Scheme != "https" || form.action.Hostname() != res.url.Hostname() {
		return nil, "", upstreamErr("ADFS: unexpected login form target %q", form.action.Host)
	}
	form.fields.Set("UserName", username)
	form.fields.Set("Password", password)
	if form.fields.Get("AuthMethod") == "" {
		form.fields.Set("AuthMethod", "FormsAuthentication")
	}

	res, err = do(ctx, c, "POST", form.action.String(), form.fields)
	if err != nil {
		return nil, "", err
	}
	if err := expectOK("ADFS login", res); err != nil {
		return nil, "", err
	}
	toENT, err := parseForm(res.body, res.url)
	if err != nil {
		return nil, "", upstreamErr("ADFS login: %v", err)
	}
	if toENT.fields.Get("SAMLResponse") == "" {
		return nil, "", ErrAuth
	}
	if toENT.action.Scheme != "https" || toENT.action.Hostname() != entHost {
		return nil, "", upstreamErr("ADFS: unexpected SAML target %q", toENT.action.Host)
	}

	if _, err = do(ctx, c, "POST", toENT.action.String(), toENT.fields); err != nil {
		return nil, "", err
	}

	code := ""
	if page, e := do(ctx, c, "GET", "https://"+entHost+"/mon-emploi-du-temps", nil); e == nil {
		if m := reCodePer.FindStringSubmatch(page.body); m != nil {
			code = m[1]
		}
	}
	return c, code, nil
}

func fetchSeances(ctx context.Context, c *http.Client, code, start, end string) ([]seance, error) {
	u := fmt.Sprintf(
		"https://%s/api/seance/all?start=%s&end=%s&codePersonne=%s&_=%d",
		entHost, url.QueryEscape(start), url.QueryEscape(end), url.QueryEscape(code),
		time.Now().UnixMilli(),
	)

	res, err := do(ctx, c, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	switch {
	case res.status == http.StatusUnauthorized, res.status == http.StatusForbidden:
		return nil, errUnauth
	case res.url.Hostname() != entHost || !strings.HasPrefix(res.url.Path, "/api/"):
		return nil, errUnauth
	case res.status != http.StatusOK:
		return nil, upstreamErr("seances %s..%s: HTTP %d: %.300s", start, end, res.status, res.body)
	}

	var out []seance
	if err := json.Unmarshal([]byte(res.body), &out); err != nil {
		if strings.HasPrefix(strings.TrimSpace(res.body), "<") {
			return nil, errUnauth
		}
		return nil, upstreamErr("seances: %v", err)
	}

	return out, nil
}
