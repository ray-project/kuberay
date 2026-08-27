package historyserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emicklei/go-restful/v3"
	lru "github.com/hashicorp/golang-lru/v2/expirable"
	authv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// newTestAuthenticator wires an Authenticator onto a fake clientset whose
// TokenReview reaction is supplied by the caller.
func newTestAuthenticator(t *testing.T, reaction k8stesting.ReactionFunc) *Authenticator {
	t.Helper()
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", reaction)
	return &Authenticator{
		client: client,
		cache:  lru.NewLRU[string, string](authCacheSize, nil, time.Minute),
	}
}

func authenticatedReaction(user string) k8stesting.ReactionFunc {
	return func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.TokenReview{
			Status: authv1.TokenReviewStatus{
				Authenticated: true,
				User:          authv1.UserInfo{Username: user},
			},
		}, nil
	}
}

func TestAuthenticateRejectsInvalidTokenWith401(t *testing.T) {
	a := newTestAuthenticator(t, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.TokenReview{
			Status: authv1.TokenReviewStatus{Authenticated: false, Error: "token expired"},
		}, nil
	})

	_, err := a.Authenticate(context.Background(), "bad-token")
	if err == nil {
		t.Fatal("expected an error for a rejected token")
	}
	if got := authStatus(err); got != http.StatusUnauthorized {
		t.Fatalf("invalid token must map to 401, got %d", got)
	}
}

func TestAuthenticateFailsClosedWith503WhenBackendIsDown(t *testing.T) {
	// A TokenReview outage must never be mistaken for a valid credential.
	a := newTestAuthenticator(t, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver unreachable")
	})

	_, err := a.Authenticate(context.Background(), "some-token")
	if err == nil {
		t.Fatal("expected an error when TokenReview fails")
	}
	if got := authStatus(err); got != http.StatusServiceUnavailable {
		t.Fatalf("backend failure must map to 503, got %d", got)
	}
}

func TestAuthenticateCachesSuccessfulReview(t *testing.T) {
	calls := 0
	a := newTestAuthenticator(t, func(action k8stesting.Action) (bool, runtime.Object, error) {
		calls++
		return authenticatedReaction("alice")(action)
	})

	for i := 0; i < 3; i++ {
		user, err := a.Authenticate(context.Background(), "good-token")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user != "alice" {
			t.Fatalf("expected alice, got %q", user)
		}
	}
	if calls != 1 {
		t.Fatalf("expected the review to be cached after the first call, got %d calls", calls)
	}
}

