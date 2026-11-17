package oauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCallbackPages(t *testing.T) {
	const server = `<b>"linear"&</b>`
	const state = "the-state-value"
	secrets := []string{"the-code-value", "the-description-value", "evil<script>"}
	tests := []struct {
		name   string
		query  url.Values
		status int
		want   []string
		passed bool
	}{
		{"signed in", url.Values{"state": {state}, "code": {"the-code-value"}}, 200,
			[]string{"Signed in to <code>&lt;b&gt;&#34;linear&#34;&amp;&lt;/b&gt;</code>", "close this tab"}, true},
		{"refused", url.Values{"state": {state}, "error": {"access_denied"}, "error_description": {"the-description-value"}}, 200,
			[]string{"<code>&lt;b&gt;&#34;linear&#34;&amp;&lt;/b&gt;</code> was refused", "refused the sign-in: <code>access_denied</code>."}, true},
		{"refused, odd code", url.Values{"state": {state}, "error": {"evil<script>"}}, 200,
			[]string{"The provider refused the sign-in.</p>"}, true},
		{"wrong state", url.Values{"state": {"other"}, "code": {"the-code-value"}}, 400,
			[]string{"isn't the sign-in ctxtra is waiting for"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := make(chan url.Values, 1)
			h := callback(server, state, got)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/callback?"+tt.query.Encode(), nil))
			if rec.Code != tt.status {
				t.Errorf("status %d, want %d", rec.Code, tt.status)
			}
			for k, v := range map[string]string{
				"Content-Type":            "text/html; charset=utf-8",
				"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'",
				"Cache-Control":           "no-store",
			} {
				if g := rec.Header().Get(k); g != v {
					t.Errorf("%s: %q, want %q", k, g, v)
				}
			}
			body := rec.Body.String()
			for _, w := range tt.want {
				if !strings.Contains(body, w) {
					t.Errorf("body lacks %q:\n%s", w, body)
				}
			}
			for _, s := range append(secrets, state, server) {
				if strings.Contains(body, s) {
					t.Errorf("body has %q", s)
				}
			}
			for _, s := range []string{"<script", "<link", "<img", "http://", "https://", "@import", "url("} {
				if strings.Contains(strings.ToLower(body), s) {
					t.Errorf("body has %q", s)
				}
			}
			if passed := len(got) == 1; passed != tt.passed {
				t.Errorf("passed to the sign-in: %v, want %v", passed, tt.passed)
			}
		})
	}
}

func TestCallbackOnlyOnce(t *testing.T) {
	got := make(chan url.Values, 2)
	h := callback("s", "st", got)
	for i, want := range []int{200, 400} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/callback?state=st&code=c", nil))
		if rec.Code != want {
			t.Errorf("request %d: status %d, want %d", i, rec.Code, want)
		}
	}
	if len(got) != 1 {
		t.Errorf("passed %d answers, want 1", len(got))
	}
}
