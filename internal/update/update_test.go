package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/state"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

// load is what the checks need from the config at path.
func load(t *testing.T, path string) Config {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Parse(data, config.Options{})
	if len(cfg.Errors) > 0 {
		t.Fatalf("errors %v", cfg.Errors)
	}
	return FromConfig(cfg)
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.3.0", "v0.2.3", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "0.99.99", true},
		{"v0.2.3", "v0.2.3", false},
		{"v0.2.2", "v0.2.3", false},
		{"v0.3.0", "dev", false},
		{"v0.3.0", "v0.2.4-0.20261104010203-abcdefabcdef", false},
		{"v0.3.0-rc1", "v0.2.3", false},
		{"v0.3.0-rc.0", "v0.2.3", false},
		{"v0.3.0-beta.1", "v0.2.3", false},
		{"v0.3.0-rc.1", "v0.2.3", true},
		{"v0.3.0-rc.2", "v0.3.0-rc.1", true},
		{"v0.3.0-rc.10", "v0.3.0-rc.9", true},
		{"v0.3.0", "v0.3.0-rc.2", true},
		{"v0.3.0-rc.2", "v0.3.0", false},
		{"v0.3.0-rc.1", "v0.3.0-rc.1", false},
		{"v0.3.1", "v0.4.0-rc.1", false},
		{"v0.4.0-rc.1", "v0.3.1", true},
		{"", "v0.2.3", false},
	} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// release serves answer as GitHub's latest release, and counts requests.
func release(t *testing.T, status int, answer string) *int {
	t.Helper()
	n := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*n++
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "ctxtra/") {
			t.Errorf("User-Agent %q", got)
		}
		w.WriteHeader(status)
		fmt.Fprint(w, answer)
	}))
	t.Cleanup(srv.Close)
	old := releaseURL
	releaseURL = srv.URL
	t.Cleanup(func() { releaseURL = old })
	return n
}

func TestLatestRelease(t *testing.T) {
	testenv.Home(t)
	for _, c := range []struct {
		status        int
		answer, want  string
		wantErrSubstr string
	}{
		{200, `{"tag_name": "v0.3.0", "body": "\u001b[31mignored"}`, "v0.3.0", ""},
		{200, `{"tag_name": "v0.3.0\u001b[31m"}`, "", "no version ctxtra can read"},
		{200, `{"tag_name": "v0.3.0-rc.1"}`, "", "no version ctxtra can read"},
		{200, `not json`, "", "not a release"},
		{404, `{}`, "", "GitHub answered 404"},
	} {
		release(t, c.status, c.answer)
		got, err := latestRelease(context.Background(), "v0.2.3")
		if got != c.want || (c.wantErrSubstr == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), c.wantErrSubstr) {
			t.Errorf("%d %s: %q, %v", c.status, c.answer, got, err)
		}
	}
}

