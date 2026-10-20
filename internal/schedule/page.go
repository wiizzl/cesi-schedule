package schedule

import (
	"html/template"
	"log"
	"net/http"
)

type pageData struct {
	Username, Error, Link string
}

var page = template.Must(template.New("page").Parse(`<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>CESI Schedule</title>
<style>
:root { color-scheme: light dark; }
body { max-width: 24rem; margin: 20vh auto; padding: 0 1rem; font-family: system-ui, sans-serif; }
input, button { display: block; width: 100%; box-sizing: border-box; margin-bottom: .5rem; padding: .6rem; font: inherit; border: 1px solid #8886; border-radius: 6px; }
button { cursor: pointer; }
a { word-break: break-all; }
</style>
{{if .Link}}<a href="{{.Link}}">{{.Link}}</a>
{{else}}{{with .Error}}<p>{{.}}</p>{{end}}
<form method="post" action="/">
<input name="username" type="email" placeholder="Email" autocomplete="username" required value="{{.Username}}">
<input name="password" type="password" placeholder="Password" autocomplete="current-password" required>
<button>OK</button>
</form>
{{end}}`))

func renderPage(w http.ResponseWriter, status int, d pageData) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(status)
	if err := page.Execute(w, d); err != nil {
		log.Printf("render: %v", err)
	}
}
