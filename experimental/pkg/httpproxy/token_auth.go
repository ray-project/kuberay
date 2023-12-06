package httpproxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

const (
	token_header = "Authorization"
)

type tokenAuth struct {
	authorization
	token string
}

func NewTokenAuth(token string, proxy *httputil.ReverseProxy, prefix string, upstream *url.URL) tokenAuth {
	auth := authorization{proxy: proxy, prefix: prefix, upstream: upstream}
	return tokenAuth{token: token, authorization: auth}
}

func (ta tokenAuth) AuthFunc() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.String(), ta.prefix) {
			auth := r.Header.Get(token_header)
			if auth != ta.token {
				// Wrong token
				WriteUnauthorisedResponse(w)
				return
			}
		}
		modifyRequest(r, ta.upstream)
		ta.proxy.ServeHTTP(w, r)
	}
}
