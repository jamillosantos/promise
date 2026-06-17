# promise

A tiny generic promise library for Go. Run a function on its own goroutine and
`Await` the result later, with first-class support for `context` cancellation,
panic recovery, and pre-settled values.

```bash
go get github.com/jamillosantos/promise
```

Requires Go 1.22+.

## Why

Sometimes you want to kick off work now and collect the result somewhere else,
without hand-rolling a channel and a goroutine every time. `promise` wraps that
pattern:

- `New` starts `f` immediately on a goroutine and hands you a `*Promise[T]`.
- `Await` blocks until the promise settles **or** the passed context is done.
- Many goroutines can `Await` the same promise; each gets the same result.
- A panic inside `f` becomes a rejection instead of crashing the process.

## Usage

### Create and await

`New` runs `f` right away. `Await` returns the result once `f` finishes.

```go
ctx := context.Background()

p := promise.New(ctx, func(ctx context.Context) (int, error) {
    return expensiveComputation(ctx)
})

// ... do other work while p runs ...

result, err := promise.Await(ctx, p)
if err != nil {
    // f returned an error, panicked, or ctx was canceled.
    return err
}
fmt.Println("got", result)
```

The type parameter is inferred from `f`'s return value, so `New` /
`Await` are fully generic with no casting.

### Cancellation

`Await` honors the context passed *to it*. If that context is canceled before
the promise settles, `Await` returns `ctx.Err()` immediately:

```go
ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
defer cancel()

p := promise.New(context.Background(), func(ctx context.Context) (int, error) {
    time.Sleep(time.Second) // slow
    return 1, nil
})

_, err := promise.Await(ctx, p)
// err == context.DeadlineExceeded after ~50ms; the promise keeps running.
```

Two independent contexts are in play:

- The context passed to **`New`** is forwarded to `f`. Make `f` actually watch
  it (`<-ctx.Done()`) if you want the underlying work to stop. If `f` ignores
  it, the goroutine runs to completion regardless — see *Leaks* below.
- The context passed to **`Await`** only controls how long *this* caller waits.

If the promise has already settled, `Await` returns the result even when its
context is also done — a completed value is never thrown away in favor of
`ctx.Err()`.

### Pre-settled promises

`Resolved` and `Rejected` build a promise that is already settled. They spawn
no goroutine, and `Await` on them returns instantly (ignoring the context).
Useful for stubbing, early returns, and tests.

```go
ok  := promise.Resolved(42)
bad := promise.Rejected[int](errors.New("nope"))

v, _   := promise.Await(ctx, ok)  // 42, nil — immediately
_, err := promise.Await(ctx, bad) // 0, "nope" — immediately
```

### Panics

If `f` panics, the promise is rejected instead of taking down the process:

- `panic(err)` where the value is an `error` → that error is the rejection.
- `panic(anything else)` → wrapped as `fmt.Errorf("promise panicked: %v", v)`.

```go
p := promise.New(ctx, func(ctx context.Context) (int, error) {
    panic("boom")
})

_, err := promise.Await(ctx, p)
// err.Error() == "promise panicked: boom"
```

### Fan-out: one promise, many awaiters

A single promise can be awaited from any number of goroutines. Every awaiter
observes the same settled result.

```go
p := promise.New(ctx, fetchConfig)

var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        cfg, err := promise.Await(ctx, p) // all 100 get the same cfg/err
        _ = cfg
        _ = err
    }()
}
wg.Wait()
```

## API

| Function | Description |
| --- | --- |
| `New[T](ctx, f) *Promise[T]` | Runs `f(ctx)` on a goroutine; settles with its `(T, error)`. |
| `Await[T](ctx, p) (T, error)` | Blocks until `p` settles or `ctx` is done. |
| `Resolved[T](v) *Promise[T]` | Already-fulfilled promise holding `v`. No goroutine. |
| `Rejected[T](err) *Promise[T]` | Already-rejected promise holding `err`. No goroutine. |

### Sentinel errors

- `ErrExitedWithoutResult` — `f` exited without returning a value: it called
  `runtime.Goexit`, or it panicked with `nil` under the legacy
  `GODEBUG=panicnil=1` semantics where `recover` reports no panic.
- `ErrInvalidState` — returned by `Await` for a promise that has no settle
  channel and is still pending. Cannot arise from `New` / `Resolved` /
  `Rejected`; it only surfaces misuse such as awaiting a zero-value `Promise`.

## Caveats

- **Leaks.** `New` does not cancel `f` for you. If `f` ignores its context and
  the awaiting context is canceled, `Await` returns early but the goroutine
  running `f` lives until `f` returns. Make long-running `f` cancelable.
- **Shared result.** Every awaiter receives the same `T`. If `T` is a pointer,
  slice, or map, awaiters share the underlying data — copy before mutating.

## Testing

The suite is written with [Ginkgo](https://onsi.github.io/ginkgo/) /
[Gomega](https://onsi.github.io/gomega/) and includes dedicated concurrency,
stress, and goroutine-leak specs. Because the library is built around
goroutine synchronization, **always run the tests with the race detector.**

```bash
# Standard run, with the race detector (the baseline gate).
go test -race ./...

# Coverage report.
go test -coverprofile=cover.out ./... && go tool cover -func=cover.out
```

### Shaking out flaky interleavings

Statement coverage is 100%, but races only surface under specific schedules.
To hunt for scheduling-dependent failures, repeat the suite and vary the
scheduler. Note that `go test -count=N` is rejected by Ginkgo for repeats; use
the Ginkgo CLI instead:

```bash
go install github.com/onsi/ginkgo/v2/ginkgo@latest

# Re-run the whole suite under -race until it fails (or for N repeats).
ginkgo -race -until-it-fails ./...
ginkgo -race -repeat=20 ./...

# Vary parallelism to expose ordering assumptions.
GOMAXPROCS=1 go test -race ./...
GOMAXPROCS=2 go test -race ./...
GOMAXPROCS=4 go test -race ./...
```

### Benchmarks

```bash
go test -run '^$' -bench . -benchmem ./...
```
