package promise

import (
	"context"
	"errors"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These specs are designed to be run under `go test -race`. They drive many
// goroutines through Await/New concurrently so the race detector can observe
// the synchronization around p.state/p.result/p.err and the close of p.ch.
//
// Assertions are made on the spec goroutine after a WaitGroup join; the worker
// goroutines only record results. That keeps Gomega failures on the spec
// goroutine (no need for defer GinkgoRecover in every worker).

var _ = Describe("concurrency", func() {
	errBoom := errors.New("boom")

	When("many goroutines Await the same promise", func() {
		fanOut := func(ctx context.Context, p *Promise[int], n int) ([]int, []error) {
			results := make([]int, n)
			errs := make([]error, n)
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(n)
			for i := 0; i < n; i++ {
				go func(i int) {
					defer wg.Done()
					<-start // release all awaiters as simultaneously as possible
					results[i], errs[i] = Await(ctx, p)
				}(i)
			}
			close(start)
			wg.Wait()
			return results, errs
		}

		It("delivers the same fulfilled value to every awaiter", func() {
			ctx := context.Background()
			p := New(ctx, func(context.Context) (int, error) {
				time.Sleep(time.Millisecond * 5)
				return 42, nil
			})

			results, errs := fanOut(ctx, p, 200)
			for i := range results {
				Expect(errs[i]).ToNot(HaveOccurred())
				Expect(results[i]).To(Equal(42))
			}
		})

		It("delivers the same rejection error to every awaiter", func() {
			ctx := context.Background()
			p := New(ctx, func(context.Context) (int, error) {
				time.Sleep(time.Millisecond * 5)
				return 0, errBoom
			})

			results, errs := fanOut(ctx, p, 200)
			for i := range results {
				Expect(errs[i]).To(MatchError(errBoom))
				Expect(results[i]).To(BeZero())
			}
		})

		It("delivers the panic-derived error to every awaiter", func() {
			// This is the case that exposes the panic-path race: state/err are
			// written from the recover handler; every awaiter must observe them.
			ctx := context.Background()
			p := New(ctx, func(context.Context) (int, error) {
				panic(errBoom)
			})

			results, errs := fanOut(ctx, p, 200)
			for i := range results {
				Expect(errs[i]).To(MatchError(errBoom))
				Expect(results[i]).To(BeZero())
			}
		})
	})

	When("awaiters mix Await with raw channel receives", func() {
		It("observes a consistent settled state via both access styles", func() {
			ctx := context.Background()
			p := New(ctx, func(context.Context) (int, error) {
				time.Sleep(time.Millisecond * 5)
				return 13, nil
			})

			const n = 200
			results := make([]int, n)
			states := make([]state, n)
			errs := make([]error, n)
			var wg sync.WaitGroup
			wg.Add(n)
			for i := 0; i < n; i++ {
				go func(i int) {
					defer wg.Done()
					if i%2 == 0 {
						results[i], errs[i] = Await(ctx, p)
						states[i] = fulfilled // not asserted for even indices
					} else {
						<-p.ch
						// After the close, the field reads have a happens-before edge.
						states[i] = p.loadState()
						results[i] = p.result
					}
				}(i)
			}
			wg.Wait()

			for i := 0; i < n; i++ {
				Expect(results[i]).To(Equal(13))
				if i%2 == 0 {
					Expect(errs[i]).ToNot(HaveOccurred())
				} else {
					Expect(states[i]).To(Equal(fulfilled))
				}
			}
		})
	})

	When("many independent promises are created and awaited concurrently", func() {
		It("keeps each promise's result isolated", func() {
			ctx := context.Background()
			const n = 200
			got := make([]int, n)
			errs := make([]error, n)
			var wg sync.WaitGroup
			wg.Add(n)
			for i := 0; i < n; i++ {
				go func(i int) {
					defer wg.Done()
					p := New(ctx, func(context.Context) (int, error) {
						return i * 2, nil
					})
					got[i], errs[i] = Await(ctx, p)
				}(i)
			}
			wg.Wait()
			for i := 0; i < n; i++ {
				Expect(errs[i]).ToNot(HaveOccurred())
				Expect(got[i]).To(Equal(i * 2))
			}
		})
	})

	When("the promise settles strictly before the context is canceled", func() {
		It("never discards the settled result in favor of ctx.Err", func() {
			// Awaiters block in the inner select while the promise is pending.
			// The promise then settles and ONLY AFTER that (observed via p.ch)
			// the ctx is canceled. Both channels may be ready when an awaiter
			// wakes, and a bare two-way select would pick pseudo-randomly;
			// Await must re-check the settle channel so the result always wins.
			for round := 0; round < 300; round++ {
				ctx, cancel := context.WithCancel(context.Background())
				gate := make(chan struct{})
				p := New(context.Background(), func(context.Context) (int, error) {
					<-gate
					return 42, nil
				})

				const n = 8
				results := make([]int, n)
				errs := make([]error, n)
				var wg sync.WaitGroup
				wg.Add(n)
				for i := 0; i < n; i++ {
					go func(i int) {
						defer wg.Done()
						results[i], errs[i] = Await(ctx, p)
					}(i)
				}

				close(gate) // let the promise settle
				<-p.ch      // settled, happens-before the cancel below
				cancel()
				wg.Wait()

				for i := 0; i < n; i++ {
					Expect(errs[i]).ToNot(HaveOccurred(),
						"round %d awaiter %d: result settled before cancel was discarded", round, i)
					Expect(results[i]).To(Equal(42))
				}
			}
		})
	})

	When("Await races a context cancellation against completion", func() {
		It("returns either the result or ctx.Err without racing internal state", func() {
			// Run many rounds where the promise completion and the ctx deadline
			// fire at roughly the same time; -race must stay quiet and the
			// returned (value,err) pair must always be internally consistent.
			for round := 0; round < 200; round++ {
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				p := New(context.Background(), func(context.Context) (int, error) {
					time.Sleep(time.Millisecond)
					return 1, nil
				})
				r, err := Await(ctx, p)
				if err != nil {
					Expect(err).To(MatchError(context.DeadlineExceeded))
					Expect(r).To(BeZero())
				} else {
					Expect(r).To(Equal(1))
				}
				cancel()
			}
		})
	})
})
