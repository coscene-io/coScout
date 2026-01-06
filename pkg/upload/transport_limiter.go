// Copyright 2026 coScene
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package upload

import (
	"context"
	"errors"
	"io"
	"net/http"

	"golang.org/x/time/rate"
)

var (
	errReaderNotSeekable = errors.New("rateLimitedReader: underlying reader is not seekable")
	errNilRoundTripper   = errors.New("rate limited transport: nil RoundTripper")
)

// rateLimitedTransport wraps a RoundTripper and throttles request/response bodies.
type rateLimitedTransport struct {
	uploadLimiter   *rate.Limiter
	downloadLimiter *rate.Limiter
	transport       http.RoundTripper
}

type rateLimitedReader struct {
	reader   io.Reader
	limiter  *rate.Limiter
	maxChunk int
	wait     func(int) error
}

// rateLimitedReadCloser bundles the limited Reader, the original Closer, and Seeker.
type rateLimitedReadCloser struct {
	io.Reader
	io.Closer
	seeker io.Seeker
}

// Seek delegates to the underlying seeker.
func (r *rateLimitedReadCloser) Seek(offset int64, whence int) (int64, error) {
	if r.seeker == nil {
		return 0, errReaderNotSeekable
	}
	return r.seeker.Seek(offset, whence)
}

func newRateLimitedReader(ctx context.Context, reader io.Reader, limiter *rate.Limiter) io.Reader {
	if limiter == nil {
		return reader
	}

	maxChunk := limiter.Burst()
	if maxChunk <= 0 {
		maxChunk = 64 * 1024
	}

	//nolint: contextcheck // ctx sourced from request.
	if ctx == nil {
		ctx = context.Background()
	}

	return &rateLimitedReader{
		reader:   reader,
		limiter:  limiter,
		maxChunk: maxChunk,
		wait: func(n int) error {
			return limiter.WaitN(ctx, n)
		},
	}
}

func (r *rateLimitedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if len(p) > r.maxChunk {
		p = p[:r.maxChunk]
	}

	// Read first
	n, err := r.reader.Read(p)

	// Enforce rate after bytes are read
	if n > 0 && r.limiter != nil && r.wait != nil {
		if waitErr := r.wait(n); waitErr != nil && err == nil {
			// If context is canceled during wait, return that error
			err = waitErr
		}
	}

	return n, err
}

func newRateLimitedReadCloser(ctx context.Context, rc io.ReadCloser, limiter *rate.Limiter) io.ReadCloser {
	if rc == nil {
		return nil
	}

	limited := newRateLimitedReader(ctx, rc, limiter)
	seeker, _ := rc.(io.Seeker) // Detect if the original body supports Seek

	// Explicitly combine limited reader with original closer
	return &rateLimitedReadCloser{
		Reader: limited, // Rate-limited reader
		Closer: rc,      // Original closer (avoid handle leaks)
		seeker: seeker,  // Original seeker
	}
}

// RoundTrip throttles upload/download streams via limiters.
func (t rateLimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.transport == nil {
		return nil, errNilRoundTripper
	}

	if req.Body != nil {
		req.Body = newRateLimitedReadCloser(req.Context(), req.Body, t.uploadLimiter)
	}

	resp, err := t.transport.RoundTrip(req)
	if resp != nil && resp.Body != nil {
		resp.Body = newRateLimitedReadCloser(req.Context(), resp.Body, t.downloadLimiter)
	}

	return resp, err
}

// NewRateLimitedTransport returns a RoundTripper that throttles uploads/downloads.
func NewRateLimitedTransport(uploadLimit, downloadLimit int64, transport http.RoundTripper) http.RoundTripper {
	if uploadLimit == 0 && downloadLimit == 0 {
		return transport
	}

	return rateLimitedTransport{
		uploadLimiter:   buildLimiter(uploadLimit),
		downloadLimiter: buildLimiter(downloadLimit),
		transport:       transport,
	}
}

func buildLimiter(maxBytesPerSec int64) *rate.Limiter {
	if maxBytesPerSec <= 0 {
		return nil
	}

	burstLimit := maxBytesPerSec
	// Burst is capped; keep original logic but limit to a sane maximum.
	if burstLimit > 1<<31-1 {
		burstLimit = 1<<31 - 1
	}

	burst := int(burstLimit) / 10
	if burst < 64*1024 {
		burst = 64 * 1024
	}
	if burst > 4*1024*1024 {
		burst = 4 * 1024 * 1024
	}

	return rate.NewLimiter(rate.Limit(maxBytesPerSec), burst)
}
