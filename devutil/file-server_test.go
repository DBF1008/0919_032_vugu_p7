package devutil

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/language"
)

func TestFileServer(t *testing.T) {

	tmpDir, err := os.MkdirTemp("", "TestFileServer")
	must(err)
	defer os.RemoveAll(tmpDir)
	t.Logf("Using temporary dir: %s", tmpDir)

	fs := NewFileServer().SetDir(tmpDir)

	// redirect /dir to /dir/
	err = os.Mkdir(filepath.Join(tmpDir, "dir"), 0755)
	must(err)
	wr := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "/dir", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 301)
	checkHeader(t, r, wr.Result(), "Location", "dir/")

	// should error in a sane way on /dir/ if no listings
	wr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "/dir/", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 404)

	// serve index.html from /dir/
	must(os.WriteFile(filepath.Join(tmpDir, "dir/index.html"), []byte(`<html><body>index page here</body></html>`), 0644))
	wr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "/dir/", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 200)
	checkBody(t, r, wr.Result(), "index page here")

	// listing for /dir/
	os.Remove(filepath.Join(tmpDir, "dir/index.html"))
	must(os.WriteFile(filepath.Join(tmpDir, "dir/blerg.html"), []byte(`<html><body>blerg page here</body></html>`), 0644))
	fs.SetListings(true)
	wr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "/dir/", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 200)
	checkBody(t, r, wr.Result(), "blerg.html")
	checkHeader(t, r, wr.Result(), "Content-Type", "text/html; charset=utf-8")

	fs.SetListings(false)

	// /a.html should serve a.html
	must(os.WriteFile(filepath.Join(tmpDir, "a.html"), []byte(`<html><body>a page here</body></html>`), 0644))
	wr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "/a.html", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 200)
	checkBody(t, r, wr.Result(), "a page here")
	checkHeader(t, r, wr.Result(), "Content-Type", "text/html; charset=utf-8")

	// /a should also serve a.html
	wr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "/a", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 200)
	checkBody(t, r, wr.Result(), "a page here")
	checkHeader(t, r, wr.Result(), "Content-Type", "text/html; charset=utf-8")

	// not found should serve 404.html if present
	must(os.WriteFile(filepath.Join(tmpDir, "404.html"), []byte(`<html><body>custom not found page here</body></html>`), 0644))
	wr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "/ainthere", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 404)
	checkBody(t, r, wr.Result(), "custom not found page here")
	checkHeader(t, r, wr.Result(), "Content-Type", "text/html; charset=utf-8")

	// default not found
	os.Remove(filepath.Join(tmpDir, "404.html"))
	wr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "/ainthere", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 404)
	checkBody(t, r, wr.Result(), "404 page not found")

	// custom not found
	fs.SetNotFoundHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte("some other response here"))
	}))
	wr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "/ainthere", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 403)
	checkBody(t, r, wr.Result(), "some other response here")

}

// countingFS wraps an http.FileSystem and counts the number of Open calls.
type countingFS struct {
	http.FileSystem
	openCount int
	openNames []string
}

func (fs *countingFS) Open(name string) (http.File, error) {
	if name != "/404.html" {
		fs.openCount++
		fs.openNames = append(fs.openNames, name)
	}
	return fs.FileSystem.Open(name)
}

// TestFileServerHTMLRetryQueryString verifies that the .html fallback
// lookup is skipped when the request carries a query string, avoiding a
// wasted filesystem lookup on the way to the not-found handler.
func TestFileServerHTMLRetryQueryString(t *testing.T) {

	tmpDir, err := os.MkdirTemp("", "TestFileServerHTMLRetryQueryString")
	must(err)
	defer os.RemoveAll(tmpDir)

	must(os.WriteFile(filepath.Join(tmpDir, "getUser.html"),
		[]byte(`<html><body>getuser page</body></html>`), 0644))

	cfs := &countingFS{FileSystem: http.Dir(tmpDir)}
	fs := NewFileServer().SetFileSystem(cfs)

	// A request without a query string: the .html retry is performed and
	// succeeds.
	cfs.openCount = 0
	wr := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "/getUser", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 200)
	checkBody(t, r, wr.Result(), "getuser page")
	if cfs.openCount != 2 {
		t.Errorf("for %q expected 2 filesystem opens (path then path+.html), got %d: %v",
			r.URL.Path, cfs.openCount, cfs.openNames)
	}

	// A request with a query string for a missing dynamic path: the
	// .html retry must be skipped, so only one Open occurs.
	cfs.openCount = 0
	cfs.openNames = nil
	wr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "/api/getUser?name=foo", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 404)
	if cfs.openCount != 1 {
		t.Errorf("for %q expected exactly 1 filesystem open, got %d: %v",
			r.URL.Path, cfs.openCount, cfs.openNames)
	}
	for _, n := range cfs.openNames {
		if strings.Contains(n, "?") {
			t.Errorf("filesystem open must not include query string, got %q", n)
		}
	}

	// An escaped "?" inside the path itself (%3F) likewise must not be
	// retried with .html appended.
	cfs.openCount = 0
	cfs.openNames = nil
	wr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "/api/what%3Fever", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 404)
	if cfs.openCount != 1 {
		t.Errorf("for %q expected exactly 1 filesystem open, got %d: %v",
			r.URL.Path, cfs.openCount, cfs.openNames)
	}
}

