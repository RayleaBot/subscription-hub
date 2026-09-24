package httpaction

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoKeepsHostActionResultShape(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/flaky":
			attempts++
			if attempts == 1 {
				writer.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			http.SetCookie(writer, &http.Cookie{Name: "session", Value: "fixture"})
			_, _ = writer.Write([]byte(`{"ok":true}`))
		case "/redirect":
			http.Redirect(writer, request, "/flaky", http.StatusFound)
		case "/binary":
			_, _ = writer.Write([]byte{0xff, 0xfe, 0x00})
		case "/large":
			_, _ = writer.Write(make([]byte, maxHTTPResponseBodySize+1))
		}
	}))
	defer server.Close()
	ctx := context.Background()

	result, err := Do(ctx, Request{Method: "GET", URL: server.URL + "/flaky"})
	cookies, _ := result["set_cookies"].([]any)
	if err != nil || attempts != 2 || result["status_code"] != http.StatusOK || result["body_text"] != `{"ok":true}` || len(cookies) != 1 {
		t.Fatalf("retried GET = %#v, %v after %d attempts", result, err, attempts)
	}
	if result, err := Do(ctx, Request{Method: "GET", URL: server.URL + "/redirect"}); err != nil || result["status_code"] != http.StatusFound {
		t.Fatalf("redirect = %#v, %v; want the unfollowed 302 response", result, err)
	}
	if result, err := Do(ctx, Request{URL: server.URL + "/binary"}); err != nil || result["body_base64"] != "//4A" {
		t.Fatalf("binary body = %#v, %v", result, err)
	}
	if _, err := Do(ctx, Request{URL: server.URL + "/large"}); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("oversized body error = %v", err)
	}
}

func TestRedirectDoesNotWaitForResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/target")
		w.WriteHeader(http.StatusFound)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := Do(ctx, Request{URL: server.URL})
	if err != nil || result["status_code"] != http.StatusFound || ctx.Err() != nil {
		t.Fatalf("redirect waited for body: %#v, %v", result, err)
	}
	if result["headers"].(map[string]any)["Location"] != "/target" {
		t.Fatalf("redirect lost Location: %#v", result)
	}
}