func TestExtractTokenPrefersHeaderAndFailsHardOnBadScheme(t *testing.T) {
	cookie := &http.Cookie{Name: AuthCookieName, Value: "cookie-token"}

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/", nil)
	req.Header.Set("Authorization", "Bearer header-token")
	req.AddCookie(cookie)
	got, err := extractToken(req)
	if err != nil || got != "header-token" {
		t.Fatalf("header must win over cookie, got %q err=%v", got, err)
	}

	// A malformed header must not silently fall back to the cookie: that would
	// authenticate a misconfigured client as the browser's user.
	req = httptest.NewRequest(http.MethodGet, "/api/jobs/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	req.AddCookie(cookie)
	if _, err = extractToken(req); err == nil {
		t.Fatal("a non-Bearer Authorization header must be a hard failure")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/jobs/", nil)
	req.AddCookie(cookie)
	got, err = extractToken(req)
	if err != nil || got != "cookie-token" {
		t.Fatalf("cookie fallback failed: %q err=%v", got, err)
	}
}

func TestStripAuthCookieRemovesOnlyTheAuthCookie(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"removes auth cookie", "a=1; " + AuthCookieName + "=secret; b=2", "a=1; b=2"},
		{"only auth cookie", AuthCookieName + "=secret", ""},
		{"no auth cookie", "cluster_name=c1; session_name=live", "cluster_name=c1; session_name=live"},
		{"keeps similarly named cookie", AuthCookieName + "_other=v", AuthCookieName + "_other=v"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripAuthCookie(tc.in); got != tc.want {
				t.Fatalf("stripAuthCookie(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSafeNextRejectsOffOriginRedirects(t *testing.T) {
	tests := map[string]string{
		"/#/overview":           "/#/overview",
		"/enter_cluster/ns/c/s": "/enter_cluster/ns/c/s",
		"https://evil.test/x":   "/",
		"//evil.test/x":         "/",
		"javascript:alert(1)":   "/",
		"":                      "/",
	}
	for in, want := range tests {
		if got := safeNext(in); got != want {
			t.Fatalf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAuthFilterRedirectsBrowserButReturnsJSONToXHR(t *testing.T) {
	s := &ServerHandler{authenticator: newTestAuthenticator(t, authenticatedReaction("alice"))}

	// Browser navigation without a credential: keep the single-link flow working.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/enter_cluster/ns/raycluster/c/s", nil)
	req.Header.Set("Accept", "text/html")
	s.AuthFilter(restfulRequest(req), restfulResponse(rec), emptyChain())
	if rec.Code != http.StatusFound {
		t.Fatalf("browser navigation should redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc == "" || loc[:len(LoginPath)] != LoginPath {
		t.Fatalf("expected redirect to %s, got %q", LoginPath, loc)
	}

	// XHR without a credential: machine-readable 401, no HTML.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/jobs/", nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	s.AuthFilter(restfulRequest(req), restfulResponse(rec), emptyChain())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("XHR should get 401, got %d", rec.Code)
	}
}

func TestSelectClusterAuthentication(t *testing.T) {
	t.Run("unauthenticated request redirects to login", func(t *testing.T) {
		s := &ServerHandler{authenticator: newTestAuthenticator(t, authenticatedReaction("alice"))}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/select_cluster?from=test", nil)
		selectClusterHandler(s).ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("unauthenticated selector should redirect, got %d", rec.Code)
		}
		if got, want := rec.Header().Get("Location"), "/login?next=%2Fselect_cluster%3Ffrom%3Dtest"; got != want {
			t.Fatalf("redirect location = %q, want %q", got, want)
		}
	})

	t.Run("authenticated request renders selector", func(t *testing.T) {
		s := &ServerHandler{authenticator: newTestAuthenticator(t, authenticatedReaction("alice"))}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/select_cluster", nil)
		req.AddCookie(&http.Cookie{Name: AuthCookieName, Value: "good-token"})
		selectClusterHandler(s).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("authenticated selector should render, got %d", rec.Code)
		}
	})

	t.Run("TokenReview failure returns service unavailable", func(t *testing.T) {
		s := &ServerHandler{authenticator: newTestAuthenticator(t, func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("apiserver unavailable")
		})}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/select_cluster", nil)
		req.AddCookie(&http.Cookie{Name: AuthCookieName, Value: "token"})
		selectClusterHandler(s).ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("TokenReview failure should return 503, got %d", rec.Code)
		}
	})

	t.Run("auth disabled renders selector", func(t *testing.T) {
		rec := httptest.NewRecorder()
		selectClusterHandler(&ServerHandler{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/select_cluster", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("selector should render when auth is disabled, got %d", rec.Code)
		}
	})
}

func TestAuthFilterPassesThroughWhenAuthDisabled(t *testing.T) {
	s := &ServerHandler{} // authenticator nil => --enable-auth off
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/", nil)

	called := false
	chain := chainCalling(&called)
	s.AuthFilter(restfulRequest(req), restfulResponse(rec), chain)

	if !called {
		t.Fatal("with auth disabled the request must reach the handler chain")
	}
}

// --- go-restful test helpers -------------------------------------------------

func restfulRequest(r *http.Request) *restful.Request {
	return restful.NewRequest(r)
}

func restfulResponse(w http.ResponseWriter) *restful.Response {
	return restful.NewResponse(w)
}

// emptyChain fails the test if the filter lets an unauthenticated request pass.
func emptyChain() *restful.FilterChain {
	return &restful.FilterChain{
		Target: func(_ *restful.Request, _ *restful.Response) {
			panic("handler must not be reached for an unauthenticated request")
		},
	}
}

// chainCalling records whether the filter forwarded the request.
func chainCalling(called *bool) *restful.FilterChain {
	return &restful.FilterChain{
		Target: func(_ *restful.Request, _ *restful.Response) { *called = true },
	}
}