// A ctxtra from the ctxtra-rc formula takes the newer of GitHub's latest
// release and the newest release candidate in the list. Others don't ask
// for the list.
func TestLatestReleaseCandidates(t *testing.T) {
	h := testenv.Home(t)
	var answer string
	lists := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lists++
		fmt.Fprint(w, answer)
	}))
	t.Cleanup(srv.Close)
	oldURL := releasesURL
	releasesURL = srv.URL
	t.Cleanup(func() { releasesURL = oldURL })

	stable := filepath.Join(h, "Cellar", "ctxtra", "0.2.3", "bin", "ctxtra")
	testenv.Write(t, stable, "")
	old := executable
	executable = func() string { return stable }
	t.Cleanup(func() { executable = old })
	release(t, 200, `{"tag_name": "v0.2.3"}`)
	answer = `[{"tag_name": "v0.3.0-rc.1"}]`
	if got, err := latestRelease(context.Background(), "v0.2.3"); got != "v0.2.3" || err != nil || lists != 0 {
		t.Errorf("ctxtra formula: %q, %v, %d lists", got, err, lists)
	}

	rc := filepath.Join(h, "Cellar", "ctxtra-rc", "0.3.0-rc.1", "bin", "ctxtra")
	testenv.Write(t, rc, "")
	executable = func() string { return rc }
	betas := "["
	for i := range 30 {
		betas += fmt.Sprintf(`{"tag_name": "v0.4.0-beta.%d"},`, 30-i)
	}
	betas = strings.TrimSuffix(betas, ",") + "]"
	for _, c := range []struct {
		status                      int
		latest, list, want, wantErr string
	}{
		{200, "v0.2.3", `[{"tag_name": "v0.3.0-rc.2"}, {"tag_name": "v0.3.0-rc.1"}, {"tag_name": "v0.2.3"}]`, "v0.3.0-rc.2", ""},
		{200, "v0.3.0", `[{"tag_name": "v0.3.0"}, {"tag_name": "v0.3.0-rc.2"}]`, "v0.3.0", ""},
		{200, "v0.2.4", `[{"tag_name": "v0.3.0-rc.1"}, {"tag_name": "v0.2.4"}]`, "v0.3.0-rc.1", ""},
		{200, "v0.2.4", `[{"tag_name": "v0.2.4"}, {"tag_name": "v0.2.4-rc.1"}, {"tag_name": "v0.3.0-rc.1"}]`, "v0.3.0-rc.1", ""},
		{200, "v0.3.0", betas, "v0.3.0", ""},
		{200, "v0.3.0", `[{"tag_name": "nightly"}, {"tag_name": "v0.2.0-rc.1"}]`, "v0.3.0", ""},
		{200, "v0.3.0", `[]`, "v0.3.0", ""},
		{200, "v0.3.0", `{"tag_name": "v0.3.0"}`, "", "not a release"},
		{404, "", `[{"tag_name": "v0.3.0-rc.1"}]`, "", "GitHub answered 404"},
	} {
		release(t, c.status, `{"tag_name": "`+c.latest+`"}`)
		answer = c.list
		got, err := latestRelease(context.Background(), "v0.3.0-rc.1")
		if got != c.want || (c.wantErr == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s, %.60s: %q, %v", c.latest, c.list, got, err)
		}
	}
}

