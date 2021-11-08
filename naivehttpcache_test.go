package naivehttpcache_test

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/blukai/naivehttpcache"
	"github.com/gregjones/httpcache"
)

func TestMaxAge(t *testing.T) {
	tsHits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tsHits++
	}))
	defer ts.Close()

	maxAge := time.Second
	httpClient := &http.Client{
		Transport: naivehttpcache.NewTransport(
			httpcache.NewMemoryCache(),
			naivehttpcache.WithMaxAge(maxAge),
		),
	}

	check := func(expected string) {
		resp, err := httpClient.Get(ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if _, err := ioutil.ReadAll(resp.Body); err != nil {
			t.Fatal(err)
		}
		if got := resp.Header.Get(naivehttpcache.XFromCache); got != expected {
			t.Fatalf("expected %q; got %q\n", expected, got)
		}
	}

	// we don't expect to hit cache on first request
	check("")
	// second request should hit it
	check("1")
	// after sleep cached response should qulify as expired
	time.Sleep(maxAge)
	// third request should be a miss
	check("")

	if tsHits != 2 {
		t.Fatalf("expected 2 server hits; got %d", tsHits)
	}
}
