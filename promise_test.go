package promise

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Promise", func() {
	wantErr := errors.New("some error")

	When("the promise is completed immediately", func() {
		When("the promise returns a value", func() {
			It("should be fulfilled", func() {
				ctx := context.Background()

				p := New(ctx, func(context.Context) (int, error) {
					return 1, nil
				})

				select {
				case <-p.ch:
				// Promise done
				case <-time.After(time.Millisecond * 100):
					Fail("the promise should be completed immediately")
				}
				Expect(p.ch).To(BeClosed())
				Expect(p.state).To(Equal(fulfilled))
				Expect(p.result).To(Equal(1))
				Expect(p.err).ToNot(HaveOccurred())
			})
		})

		When("the promise returns an error", func() {
			It("should be rejected", func() {
				ctx := context.Background()

				p := New(ctx, func(context.Context) (int, error) {
					return 0, wantErr
				})

				select {
				case <-p.ch:
				// Promise done
				case <-time.After(time.Millisecond * 100):
					Fail("the promise should be completed immediately")
				}

				Expect(p.ch).To(BeClosed())
				Expect(p.state).To(Equal(rejected))
				Expect(p.result).To(Equal(0))
				Expect(p.err).To(MatchError(wantErr))
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

				select {
				case <-p.ch:
				// Promise done
				case <-time.After(time.Millisecond * 200):
					Fail("the promise should have been completed")
				}

				Expect(p.ch).To(BeClosed())
				Expect(p.state).To(Equal(fulfilled))
				Expect(p.result).To(Equal(1))
				Expect(p.err).ToNot(HaveOccurred())
			})
		})

		When("the promise returns an error", func() {
			It("should be rejected", func() {
				ctx := context.Background()

				p := New(ctx, func(context.Context) (int, error) {
					time.Sleep(time.Millisecond * 100)
					return 0, wantErr
				})

				select {
				case <-p.ch:
				// Promise done
				case <-time.After(time.Millisecond * 200):
					Fail("the promise should have been completed")
				}

				Expect(p.ch).To(BeClosed())
				Expect(p.state).To(Equal(rejected))
				Expect(p.result).To(Equal(0))
				Expect(p.err).To(MatchError(wantErr))
			})
		})
	})

	When("the context is called before the function returns", func() {
		When("f is cancelable", func() {
			It("should cancel the promise execution", func() {
				ctx, cancelFnc := context.WithTimeout(context.Background(), time.Millisecond*100)
				defer cancelFnc()

				p := New(ctx, func(ctx context.Context) (int, error) {
					select {
					case <-ctx.Done():
						return 0, ctx.Err()
					case <-time.After(time.Millisecond * 200):
					}
					return 1, nil
				})

				now := time.Now()
				Eventually(func() state {
					return p.state
				}).
					Within(time.Millisecond * 120).
					WithPolling(time.Millisecond).
					Should(Equal(rejected))

				Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 100, 10))
				Expect(p.ch).To(BeClosed())
				Expect(p.state).To(Equal(rejected))
				Expect(p.result).To(Equal(0))
				Expect(p.err).To(MatchError(context.DeadlineExceeded))
			})
		})

		When("f is not cancelable", func() {
			When("f returns a value", func() {
				It("should eventually fulfill the promise", func() {
					ctx, cancelFnc := context.WithCancel(context.Background())

					p := New(ctx, func(ctx context.Context) (int, error) {
						time.Sleep(time.Millisecond * 200)
						return 1, nil
					})

					go func() {
						<-time.After(time.Millisecond * 100)
						cancelFnc()
					}()

					now := time.Now()
					Consistently(func() state {
						return p.state
					}).
						Within(time.Millisecond * 199).
						WithPolling(time.Millisecond).
						Should(Equal(pending))

					Eventually(func() state {
						return p.state
					}).
						Within(time.Millisecond * 100).
						WithPolling(time.Millisecond).
						Should(Equal(fulfilled))

					Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 200, 10))
					Expect(p.ch).To(BeClosed())
					Expect(p.state).To(Equal(fulfilled))
					Expect(p.result).To(Equal(1))
					Expect(p.err).ToNot(HaveOccurred())
				})
			})

			When("f returns a value", func() {
				It("should eventually fulfill the promise", func() {
					ctx, cancelFnc := context.WithCancel(context.Background())

					p := New(ctx, func(ctx context.Context) (int, error) {
						time.Sleep(time.Millisecond * 200)
						return 0, wantErr
					})

					go func() {
						<-time.After(time.Millisecond * 100)
						cancelFnc()
					}()

					now := time.Now()
					Consistently(func() state {
						return p.state
					}).
						Within(time.Millisecond * 199).
						WithPolling(time.Millisecond).
						Should(Equal(pending))

					Eventually(func() state {
						return p.state
					}).
						Within(time.Millisecond * 100).
						WithPolling(time.Millisecond).
						Should(Equal(rejected))

					Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 200, 10))
					Expect(p.ch).To(BeClosed())
					Expect(p.state).To(Equal(rejected))
					Expect(p.result).To(Equal(0))
					Expect(p.err).To(MatchError(wantErr))
				})
			})
		})
	})

	When("the promise panics", func() {
		When("the panic is an error", func() {
			It("should be rejected with the panic error", func() {
				ctx := context.Background()

				p := New(ctx, func(context.Context) (int, error) {
					panic(wantErr)
				})

				select {
				case <-p.ch:
				// Promise done
				case <-time.After(time.Millisecond * 100):
					Fail("the promise should have been completed")
				}

				Expect(p.ch).To(BeClosed())
				Expect(p.state).To(Equal(rejected))
				Expect(p.result).To(Equal(0))
				Expect(p.err).To(MatchError(wantErr))
			})
		})

		When("the panic is NOT an error", func() {
			PIt("should be rejected with the panic error", func() {
				ctx := context.Background()

				p := New(ctx, func(context.Context) (int, error) {
					panic("some panic")
				})

				select {
				case <-p.ch:
				// Promise done
				case <-time.After(time.Millisecond * 100):
					Fail("the promise should have been completed")
				}

				Expect(p.ch).To(BeClosed())
				Expect(p.state).To(Equal(rejected))
				Expect(p.result).To(Equal(0))
				Expect(p.err).To(MatchError("some panic"))
			})
		})
	})
})

var _ = Describe("Resolved", func() {
	When("the promise is fulfilled", func() {
		It("should return true", func() {
			p := Resolved(1)

			Expect(p.ch).To(BeNil())
			Expect(p.state).To(Equal(fulfilled))
			Expect(p.result).To(Equal(1))
			Expect(p.err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("Rejected", func() {
	wantErr := errors.New("some error")

	When("the promise is rejected", func() {
		It("should return true", func() {
			p := Rejected[int](wantErr)

			Expect(p.ch).To(BeNil())
			Expect(p.state).To(Equal(rejected))
			Expect(p.result).To(BeZero())
			Expect(p.err).To(MatchError(wantErr))
		})
	})
})
