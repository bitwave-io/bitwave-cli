package orgreports

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestRawRequestDetailedPreservesStatusHeadersAndBody(t *testing.T) {
	client := New("https://api.bitwave.test", func() (string, error) { return "token", nil })
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization header = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("If-Match") != `"revision-1"` {
			t.Fatalf("If-Match header = %q", request.Header.Get("If-Match"))
		}
		responseHeader := make(http.Header)
		responseHeader.Set("ETag", `"revision-2"`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     responseHeader,
			Body:       io.NopCloser(strings.NewReader(`{"id":"org"}`)),
			Request:    request,
		}, nil
	})}

	response, err := client.RawRequestDetailed(context.Background(), APIServiceCore, http.MethodPatch, "/v3/orgs/org", []byte(`{}`), http.Header{"If-Match": []string{`"revision-1"`}})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("ETag") != `"revision-2"` || string(response.Body) != `{"id":"org"}` {
		t.Fatalf("unexpected response: %#v", response)
	}
}