// The release list is read page by page until the page of the latest
// release, and only from the same server. A longer list is an error.
func TestNewestCandidatePages(t *testing.T) {
	testenv.Home(t)
	var srv *httptest.Server
	var pages []string
	// Page 1 has only betas, page 2 the latest release, v0.2.4.
	tags := map[string][]string{"2": {"v0.3.0-rc.2", "v0.2.4"}, "3": {"v0.2.4-rc.1"}}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		pages = append(pages, page)
		list := tags[page]
		if page == "1" || page >= "4" {
			for i := range 100 {
				list = append(list, fmt.Sprintf("v0.4.0-beta.%d", i+1))
			}
		}
		n, _ := strconv.Atoi(page)
		next := srv.URL
		if r.URL.Query().Get("other") != "" {
			next = "https://example.com"
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/releases?page=%d>; rel="next", <%s/releases?page=9>; rel="last"`, next, n+1, next))
		rels := make([]string, len(list))
		for i, tag := range list {
			rels[i] = fmt.Sprintf(`{"tag_name": %q}`, tag)
		}
		fmt.Fprint(w, "["+strings.Join(rels, ",")+"]")
	}))
	t.Cleanup(srv.Close)
	old := releasesURL
	t.Cleanup(func() { releasesURL = old })

	releasesURL = srv.URL + "/releases"
	if got, err := newestCandidate(context.Background(), "v0.2.4", "v0.2.4"); got != "v0.3.0-rc.2" || err != nil || strings.Join(pages, ",") != "1,2" {
		t.Errorf("until the latest release: %q, %v, pages %v", got, err, pages)
	}
	pages = nil
	if got, err := newestCandidate(context.Background(), "v0.2.4", "v0.1.0"); got != "" || err == nil || !strings.Contains(err.Error(), "more than 5 pages") || len(pages) != maxReleasePages {
		t.Errorf("no latest release: %q, %v, pages %v", got, err, pages)
	}
	pages = nil
	releasesURL = srv.URL + "/releases?other=1"
	if got, err := newestCandidate(context.Background(), "v0.2.4", "v0.2.4"); got != "" || err != nil || len(pages) != 1 {
		t.Errorf("next page on another server: %q, %v, pages %v", got, err, pages)
	}
}

func TestLatestReleaseTimeout(t *testing.T) {
	testenv.Home(t)
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)
	oldURL, oldTimeout := releaseURL, releaseTimeout
	releaseURL, releaseTimeout = srv.URL, 50*time.Millisecond
	defer func() { releaseURL, releaseTimeout = oldURL, oldTimeout }()
	if _, err := latestRelease(context.Background(), "v0.2.3"); err == nil || !strings.Contains(err.Error(), "didn't answer in time") {
		t.Errorf("timeout: %v", err)
	}
}

// Run records what each check found, and why a check failed.
func TestRun(t *testing.T) {
	h := testenv.Home(t)
	dir := testenv.Repo(t, filepath.Join(h, "srv", "team"), "")
	url := testenv.GitServer(t, filepath.Join(h, "srv")) + "/team"
	headOf := func() string {
		t.Helper()
		out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	headOf()
	testenv.Write(t, filepath.Join(dir, "b.md"), "B")
	testenv.Run(t, dir, "git", "add", "-A")
	testenv.Run(t, dir, "git", "commit", "-q", "-m", "b")
	next := headOf()
	release(t, 200, `{"tag_name": "v0.3.0"}`)
	cfg := load(t, testenv.Config(t, h, `schema: ctxtra/config/v1
sources:
  team: {repo: '`+url+`', branch: main}
  gone: {repo: 'https://127.0.0.1:1/gone', branch: main, check-updates: true}
  quiet: {repo: 'https://127.0.0.1:1/quiet', branch: main, check-updates: false}
  local: {repo: '`+dir+`', branch: main}
  own: {dir: '`+dir+`'}
`))
	results, err := Run(context.Background(), cfg, "v0.2.3", nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range results {
		names = append(names, fmt.Sprintf("%v %s %v", r.Release, r.Name, r.Err != nil))
	}
	if got, want := strings.Join(names, "; "), "true  false; false team false; false gone true; false local false"; got != want {
		t.Errorf("results %q, want %q", got, want)
	}
	st := state.Load()
	if r := st.Ctxtra; r.Latest != "v0.3.0" || r.Checked.IsZero() || r.Attempt.IsZero() || r.Error != "" {
		t.Errorf("release state %+v", r)
	}
	if e := st.Get(source.ID(url), url, "main"); e.Head != next || e.Checked.IsZero() || e.Error != "" || !e.LastCheck.IsZero() {
		t.Errorf("team state %+v", e)
	}
	if e := st.Get(source.ID(dir), dir, "main"); e.Head != next || e.Error != "" {
		t.Errorf("local state %+v", e)
	}
	gone := st.Get(source.ID("https://127.0.0.1:1/gone"), "https://127.0.0.1:1/gone", "main")
	if gone.Error == "" || gone.Head != "" || !gone.Checked.IsZero() || gone.Attempt.IsZero() {
		t.Errorf("failed check state %+v", gone)
	}
	if e := st.Get(source.ID("https://127.0.0.1:1/quiet"), "https://127.0.0.1:1/quiet", "main"); !e.Attempt.IsZero() {
		t.Errorf("a source with checks disabled was checked: %+v", e)
	}

	// A failed check keeps what the last one that worked found.
	release(t, 500, ``)
	if _, err := Run(context.Background(), cfg, "v0.2.3", nil); err != nil {
		t.Fatal(err)
	}
	if r := state.Load().Ctxtra; r.Latest != "v0.3.0" || r.Error == "" || r.Checked.IsZero() {
		t.Errorf("after a failed check %+v", r)
	}

	// A background check checks only what its session claimed.
	requests := release(t, 200, `{"tag_name": "v0.3.0"}`)
	results, err = Run(context.Background(), cfg, "v0.2.3", &Selection{Sources: []string{"team", "quiet"}})
	if err != nil || len(results) != 1 || results[0].Name != "team" || *requests != 0 {
		t.Errorf("selection: %+v, %v, %d release requests", results, err, *requests)
	}
}

func TestStartDue(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv(EnvOff, "")
	var started [][]string
	oldSpawn, oldNow := spawn, now
	spawn = func(args []string) error { started = append(started, args); return nil }
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return at }
	t.Cleanup(func() { spawn, now = oldSpawn, oldNow })
	cfgPath := testenv.Config(t, h, "schema: ctxtra/config/v1\n")
	want := func(name, version string, args ...string) {
		t.Helper()
		n := len(started)
		if err := StartDue(load(t, cfgPath), version); err != nil {
			t.Fatal(err)
		}
		switch {
		case len(args) == 0 && len(started) != n:
			t.Errorf("%s: started %q", name, started[n:])
		case len(args) > 0 && (len(started) != n+1 || strings.Join(started[n], " ") != strings.Join(args, " ")):
			t.Errorf("%s: started %q, want %q", name, started[n:], args)
		}
	}
	release := []string{"check-updates", "--background", "--release"}
	want("first session", "v0.2.3", release...)
	want("second session", "v0.2.3")
	at = at.Add(59 * time.Minute)
	want("before the interval", "v0.2.3")
	at = at.Add(time.Minute)
	want("at one hour", "v0.2.3", release...)

	at = at.Add(2 * Interval)
	t.Setenv(EnvOff, "1")
	want(EnvOff, "v0.2.3")
	t.Setenv(EnvOff, "")
	want("a dev build without source checks", "dev")
	cfgPath = testenv.Config(t, h, "schema: ctxtra/config/v1\ncheck-releases: false\n")
	want("check-releases: false", "v0.2.3")
	// A config that can't be used is no consent to a check: its
	// check-releases: false may be what failed.
	cfgPath = testenv.Config(t, h, "schema: ctxtra/config/v1\ncheck-releases: false\n: [\n")
	data, _ := os.ReadFile(cfgPath)
	if err := StartDue(FromConfig(config.Parse(data, config.Options{})), "v0.2.3"); err != nil || len(started) != 2 {
		t.Errorf("an ignored config: %q, %v", started, err)
	}

	// A source with check-updates on is due on its own, also in a dev build,
	// and the check checks only that source.
	cfgPath = testenv.Config(t, h, "schema: ctxtra/config/v1\nsources:\n  team: {repo: 'https://h/team', branch: main}\n")
	want("a source to check", "dev", "check-updates", "--background", "--source", "team")
	want("the source was just claimed", "dev")
	at = at.Add(Interval)
	want("both due", "v0.2.3", "check-updates", "--background", "--release", "--source", "team")

	// A check that can't start says so, for doctor.
	spawn = func([]string) error { return fmt.Errorf("no such file") }
	at = at.Add(Interval)
	if err := StartDue(load(t, cfgPath), "v0.2.3"); err == nil {
		t.Error("a failed start is no error")
	}
	st := state.Load()
	if e := st.Get(source.ID("https://h/team"), "https://h/team", "main"); e.Error != "can't start the check: no such file" || st.Ctxtra.Error != e.Error {
		t.Errorf("failed start: source %q, release %q", e.Error, st.Ctxtra.Error)
	}
	want("failed start is throttled", "v0.2.3")
	at = at.Add(59 * time.Minute)
	want("failed start before one hour", "v0.2.3")
	// A check that started after the claim keeps its result.
	spawn = func([]string) error {
		if err := state.UpdateRelease(func(r *state.Release) { r.Attempt, r.Error = at.Add(time.Second), "" }); err != nil {
			t.Fatal(err)
		}
		return fmt.Errorf("no such file")
	}
	at = at.Add(time.Minute)
	_ = StartDue(load(t, cfgPath), "v0.2.3")
	if st := state.Load(); st.Ctxtra.Error != "" {
		t.Errorf("a failed start replaced a newer result: %q", st.Ctxtra.Error)
	}

	// Saving the error doesn't make the session wait for a held lock, also
	// with many sources.
	many := "schema: ctxtra/config/v1\nsources:\n"
	for i := range 20 {
		many += fmt.Sprintf("  s%d: {repo: 'https://h/s%d', branch: main, check-updates: true}\n", i, i)
	}
	cfgPath = testenv.Config(t, h, many)
	var unlock func()
	spawn = func([]string) error {
		var err error
		if unlock, err = fsutil.Lock(filepath.Join(paths.StateDir(), "state.lock")); err != nil {
			t.Fatal(err)
		}
		return fmt.Errorf("no such file")
	}
	at = at.Add(Interval)
	start := time.Now()
	_ = StartDue(load(t, cfgPath), "v0.2.3")
	if d := time.Since(start); d > time.Second {
		t.Errorf("StartDue waited %v for the lock", d)
	}
	unlock()
}

// The second of two sessions that start at once finds the first one's
// attempt, also when its own clock reading is a little earlier.
func TestClaimOrder(t *testing.T) {
	testenv.Home(t)
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	claim := func(t0 time.Time) bool {
		t.Helper()
		ok, _, _, err := state.ClaimChecks(true, nil, func() time.Time { return t0 }, Interval, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !claim(at) {
		t.Fatal("first claim")
	}
	if claim(at.Add(-time.Second)) {
		t.Error("an earlier clock reading claimed again")
	}
	if !claim(at.Add(-2 * Interval)) {
		t.Error("after the clock went back by more than the interval, no check")
	}
}

func TestReleaseNotice(t *testing.T) {
	h := testenv.Home(t)
	on := load(t, testenv.Config(t, h, "schema: ctxtra/config/v1\n"))
	st := state.State{Ctxtra: state.Release{Latest: "v0.3.0"}}
	if got := ReleaseNotice(on, st, "v0.2.3"); got != "ctxtra update v0.3.0 is available - "+Releases {
		t.Errorf("newer: %q", got)
	}
	for _, v := range []string{"v0.3.0", "v0.4.0", "dev"} {
		if got := ReleaseNotice(on, st, v); got != "" {
			t.Errorf("%s: %q", v, got)
		}
	}
	// With a known install method, the command is on a second line.
	brew := filepath.Join(h, "Cellar", "ctxtra", "0.2.3", "bin", "ctxtra")
	testenv.Write(t, brew, "")
	old := executable
	executable = func() string { return brew }
	t.Cleanup(func() { executable = old })
	if got := ReleaseNotice(on, st, "v0.2.3"); got != "ctxtra update v0.3.0 is available - "+Releases+"\nrun `brew upgrade atelpis/tap/ctxtra` to update." {
		t.Errorf("Homebrew: %q", got)
	}
	gobin := filepath.Join(h, "gobin", "ctxtra")
	testenv.Write(t, gobin, "")
	t.Setenv("GOBIN", filepath.Dir(gobin))
	executable = func() string { return gobin }
	if got := ReleaseNotice(on, st, "v0.2.3"); got != "ctxtra update v0.3.0 is available - https://github.com/ctxtra/ctxtra/releases\nrun `go install github.com/ctxtra/ctxtra/cmd/ctxtra@latest` to update." {
		t.Errorf("Go: %q", got)
	}
	off := load(t, testenv.Config(t, h, "schema: ctxtra/config/v1\ncheck-releases: false\n"))
	if got := ReleaseNotice(off, st, "v0.2.3"); got != "" {
		t.Errorf("check-releases: false: %q", got)
	}

	// A release candidate is offered only to a ctxtra from the ctxtra-rc
	// formula, with its upgrade command.
	cand := state.State{Ctxtra: state.Release{Latest: "v0.3.0-rc.2"}}
	executable = func() string { return brew }
	if got := ReleaseNotice(on, cand, "v0.2.3"); got != "" {
		t.Errorf("candidate for the ctxtra formula: %q", got)
	}
	rc := filepath.Join(h, "Cellar", "ctxtra-rc", "0.3.0-rc.1", "bin", "ctxtra")
	testenv.Write(t, rc, "")
	executable = func() string { return rc }
	if got := ReleaseNotice(on, cand, "v0.3.0-rc.1"); got != "ctxtra update v0.3.0-rc.2 is available - "+Releases+"\nrun `brew upgrade atelpis/tap/ctxtra-rc` to update." {
		t.Errorf("ctxtra-rc: %q", got)
	}
	if got := ReleaseNotice(on, st, "v0.3.0-rc.1"); got != "ctxtra update v0.3.0 is available - "+Releases+"\nrun `brew upgrade atelpis/tap/ctxtra-rc` to update." {
		t.Errorf("ctxtra-rc, final release: %q", got)
	}
	// A release candidate build compares with final releases too.
	executable = func() string { return gobin }
	if got := ReleaseNotice(on, st, "v0.3.0-rc.1"); got != "ctxtra update v0.3.0 is available - "+Releases+"\nrun `go install github.com/ctxtra/ctxtra/cmd/ctxtra@latest` to update." {
		t.Errorf("Go, candidate build: %q", got)
	}
}

func TestUpgradeCommand(t *testing.T) {
	h := testenv.Home(t)
	brew := filepath.Join(h, "brew", "Cellar", "ctxtra", "0.2.3", "bin", "ctxtra")
	testenv.Write(t, brew, "")
	link := filepath.Join(h, "brew", "bin", "ctxtra")
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(brew, link); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(h, "brew", "Cellar", "ctxtra-rc", "0.3.0-rc.1", "bin", "ctxtra")
	testenv.Write(t, rc, "")
	gobin := filepath.Join(h, "gobin", "ctxtra")
	testenv.Write(t, gobin, "")
	other := filepath.Join(h, "opt", "ctxtra")
	testenv.Write(t, other, "")
	t.Setenv("GOBIN", filepath.Dir(gobin))
	for exe, want := range map[string]string{link: brewUpgrade, rc: brewUpgradeRC, gobin: goInstall, other: "", filepath.Join(h, "none"): ""} {
		if got := upgradeCommand(exe); got != want {
			t.Errorf("%s: %q, want %q", exe, got, want)
		}
	}
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", filepath.Join(h, "gp"))
	testenv.Write(t, filepath.Join(h, "gp", "bin", "ctxtra"), "")
	if got := upgradeCommand(filepath.Join(h, "gp", "bin", "ctxtra")); got != goInstall {
		t.Errorf("GOPATH: %q", got)
	}
}

// The result of a check that started before another one, and ended after
// it, doesn't replace the newer result, however long it took.
func TestRecordDropsSupersededResults(t *testing.T) {
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	clock := at.Add(2 * time.Minute)
	old := now
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = old })
	var c state.Check
	if !Record(&c, at.Add(time.Minute), fmt.Errorf("GitHub answered 500")) || c.Error == "" {
		t.Fatalf("newer failure not recorded: %+v", c)
	}
	if Record(&c, at, nil) || c.Error == "" || !c.Checked.IsZero() || !c.Attempt.Equal(at.Add(time.Minute)) {
		t.Errorf("older success replaced the newer failure: %+v", c)
	}
	// A check that started before a laptop slept for 8 hours.
	if Record(&c, at.Add(-8*time.Hour), nil) || c.Error == "" {
		t.Errorf("a check from before sleep replaced the newer result: %+v", c)
	}
	// After the clock went back, the recorded attempt is in the future: it
	// doesn't count.
	clock = at.Add(-time.Hour)
	if !Record(&c, clock, nil) || c.Error != "" {
		t.Errorf("after the clock went back: %+v", c)
	}
}
