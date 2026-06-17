package promise

import (
	"context"
	"errors"
	"testing"
)

var benchErr = errors.New("bench error")

// sink prevents the compiler from optimizing away benchmarked work.
var (
	sinkInt int
	sinkErr error
)

func BenchmarkNewAwaitSuccess(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(ctx, func(context.Context) (int, error) {
			return 1, nil
		})
		sinkInt, sinkErr = Await(ctx, p)
	}
}

func BenchmarkNewAwaitError(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(ctx, func(context.Context) (int, error) {
			return 0, benchErr
		})
		sinkInt, sinkErr = Await(ctx, p)
	}
}

func BenchmarkNewAwaitPanic(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(ctx, func(context.Context) (int, error) {
			panic("bench panic")
		})
		sinkInt, sinkErr = Await(ctx, p)
	}
}

func BenchmarkAwaitResolved(b *testing.B) {
	ctx := context.Background()
	p := Resolved(42)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkInt, sinkErr = Await(ctx, p)
	}
}

func BenchmarkAwaitRejected(b *testing.B) {
	ctx := context.Background()
	p := Rejected[int](benchErr)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkInt, sinkErr = Await(ctx, p)
	}
}

func BenchmarkConcurrentAwaitFanOut(b *testing.B) {
	ctx := context.Background()
	p := New(ctx, func(context.Context) (int, error) {
		return 99, nil
	})
	<-p.ch // ensure settled so the benchmark measures the await fast path

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var r int
		var err error
		for pb.Next() {
			r, err = Await(ctx, p)
		}
		sinkInt, sinkErr = r, err
	})
}
