package api

import (
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

const userAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0"

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

var errUnauth = errors.New("session expired")

var (
	reForm    = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	reAction  = regexp.MustCompile(`(?i)\baction\s*=\s*"([^"]*)"`)
	reInput   = regexp.MustCompile(`(?is)<input\b[^>]*>`)
	reName    = regexp.MustCompile(`(?i)\bname\s*=\s*"([^"]*)"`)
	reValue   = regexp.MustCompile(`(?i)\bvalue\s*=\s*"([^"]*)"`)
	reCodePer = regexp.MustCompile(`(?i)data-code-personne-courante=["']?(\d+)`)
)

type parsedForm struct {
	action string
	fields url.Values
}

func parseForm(body, base string) (parsedForm, error) {
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

		b, _ := url.Parse(base)
		pf.action = b.ResolveReference(ref).String()
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
	return &http.Client{Jar: jar, Timeout: 30 * time.Second}
}

func do(c *http.Client, method, u string, body url.Values) (respBody, finalURL string, err error) {
	var req *http.Request

	if body != nil {
		req, err = http.NewRequest(method, u, strings.NewReader(body.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		req, err = http.NewRequest(method, u, nil)
	}

	if err != nil {
		return "", "", err
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := c.Do(req)
	if err != nil {
		return "", "", err
	}

	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)

	return string(b), resp.Request.URL.String(), err
}

func login(username, password string) (*http.Client, string, error) {
	c := newClient()

	if _, _, err := do(c, "GET", "https://ent.cesi.fr/identification/wayf", nil); err != nil {
		return nil, "", err
	}

	body, u, err := do(c, "POST", "https://ent.cesi.fr/identification/wayf/",
		url.Values{"login": {username}})
	if err != nil {
		return nil, "", err
	}
	toADFS, err := parseForm(body, u)
	if err != nil || toADFS.fields.Get("SAMLRequest") == "" {
		return nil, "", errors.New("no SAMLRequest returned by WAYF (unknown identifier or non-federated domain?)")
	}

	body, u, err = do(c, "POST", toADFS.action, toADFS.fields)
	if err != nil {
		return nil, "", err
	}
	form, err := parseForm(body, u)
	if err != nil {
		return nil, "", err
	}
	form.fields.Set("UserName", username)
	form.fields.Set("Password", password)
	if form.fields.Get("AuthMethod") == "" {
		form.fields.Set("AuthMethod", "FormsAuthentication")
	}

	body, u, err = do(c, "POST", form.action, form.fields)
	if err != nil {
		return nil, "", err
	}
	toENT, err := parseForm(body, u)
	if err != nil || toENT.fields.Get("SAMLResponse") == "" {
		return nil, "", errors.New("ADFS authentication failed (invalid credentials or unsupported MFA)")
	}

	if _, _, err = do(c, "POST", toENT.action, toENT.fields); err != nil {
		return nil, "", err
	}

	code := ""
	if page, _, e := do(c, "GET", "https://ent.cesi.fr/mon-emploi-du-temps", nil); e == nil {
		if m := reCodePer.FindStringSubmatch(page); m != nil {
			code = m[1]
		}
	}
	return c, code, nil
}

func fetchSeances(c *http.Client, code, start, end string) ([]seance, error) {
	u := fmt.Sprintf(
		"https://ent.cesi.fr/api/seance/all?start=%s&end=%s&codePersonne=%s&_=%d",
		url.QueryEscape(start), url.QueryEscape(end), url.QueryEscape(code),
		time.Now().UnixMilli(),
	)

	body, _, err := do(c, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	var out []seance
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, errUnauth
	}

	return out, nil
}
