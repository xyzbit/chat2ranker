package httpapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyzbit/agentkiln/rank/backend/internal/access"
	"github.com/xyzbit/agentkiln/rank/backend/internal/httpapi"
)

func TestBrowserLoginAndLogout(t *testing.T) {
	token := strings.Repeat("a", 64)
	hash := sha256.Sum256([]byte(token))
	path := filepath.Join(t.TempDir(), "access.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"version":1,"operators":[{"subject":"alice","tokenSha256":"%s"}]}`, hex.EncodeToString(hash[:]))), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := access.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"http://rank.test", "https://rank.test"} {
		t.Run(origin, func(t *testing.T) {
			handler := httpapi.New(nil, httpapi.Options{AccessFile: file, ManagementOrigin: origin})
			call := func(method, path, body, cookie, requestOrigin string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, origin+path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Cookie", cookie)
				req.Header.Set("Origin", requestOrigin)
				out := httptest.NewRecorder()
				handler.ServeHTTP(out, req)
				return out
			}
			login := func(username, password, cookie string) *httptest.ResponseRecorder {
				return call("POST", "/api/auth/login", fmt.Sprintf(`{"username":%q,"password":%q}`, username, password), cookie, origin)
			}
			require := func(out *httptest.ResponseRecorder, status int) {
				t.Helper()
				if out.Code != status {
					t.Fatalf("status %d want %d: %s", out.Code, status, out.Body.String())
				}
				if out.Header().Get("WWW-Authenticate") != "" {
					t.Fatal("browser challenge present")
				}
			}
			require(call("GET", "/api/identity", "", "", ""), 401)
			for _, user := range []string{"", "bob"} {
				require(login(user, token, ""), 401)
			}
			require(login("alice", "incorrect", ""), 401)
			require(call("POST", "/api/auth/login", `{}`, "", "https://other.test"), 403)
			require(call("POST", "/api/auth/login", `{}`, "", ""), 403)
			require(call("POST", "/api/auth/login", `{} {}`, "", origin), 400)
			require(call("POST", "/api/auth/login", strings.Repeat("x", 5000), "", origin), 400)
			out := login("alice", token, "")
			require(out, 200)
			cookies := out.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatal("missing session cookie")
			}
			cookie := cookies[0]
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge <= 0 || cookie.Secure != strings.HasPrefix(origin, "https:") {
				t.Fatal("incorrect cookie policy")
			}
			if strings.Contains(out.Body.String(), token) || cookie.Value == token {
				t.Fatal("credential exposed")
			}
			raw := cookie.Name + "=" + cookie.Value
			require(call("GET", "/api/identity", "", raw, ""), 200)
			require(call("POST", "/api/experiments", `{}`, raw, ""), 403)
			require(call("POST", "/api/experiments", `{}`, raw, "http://rank.test:9999"), 403)
			require(call("POST", "/v1/responses", `{}`, raw, origin), 401)
			rotated := login("alice", token, raw)
			require(rotated, 200)
			require(call("GET", "/api/identity", "", raw, ""), 401)
			raw = cookie.Name + "=" + rotated.Result().Cookies()[0].Value
			require(call("POST", "/api/auth/logout", `{}`, raw, "https://other.test"), 403)
			require(call("GET", "/api/identity", "", raw, ""), 200)
			logout := call("POST", "/api/auth/logout", `{}`, raw, origin)
			require(logout, 204)
			if logout.Result().Cookies()[0].MaxAge != -1 {
				t.Fatal("cookie not cleared")
			}
			require(call("GET", "/api/identity", "", raw, ""), 401)
			req := httptest.NewRequest("POST", origin+"/api/auth/login", io.NopCloser(strings.NewReader(`{}`)))
			req.Header.Set("Origin", origin)
			req.Header.Set("Content-Type", "text/plain")
			out = httptest.NewRecorder()
			handler.ServeHTTP(out, req)
			require(out, 415)
		})
	}
}
