package oauth

import (
	"bytes"
	"html/template"
	"net/http"
)

// signInPage is the page the browser shows when the provider redirects to
// the listener. It is self-contained so that the page loads nothing.
var signInPage = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>{{.Title}}</title>
<style>
:root{--bg:#f6f7f9;--fg:#1c1e21;--muted:#5d6470}
@media (prefers-color-scheme:dark){:root{--bg:#121417;--fg:#e8eaed;--muted:#9aa1ab}}
body{margin:0;min-height:100vh;display:flex;flex-direction:column;align-items:center;justify-content:center;
padding:16px;box-sizing:border-box;text-align:center;background:var(--bg);color:var(--fg);
font:16px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif}
h1{margin:0 0 8px;font-size:24px;font-weight:600}
p{margin:0;color:var(--muted)}
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Subtitle}}</p>
</body>
</html>
`))

// page is one answer of the redirect listener.
type page struct {
	Status   int
	Title    string
	Subtitle string
}

var signedInPage = page{http.StatusOK, "Sign-in successful", "You can close this window and return to the terminal."}

// refusedPage names the provider's error only when it is an OAuth error
// code: the provider controls the text.
func refusedPage(errCode string) page {
	sub := "The provider refused the sign-in."
	if isCode(errCode) {
		sub = "The provider refused the sign-in: " + errCode + "."
	}
	return page{http.StatusOK, "Sign-in failed", sub}
}

var wrongPage = page{http.StatusBadRequest, "Sign-in failed",
	"This link is from another sign-in, or it was already used. Use the link the terminal shows."}

func (p page) write(w http.ResponseWriter) {
	var b bytes.Buffer
	if err := signInPage.Execute(&b, p); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(p.Status)
	_, _ = b.WriteTo(w)
}
