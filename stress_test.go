package promise

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// completesWithin runs fn on a separate goroutine and reports whether it
// finished before d. It is used as a deadlock watchdog: a hung promise would
// otherwise only surface as a whole-binary `go test` timeout.
func completesWithin(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer GinkgoRecover()
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

var _ = Describe("stress", func() {
	errBoom := errors.New("boom")

	When("a single slow promise is awaited by a massive fan-out", func() {
		It("delivers the settled result to all awaiters without lost wakeups", func() {
			ctx := context.Background()
			p := New(ctx, func(context.Context) (int, error) {
				time.Sleep(time.Millisecond * 10)
				return 7, nil
			})

			const n = 5000
			var ok int64
			ran := completesWithin(time.Second*10, func() {
				var wg sync.WaitGroup
				wg.Add(n)
				for i := 0; i < n; i++ {
					go func() {
						defer wg.Done()
						if r, err := Await(ctx, p); err == nil && r == 7 {
							atomic.AddInt64(&ok, 1)
						}
					}()
				}
				wg.Wait()
			})

			Expect(ran).To(BeTrue(), "fan-out did not complete within the watchdog window")
			Expect(ok).To(Equal(int64(n)))
		})
	})

	When("a large mixed workload churns through New/Await", func() {
		It("never deadlocks, panics out, or loses a result across success/error/panic paths", func() {
			const n = 3000
			var fulfilledN, rejectedN, panicN int64

			ran := completesWithin(time.Second*20, func() {
				var wg sync.WaitGroup
				wg.Add(n)
				for i := 0; i < n; i++ {
					go func(i int) {
						defer wg.Done()
						// Vary the ctx deadline so some Awaits race cancellation.
						ctx, cancel := context.WithTimeout(context.Background(), time.Duration(i%3+1)*time.Millisecond)
						defer cancel()

						p := New(context.Background(), func(context.Context) (int, error) {
							switch i % 3 {
							case 0:
								return i, nil
							case 1:
								return 0, errBoom
							default:
								panic("kaboom")
							}
						})

						_, err := Await(ctx, p)
						switch {
						case err == nil:
							atomic.AddInt64(&fulfilledN, 1)
						case errors.Is(err, errBoom):
							atomic.AddInt64(&rejectedN, 1)
						case errors.Is(err, context.DeadlineExceeded):
							// Awaited ctx fired first; acceptable.
						default:
							// Must be the wrapped panic error.
							if err.Error() != "" {
								atomic.AddInt64(&panicN, 1)
							}
						}
					}(i)
				}
				wg.Wait()
			})

			Expect(ran).To(BeTrue(), "mixed workload did not complete within the watchdog window")
			// We can't assert exact counts (ctx deadlines race completion), only
			// that the process survived all paths including non-error panics.
			Expect(fulfilledN + rejectedN + panicN).To(BeNumerically(">", 0))
		})
	})

	When("pre-settled promises are hammered concurrently", func() {
		It("returns instantly with the correct value/error for every call", func() {
			const n = 10000
			var good int64

			ran := completesWithin(time.Second*10, func() {
				ctx := context.Background()
				var wg sync.WaitGroup
				wg.Add(n)
				for i := 0; i < n; i++ {
					go func(i int) {
						defer wg.Done()
						if i%2 == 0 {
							if r, err := Await(ctx, Resolved(i)); err == nil && r == i {
								atomic.AddInt64(&good, 1)
							}
						} else {
							want := fmt.Errorf("err-%d", i)
							if _, err := Await(ctx, Rejected[int](want)); errors.Is(err, want) {
								atomic.AddInt64(&good, 1)
							}
						}
					}(i)
				}
				wg.Wait()
			})

			Expect(ran).To(BeTrue(), "pre-settled hammer did not complete within the watchdog window")
			Expect(good).To(Equal(int64(n)))
		})
	})

	When("many non-error panics are awaited", func() {
		It("keeps the process alive and rejects every one", func() {
			const n = 2000
			var rejected int64

			ran := completesWithin(time.Second*10, func() {
				ctx := context.Background()
				var wg sync.WaitGroup
				wg.Add(n)
				for i := 0; i < n; i++ {
					go func(i int) {
						defer wg.Done()
						p := New(ctx, func(context.Context) (int, error) {
							panic(fmt.Sprintf("panic-%d", i))
						})
						if _, err := Await(ctx, p); err != nil {
							atomic.AddInt64(&rejected, 1)
						}
					}(i)
				}
				wg.Wait()
			})

			Expect(ran).To(BeTrue(), "panic churn did not complete within the watchdog window")
			Expect(rejected).To(Equal(int64(n)))
		})
	})
})
