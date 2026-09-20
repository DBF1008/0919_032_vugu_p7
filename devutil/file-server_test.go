package devutil

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// recordingFS wraps an http.FileSystem and records every name passed to Open.
type recordingFS struct {
	fsys  http.FileSystem
	opens []string
}

func (r *recordingFS) Open(name string) (http.File, error) {
	r.opens = append(r.opens, name)
	return r.fsys.Open(name)
}

func TestFileServerQueryString(t *testing.T) {

	tmpDir, err := os.MkdirTemp("", "TestFileServerQueryString")
	must(err)
	defer os.RemoveAll(tmpDir)
	t.Logf("Using temporary dir: %s", tmpDir)

	rec := &recordingFS{fsys: http.Dir(tmpDir)}
	fs := NewFileServer().SetFileSystem(rec)

	// a normal request with a query string should still serve a.html
	must(os.WriteFile(filepath.Join(tmpDir, "a.html"), []byte(`<html><body>a page here</body></html>`), 0644))
	wr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/a?name=foo", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 200)
	checkBody(t, r, wr.Result(), "a page here")

	// a non-standard request with the query string embedded in the path
	// (e.g. "/api/getUser?name=foo") must not result in a file lookup for
	// the bogus name "/api/getUser?name=foo.html"
	rec.opens = nil
	wr = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/getUser", nil)
	r.URL.Path = "/api/getUser?name=foo" // simulate query string left in path
	r.URL.RawQuery = ""
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 404)
	for _, name := range rec.opens {
		if strings.ContainsAny(name, "?#") {
			t.Errorf("expected no file lookup containing query string or fragment, but Open was called with %q (all opens: %v)", name, rec.opens)
		}
	}

}

func TestFileServerRedirectEscaping(t *testing.T) {

	tmpDir, err := os.MkdirTemp("", "TestFileServerRedirectEscaping")
	must(err)
	defer os.RemoveAll(tmpDir)
	t.Logf("Using temporary dir: %s", tmpDir)

	fs := NewFileServer().SetDir(tmpDir)

	// directory names containing special characters must be percent-escaped
	// in the redirect Location so it is not corrupted
	for dir, location := range map[string]string{
		"my dir": "my%20dir/",
		"中文目录":   "%E4%B8%AD%E6%96%87%E7%9B%AE%E5%BD%95/",
		"100%":   "100%25/",
	} {
		must(os.Mkdir(filepath.Join(tmpDir, dir), 0755))
		wr := httptest.NewRecorder()
		// build the request target the same way a client would: with the
		// path percent-escaped
		r := httptest.NewRequest("GET", (&url.URL{Path: "/" + dir}).String(), nil)
		fs.ServeHTTP(wr, r)
		checkStatus(t, r, wr.Result(), 301)
		checkHeader(t, r, wr.Result(), "Location", location)
	}

}

func TestFileServerDirListLocaleSort(t *testing.T) {

	tmpDir, err := os.MkdirTemp("", "TestFileServerDirListLocaleSort")
	must(err)
	defer os.RemoveAll(tmpDir)
	t.Logf("Using temporary dir: %s", tmpDir)

	fs := NewFileServer().SetDir(tmpDir).SetListings(true)

	// Chinese file names should be sorted locale-aware (by pinyin), not by
	// raw byte order: pinyin order is beijing < guangzhou < shanghai,
	// whereas byte order would put 上海 (U+4E0A) first
	names := []string{"apple.txt", "北京.txt", "广州.txt", "上海.txt"}
	for _, name := range names {
		must(os.WriteFile(filepath.Join(tmpDir, name), []byte("x"), 0644))
	}
	wr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	fs.ServeHTTP(wr, r)
	checkStatus(t, r, wr.Result(), 200)
	body := wr.Body.String()
	prev := -1
	for _, name := range names {
		i := strings.Index(body, name)
		if i < 0 {
			t.Errorf("expected listing to contain %q, body: %s", name, body)
			continue
		}
		if i < prev {
			t.Errorf("expected %q to appear after previous entries in listing, body: %s", name, body)
		}
		prev = i
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
