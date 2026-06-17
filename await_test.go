package promise

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Await", func() {
	wantResult := 1
	wantErr := errors.New("some error")

	When("the promise is completed immediately", func() {
		When("the promise returns a value", func() {
			It("should be fulfilled", func() {
				ctx := context.Background()

				p := New(ctx, func(context.Context) (int, error) {
					return wantResult, nil
				})

				gotResult, err := Await(ctx, p)
				Expect(err).ToNot(HaveOccurred())
				Expect(gotResult).To(Equal(wantResult))
			})
		})

		When("the promise returns an error", func() {
			It("should be rejected", func() {
				ctx := context.Background()

				p := New(ctx, func(context.Context) (int, error) {
					return 0, wantErr
				})

				gotResult, err := Await(ctx, p)
				Expect(err).To(MatchError(wantErr))
				Expect(gotResult).To(BeZero())
			})
		})
	})

	When("the promise takes a time to complete", func() {
		When("the promise returns a value", func() {
			It("should be fulfilled", func() {
				ctx := context.Background()

				p := New(ctx, func(context.Context) (int, error) {
					time.Sleep(time.Millisecond * 100)
					return 1, nil
				})

				now := time.Now()
				gotResult, err := Await(ctx, p)
				Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 100, 20))
				Expect(err).ToNot(HaveOccurred())
				Expect(gotResult).To(Equal(wantResult))
			})
		})

		When("the promise returns an error", func() {
			It("should be rejected", func() {
				ctx := context.Background()

				p := New(ctx, func(context.Context) (int, error) {
					time.Sleep(time.Millisecond * 100)
					return 0, wantErr
				})

				now := time.Now()
				gotResult, err := Await(ctx, p)
				Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 100, 20))
				Expect(err).To(MatchError(wantErr))
				Expect(gotResult).To(BeZero())
			})
		})
	})

	When("the promise panics with a non-error value", func() {
		It("should return a wrapped panic error", func() {
			ctx := context.Background()

			p := New(ctx, func(context.Context) (int, error) {
				panic("boom")
			})

			gotResult, err := Await(ctx, p)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("boom"))
			Expect(gotResult).To(BeZero())
		})
	})

	When("the context is canceled before the function returns", func() {
		It("should return early and not wait for promise to be completed", func() {
			// For this case `p` will leak until the function returns.

			ctx, cancelFnc := context.WithCancel(context.Background())

			var isPFinished atomic.Bool
			p := New(context.Background(), func(ctx context.Context) (int, error) {
				time.Sleep(time.Millisecond * 200)
				isPFinished.Store(true)
				return 1, nil
			})

			go func() {
				<-time.After(time.Millisecond * 100)
				cancelFnc()
			}()

			now := time.Now()
			gotResult, err := Await(ctx, p)
			Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 100, 20))
			Expect(err).To(MatchError(context.Canceled))
			Expect(gotResult).To(BeZero())
			Expect(isPFinished.Load()).To(BeFalse())
		})
	})

	When("the context is already canceled before Await is called", func() {
		It("should return ctx.Err immediately without waiting for the promise", func() {
			ctx, cancelFnc := context.WithCancel(context.Background())
			cancelFnc() // cancel BEFORE Await

			// f never settles (its own context is Background), so the only way
			// Await can return promptly is by honoring the already-done ctx.
			p := New(context.Background(), func(c context.Context) (int, error) {
				time.Sleep(time.Hour)
				return 1, nil
			})

			now := time.Now()
			gotResult, err := Await(ctx, p)
			Expect(time.Since(now)).To(BeNumerically("<", time.Millisecond*50))
			Expect(err).To(MatchError(context.Canceled))
			Expect(gotResult).To(BeZero())
		})
	})

	When("the promise is already settled", func() {
		It("should return the value of a Resolved promise immediately", func() {
			// Resolved promises have a nil channel; a naive receive would block
			// forever. A short-deadline ctx proves Await does not block on it.
			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*50)
			defer cancel()

			now := time.Now()
			gotResult, err := Await(ctx, Resolved(42))
			Expect(time.Since(now)).To(BeNumerically("<", time.Millisecond*20))
			Expect(err).ToNot(HaveOccurred())
			Expect(gotResult).To(Equal(42))
		})

		It("should return the error of a Rejected promise immediately", func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*50)
			defer cancel()

			now := time.Now()
			gotResult, err := Await(ctx, Rejected[int](wantErr))
			Expect(time.Since(now)).To(BeNumerically("<", time.Millisecond*20))
			Expect(err).To(MatchError(wantErr))
			Expect(gotResult).To(BeZero())
		})

		It("should return the value of a New promise that already completed (late Await)", func() {
			ctx := context.Background()
			p := New(ctx, func(context.Context) (int, error) {
				return 5, nil
			})
			<-p.ch // guarantee the promise has settled before awaiting

			now := time.Now()
			gotResult, err := Await(ctx, p)
			Expect(time.Since(now)).To(BeNumerically("<", time.Millisecond*5))
			Expect(err).ToNot(HaveOccurred())
			Expect(gotResult).To(Equal(5))
		})

		It("should return the cached result on repeated Await calls", func() {
			ctx := context.Background()
			p := New(ctx, func(context.Context) (int, error) {
				return 7, nil
			})
			<-p.ch

			r1, e1 := Await(ctx, p)
			r2, e2 := Await(ctx, p)
			Expect(e1).ToNot(HaveOccurred())
			Expect(e2).ToNot(HaveOccurred())
			Expect(r1).To(Equal(7))
			Expect(r2).To(Equal(7))
		})

		It("should return ErrInvalidState for a promise stuck in an impossible state", func() {
			// A pending promise with no channel cannot arise from the public
			// API; this pins the defensive branch and the exported sentinel.
			p := &Promise[int]{} // nil ch, pending state

			gotResult, err := Await(context.Background(), p)
			Expect(err).To(MatchError(ErrInvalidState))
			Expect(gotResult).To(BeZero())
		})

		It("should prefer the settled result over an already-canceled ctx", func() {
			// Both the promise (Resolved) and ctx are ready; the settled value
			// must win deterministically rather than ctx.Err().
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			gotResult, err := Await(ctx, Resolved(99))
			Expect(err).ToNot(HaveOccurred())
			Expect(gotResult).To(Equal(99))
		})
	})
})
