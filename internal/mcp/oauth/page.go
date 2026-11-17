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
<title>{{.Title}} · ctxtra</title>
<style>
:root{--bg:#f6f7f9;--card:#fff;--fg:#1c1e21;--muted:#5d6470;--line:#e2e5ea;--accent:{{.Accent}}}
@media (prefers-color-scheme:dark){:root{--bg:#121417;--card:#1b1e23;--fg:#e8eaed;--muted:#9aa1ab;--line:#2c3038}}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:16px;
background:var(--bg);color:var(--fg);font:16px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif}
main{width:100%;max-width:440px;background:var(--card);border:1px solid var(--line);border-top:4px solid var(--accent);
border-radius:10px;padding:28px 28px 24px}
h1{margin:0 0 12px;font-size:20px;line-height:1.3}
p{margin:0 0 10px;color:var(--muted)}
code{font:600 0.95em ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:var(--fg);overflow-wrap:anywhere}
footer{margin-top:20px;padding-top:12px;border-top:1px solid var(--line);font-size:13px;color:var(--muted)}
</style>
</head>
<body>
<main>
<h1>{{.Heading}}</h1>
{{range .Lines}}<p>{{.}}</p>
{{end}}<footer>ctxtra</footer>
</main>
</body>
</html>
`))

// page is one answer of the redirect listener. Lines are HTML, built
// from escaped values only.
type page struct {
	Status  int
	Title   string
	Heading template.HTML
	Lines   []template.HTML
	Accent  template.CSS
}

const (
	accentOK    template.CSS = "#1f9d55"
	accentError template.CSS = "#d64545"
)

func receivedPage(server string) page {
	return page{
		Status:  http.StatusOK,
		Title:   "Sign-in received",
		Heading: "Sign-in to " + code(server) + " received",
		Lines: []template.HTML{
			"ctxtra is finishing the sign-in. Go back to the terminal to see the result.",
			"You can close this tab.",
		},
		Accent: accentOK,
	}
}

// refusedPage names the provider's error only when it is an OAuth error
// code: the provider controls the text.
func refusedPage(server, errCode string) page {
	why := template.HTML("The provider refused the sign-in.")
	if isCode(errCode) {
		why = "The provider refused the sign-in: " + code(errCode) + "."
	}
	return page{
		Status:  http.StatusOK,
		Title:   "Sign-in refused",
		Heading: "Sign-in to " + code(server) + " refused",
		Lines:   []template.HTML{why, "Nothing was saved. Go back to the terminal, and you can close this tab."},
		Accent:  accentError,
	}
}

var wrongPage = page{
	Status:  http.StatusBadRequest,
	Title:   "Not the expected sign-in",
	Heading: "This isn't the sign-in ctxtra is waiting for",
	Lines: []template.HTML{
		"It is from another attempt, or it was already used. Nothing was saved.",
		"Use the link the terminal shows for the latest sign-in. You can close this tab.",
	},
	Accent: accentError,
}

func code(s string) template.HTML {
	return "<code>" + template.HTML(template.HTMLEscapeString(s)) + "</code>"
}

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
