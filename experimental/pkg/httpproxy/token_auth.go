package httpproxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

const (
	tokenHeader = "Authorization"
)

type TokenAuth struct {
	authorization
	token string
}

func NewTokenAuth(token string, proxy *httputil.ReverseProxy, prefix string, upstream *url.URL) TokenAuth {
	auth := authorization{proxy: proxy, prefix: prefix, upstream: upstream}
	return TokenAuth{token: token, authorization: auth}
}

func (ta TokenAuth) AuthFunc() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.String(), ta.prefix) {
			auth := r.Header.Get(tokenHeader)
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
