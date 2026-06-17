package promise

import (
	"context"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/goleak"
)

// settle gives the scheduler a chance to reap finished goroutines and returns a
// stable baseline count.
func settle() int {
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	return runtime.NumGoroutine()
}

var _ = Describe("goroutine leaks", func() {
	When("promises settle normally", func() {
		It("leaves no goroutine behind after a large batch of New+Await", func() {
			base := settle()
			ctx := context.Background()

			for i := 0; i < 1000; i++ {
				p := New(ctx, func(context.Context) (int, error) {
					return i, nil
				})
				_, err := Await(ctx, p)
				Expect(err).ToNot(HaveOccurred())
			}

			// All f's have returned and all Awaits completed, so every per-New
			// goroutine must have exited. Allow a small tolerance for scheduler
			// and test-framework noise.
			Eventually(runtime.NumGoroutine).
				Within(time.Second * 2).
				WithPolling(time.Millisecond * 10).
				Should(BeNumerically("<=", base+2))
		})
	})

	When("a non-cancelable f outlives a canceled Await", func() {
		It("detaches the goroutine and reaps it once f returns", func() {
			base := settle()

			ctx, cancel := context.WithCancel(context.Background())
			p := New(context.Background(), func(context.Context) (int, error) {
				time.Sleep(time.Millisecond * 150)
				return 1, nil
			})

			go func() {
				time.Sleep(time.Millisecond * 30)
				cancel()
			}()

			_, err := Await(ctx, p)
			Expect(err).To(MatchError(context.Canceled))

			// The detached goroutine is a documented temporary leak; it must
			// drain after f's sleep elapses rather than leaking permanently.
			Eventually(runtime.NumGoroutine).
				Within(time.Second * 2).
				WithPolling(time.Millisecond * 10).
				Should(BeNumerically("<=", base))
		})
	})

	When("awaiting pre-settled promises", func() {
		It("spawns no goroutines for Resolved/Rejected", func() {
			defer goleak.VerifyNone(GinkgoT(), goleak.IgnoreCurrent())

			ctx := context.Background()
			for i := 0; i < 1000; i++ {
				_, _ = Await(ctx, Resolved(i))
				_, _ = Await(ctx, Rejected[int](context.Canceled))
			}
		})
	})
})
