package hyperliquid

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// benchRecorder installs a replay-only recorder as http.DefaultTransport for
// the duration of a benchmark, so info methods execute their full decode path
// without network access.
func benchRecorder(tb testing.TB, cassetteName string) {
	tb.Helper()
	opts := append(
		defaultRecorderOpts(false),
		// Benchmarks replay the same interaction thousands of times.
		recorder.WithReplayableInteractions(true),
	)
	r, err := recorder.New(filepath.Join("testdata", cassetteName), opts...)
	if err != nil {
		tb.Fatal(err)
	}
	orig := http.DefaultTransport
	http.DefaultTransport = r
	tb.Cleanup(func() {
		http.DefaultTransport = orig
		if err := r.Stop(); err != nil {
			tb.Logf("recorder stop: %v", err)
		}
	})
}

// BenchmarkInfoMetaAndAssetCtxs measures the full decode path (replay
// transport included) of one of the heaviest info endpoints.
func BenchmarkInfoMetaAndAssetCtxs(b *testing.B) {
	info := NewInfo(context.Background(), MainnetAPIURL, true, &Meta{}, &SpotMeta{}, nil)
	benchRecorder(b, "MetaAndAssetCtxs")
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		res, err := info.MetaAndAssetCtxs(ctx, MetaAndAssetCtxsParams{})
		if err != nil {
			b.Fatal(err)
		}
		if len(res.Ctxs) == 0 {
			b.Fatal("empty ctxs")
		}
	}
}

// BenchmarkInfoMeta measures Meta, which goes through parseMetaResponse.
func BenchmarkInfoMeta(b *testing.B) {
	info := NewInfo(context.Background(), MainnetAPIURL, true, &Meta{}, &SpotMeta{}, nil)
	benchRecorder(b, "Meta")
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		meta, err := info.Meta(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if len(meta.Universe) == 0 {
			b.Fatal("empty universe")
		}
	}
}
