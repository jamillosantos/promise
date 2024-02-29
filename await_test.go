package promise

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Promise", func() {
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
				Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 100, 10))
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
				Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 100, 10))
				Expect(err).To(MatchError(wantErr))
				Expect(gotResult).To(BeZero())
			})
		})
	})

	When("the context is called before the function returns", func() {
		It("should return early and not wait for promise to be completed", func() {
			// For this case `p` will leak until the function returns.

			ctx, cancelFnc := context.WithCancel(context.Background())

			isPFinished := false
			p := New(context.Background(), func(ctx context.Context) (int, error) {
				time.Sleep(time.Millisecond * 200)
				isPFinished = true
				return 1, nil
			})

			go func() {
				<-time.After(time.Millisecond * 100)
				cancelFnc()
			}()

			now := time.Now()
			gotResult, err := Await(ctx, p)
			Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 100, 10))
			Expect(err).To(MatchError(context.Canceled))
			Expect(gotResult).To(BeZero())
			Expect(isPFinished).To(BeFalse())
		})
	})
})