// TestFileServerLocalRedirectEscaping verifies that redirect targets
// derived from directory names containing special characters are
// percent-encoded instead of breaking the Location header.
func TestFileServerLocalRedirectEscaping(t *testing.T) {

	tmpDir, err := os.MkdirTemp("", "TestFileServerLocalRedirectEscaping")
	must(err)
	defer os.RemoveAll(tmpDir)

	must(os.Mkdir(filepath.Join(tmpDir, "a?b"), 0755))
	fs := NewFileServer().SetDir(tmpDir)

	// Requesting /a%3Fb identifies the directory "a?b"; the redirect
	// target must encode the question mark so it stays part of the path.
	wr := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "/a%3Fb", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 301)
	checkHeader(t, r, wr.Result(), "Location", "a%3Fb/")

	// Existing plain redirects keep working unchanged.
	must(os.Mkdir(filepath.Join(tmpDir, "plain"), 0755))
	wr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "/plain?x=1", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 301)
	checkHeader(t, r, wr.Result(), "Location", "plain/?x=1")
}

// TestFileServerDirListSorting verifies that directory listings use
// locale-aware ordering rather than plain UTF-8 byte ordering, which
// sorts Chinese names incorrectly for Chinese locales.
func TestFileServerDirListSorting(t *testing.T) {

	tmpDir, err := os.MkdirTemp("", "TestFileServerDirListSorting")
	must(err)
	defer os.RemoveAll(tmpDir)

	// Unicode code points: 安 U+5B89 < 张 U+5F20 < 李 U+674E < 王 U+738B.
	// Pinyin order used by the Chinese collation:
	// 安(an) < 李(li) < 王(wang) < 张(zhang).
	for _, name := range []string{"张.txt", "安.txt", "王.txt", "李.txt"} {
		must(os.WriteFile(filepath.Join(tmpDir, name), []byte("x"), 0644))
	}

	// Chinese locale: entries must appear in pinyin order.
	fs := NewFileServer().SetDir(tmpDir).SetListings(true)
	fs.listingLang = language.Chinese
	wr := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "/", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 200)
	body := wr.Body.String()
	expected := []string{"安.txt", "李.txt", "王.txt", "张.txt"}
	idx := []int{}
	for _, name := range expected {
		p := strings.Index(body, name)
		if p < 0 {
			t.Fatalf("listing missing %q, body: %s", name, body)
		}
		idx = append(idx, p)
	}
	for i := 1; i < len(idx); i++ {
		if idx[i-1] >= idx[i] {
			t.Errorf("expected pinyin order %v, got body: %s", expected, body)
		}
	}
}

// TestLocaleFromEnv verifies locale extraction from the common
// environment variables.
func TestLocaleFromEnv(t *testing.T) {

	cases := []struct {
		env  map[string]string
		want language.Tag
	}{
		{map[string]string{}, language.Und},
		{map[string]string{"LANG": "C"}, language.Und},
		{map[string]string{"LANG": "POSIX"}, language.Und},
		{map[string]string{"LANG": "zh_CN.UTF-8"}, language.MustParse("zh-CN")},
		{map[string]string{"LC_COLLATE": "de_DE.UTF-8", "LANG": "en_US.UTF-8"},
			language.MustParse("de-DE")},
		{map[string]string{"LC_ALL": "en_US.UTF-8", "LC_COLLATE": "de_DE.UTF-8"},
			language.MustParse("en-US")},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			for _, key := range []string{"LC_ALL", "LC_COLLATE", "LANG"} {
				if v, ok := c.env[key]; ok {
					t.Setenv(key, v)
				} else {
					t.Setenv(key, "")
				}
			}
			got := localeFromEnv()
			if got != c.want {
				t.Errorf("for env %v expected %v, got %v", c.env, c.want, got)
			}
		})
	}
}

func checkBody(t *testing.T, req *http.Request, res *http.Response, text string) {
	b, err := httputil.DumpResponse(res, true)
	if err != nil {
		t.Logf("response dump failed: %v", err)
		return
	}
	if !bytes.Contains(b, []byte(text)) {
		t.Errorf("for %q expected response body to contain %q but it did not, full body: %s", req.URL.Path, text, b)
	}

}

func checkStatus(t *testing.T, req *http.Request, res *http.Response, status int) {
	st := res.StatusCode
	if st != status {
		t.Errorf("for %q expected status to be %v but got %v", req.URL.Path, status, st)
	}
}

func checkHeader(t *testing.T, req *http.Request, res *http.Response, key, val string) {
	hval := res.Header.Get(key)
	if hval != val {
		t.Errorf("for %q expected header %q to be %q but got %q", req.URL.Path, key, val, hval)
	}
}
