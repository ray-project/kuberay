package httpproxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"

	"k8s.io/klog/v2"
)

type authorization struct {
	proxy    *httputil.ReverseProxy
	prefix   string
	upstream *url.URL
}

// Create Unauthorized response
func WriteUnauthorisedResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusUnauthorized)
	_, err := w.Write([]byte("Unauthorized\n"))
	if err != nil {
		klog.Info("failed writing unauthorized response ", err)
	}
}

// Create bad request response
func WriteBadRequestResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusBadRequest)
	_, err := w.Write([]byte("Bad Request\n"))
	if err != nil {
		klog.Info("failed writing bad request response ", err)
	}
}

// Create internal error response
func WriteInternalErrorResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusInternalServerError)
	_, err := w.Write([]byte("Internal Server Error\n"))
	if err != nil {
		klog.Info("failed writing internal error response ", err)
	}
}

// Modify request upstream URL
func modifyRequest(r *http.Request, upstream *url.URL) {
	r.URL.Host = upstream.Host
	r.URL.Scheme = upstream.Scheme
	r.Header.Set("X-Forwarded-Host", r.Host)
	r.Host = upstream.Host
	body, err := io.ReadAll(r.Body)
	if err != nil {
		klog.Info("failed reading request body ", err)
	}
	defer r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
}
