package naivehttpcache

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/gregjones/httpcache"
)

// XFromCache is the header added to responses that are returned from the cache.
// Re-exported from httpcache package for convenience.
const XFromCache = httpcache.XFromCache

// Transport is an implementation of http.RoundTripper that will return values from a cache
// where possible (avoiding a network request).
// Transport is based on Transport from httpcache package
// https://github.com/gregjones/httpcache/blob/901d90724c7919163f472a9812253fb26761123d/httpcache.go#L99
type Transport struct {
	// The RoundTripper interface actually used to make requests.
	// If nil, http.DefaultTransport is used.
	Transport http.RoundTripper
	Cache     httpcache.Cache
	// MaxAge states how long cached response can be used.
	// Values <= 0 will be ignored.
	MaxAge time.Duration
}

type Options struct {
	MaxAge time.Duration
}

type Option func(*Options)

func WithMaxAge(maxAge time.Duration) Option {
	return func(opts *Options) {
		opts.MaxAge = maxAge
	}
}

func NewTransport(cache httpcache.Cache, opts ...Option) *Transport {
	args := &Options{}
	for _, o := range opts {
		o(args)
	}

	return &Transport{
		Cache:  cache,
		MaxAge: args.MaxAge,
	}
}

// RoundTrip takes a Request and returns a Response.
//
// If there is a fresh Response already in cache, then it will be returned without connecting to
// the server.
// RoundTrip gives 0 fucks about Cache-Control and other stuff,
// it just blindly caches all GET requests that responsed with http.StatusOK (code 200).
//
// It's based on RoundTrip implementation from httpcache package
// https://github.com/gregjones/httpcache/blob/901d90724c7919163f472a9812253fb26761123d/httpcache.go#L139
// but heavilly differs from it.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	transport := t.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	if req.Method != http.MethodGet {
		return transport.RoundTrip(req)
	}

	// cacheKey is the same as in httpcache package
	// https://github.com/gregjones/httpcache/blob/901d90724c7919163f472a9812253fb26761123d/httpcache.go#L42
	cacheKey := req.URL.String()

	if cachedVal, ok := t.Cache.Get(cacheKey); ok {
		cachedResp, err := http.ReadResponse(bufio.NewReader(bytes.NewBuffer(cachedVal)), req)
		if err != nil {
			return cachedResp, err
		}

		if t.MaxAge > 0 {
			date, err := httpcache.Date(cachedResp.Header)
			if err != nil {
				return nil, err
			}

			if date.Add(t.MaxAge).Before(time.Now()) {
				t.Cache.Delete(cacheKey)
				cachedResp = nil
			}
		}

		if cachedResp != nil {
			cachedResp.Header.Set(XFromCache, "1")
			return cachedResp, err
		}
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	// Delay caching until EOF is reached.
	// This is stolen without any modifications from
	// https://github.com/gregjones/httpcache/blob/901d90724c7919163f472a9812253fb26761123d/httpcache.go#L233
	resp.Body = &cachingReadCloser{
		R: resp.Body,
		OnEOF: func(r io.Reader) {
			resp := *resp

			// this is naive http cache, so it should be fine to do that.
			// why do we set date manually? because not all responses have it.
			// why do we need care? because of MaxAge
			if resp.Header.Get("date") == "" {
				resp.Header.Set("date", time.Now().Format(time.RFC1123))
			}

			resp.Body = ioutil.NopCloser(r)
			respBytes, err := httputil.DumpResponse(&resp, true)
			if err == nil {
				t.Cache.Set(cacheKey, respBytes)
			}
		},
	}

	return resp, err
}

// cachingReadCloser is a wrapper around ReadCloser R that calls OnEOF
// handler with a full copy of the content read from R when EOF is
// reached.
// cachingReadCloser and all its methods are stolen without any
// modifications from https://github.com/gregjones/httpcache/blob/901d90724c7919163f472a9812253fb26761123d/httpcache.go#L520
type cachingReadCloser struct {
	// Underlying ReadCloser.
	R io.ReadCloser
	// OnEOF is called with a copy of the content of R when EOF is reached.
	OnEOF func(io.Reader)
	// buf stores a copy of the content of R.
	buf bytes.Buffer
}

// Read reads the next len(p) bytes from R or until R is drained. The
// return value n is the number of bytes read. If R has no data to
// return, err is io.EOF and OnEOF is called with a full copy of what
// has been read so far.
func (r *cachingReadCloser) Read(p []byte) (n int, err error) {
	n, err = r.R.Read(p)
	r.buf.Write(p[:n])
	if err == io.EOF {
		r.OnEOF(bytes.NewReader(r.buf.Bytes()))
	}
	return n, err
}

func (r *cachingReadCloser) Close() error {
	return r.R.Close()
}
