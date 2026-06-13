package promise

import (
	"context"
	"errors"
	"runtime"
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
				Expect(p.loadState()).To(Equal(fulfilled))
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
				Expect(p.loadState()).To(Equal(rejected))
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
				Expect(p.loadState()).To(Equal(fulfilled))
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
				Expect(p.loadState()).To(Equal(rejected))
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
				// loadState reads atomically, so polling it is race-free.
				Eventually(func() state {
					return p.loadState()
				}).
					Within(time.Millisecond * 120).
					WithPolling(time.Millisecond).
					Should(Equal(rejected))

				Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 100, 20))
				// The state Store happens-before close(p.ch) in program order, so
				// observing the settled state via loadState does not guarantee the
				// deferred close has run yet. Synchronize on the channel.
				Eventually(p.ch).Should(BeClosed())
				Expect(p.loadState()).To(Equal(rejected))
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
						return p.loadState()
					}).
						Within(time.Millisecond * 190).
						WithPolling(time.Millisecond).
						Should(Equal(pending))

					Eventually(func() state {
						return p.loadState()
					}).
						Within(time.Millisecond * 100).
						WithPolling(time.Millisecond).
						Should(Equal(fulfilled))

					Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 200, 20))
					Eventually(p.ch).Should(BeClosed())
					Expect(p.loadState()).To(Equal(fulfilled))
					Expect(p.result).To(Equal(1))
					Expect(p.err).ToNot(HaveOccurred())
				})
			})

			When("f returns an error", func() {
				It("should eventually reject the promise", func() {
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
						return p.loadState()
					}).
						Within(time.Millisecond * 190).
						WithPolling(time.Millisecond).
						Should(Equal(pending))

					Eventually(func() state {
						return p.loadState()
					}).
						Within(time.Millisecond * 100).
						WithPolling(time.Millisecond).
						Should(Equal(rejected))

					Expect(time.Since(now).Milliseconds()).To(BeNumerically("~", 200, 20))
					Eventually(p.ch).Should(BeClosed())
					Expect(p.loadState()).To(Equal(rejected))
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
				Expect(p.loadState()).To(Equal(rejected))
				Expect(p.result).To(Equal(0))
				Expect(p.err).To(MatchError(wantErr))
			})
		})

		When("the panic is NOT an error", func() {
			It("should be rejected with a wrapped panic error", func() {
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
				Expect(p.loadState()).To(Equal(rejected))
				Expect(p.result).To(Equal(0))
				Expect(p.err).To(HaveOccurred())
				Expect(p.err.Error()).To(ContainSubstring("some panic"))
			})
		})

		When("the panic value is nil", func() {
			It("should be rejected with runtime.PanicNilError", func() {
				// With the go.mod directive >= 1.21 the runtime turns panic(nil)
				// into *runtime.PanicNilError, which the recover handler treats
				// as a regular error panic. This pins that behavior.
				ctx := context.Background()

				p := New(ctx, func(context.Context) (int, error) {
					panic(nil)
				})

				gotResult, err := Await(ctx, p)
				var nilPanic *runtime.PanicNilError
				Expect(errors.As(err, &nilPanic)).To(BeTrue(), "expected *runtime.PanicNilError, got %v", err)
				Expect(gotResult).To(BeZero())
				Expect(p.loadState()).To(Equal(rejected))
			})
		})
	})

	When("the promise function exits without returning", func() {
		When("f calls runtime.Goexit", func() {
			It("should be rejected with ErrExitedWithoutResult", func() {
				// Goexit runs deferred functions but is not a panic, so recover
				// returns nil. The settle channel must still close and the
				// promise must reject rather than report ErrInvalidState.
				ctx := context.Background()

				p := New(ctx, func(context.Context) (int, error) {
					runtime.Goexit()
					return 1, nil // unreachable
				})

				gotResult, err := Await(ctx, p)
				Expect(err).To(MatchError(ErrExitedWithoutResult))
				Expect(gotResult).To(BeZero())
				Expect(p.ch).To(BeClosed())
				Expect(p.loadState()).To(Equal(rejected))
			})
		})
	})

	When("New is given a nil function", func() {
		It("should reject with the runtime nil-call error instead of crashing", func() {
			ctx := context.Background()

			p := New[int](ctx, nil)

			gotResult, err := Await(ctx, p)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("nil"))
			Expect(gotResult).To(BeZero())
			Expect(p.loadState()).To(Equal(rejected))
		})
	})
})

var _ = Describe("state", func() {
	It("stringifies every known value and unknown values", func() {
		Expect(pending.String()).To(Equal("pending"))
		Expect(fulfilled.String()).To(Equal("fulfilled"))
		Expect(rejected.String()).To(Equal("rejected"))
		Expect(state(99).String()).To(Equal("unknown"))
	})
})

var _ = Describe("Resolved", func() {
	It("should build a fulfilled promise without a channel or goroutine", func() {
		p := Resolved(1)

		Expect(p.ch).To(BeNil())
		Expect(p.loadState()).To(Equal(fulfilled))
		Expect(p.result).To(Equal(1))
		Expect(p.err).ToNot(HaveOccurred())
	})
})

var _ = Describe("Rejected", func() {
	wantErr := errors.New("some error")

	It("should build a rejected promise without a channel or goroutine", func() {
		p := Rejected[int](wantErr)

		Expect(p.ch).To(BeNil())
		Expect(p.loadState()).To(Equal(rejected))
		Expect(p.result).To(BeZero())
		Expect(p.err).To(MatchError(wantErr))
	})
})
