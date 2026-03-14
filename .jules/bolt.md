## 2023-10-24 - [Avoid Generic Envelope Boxing Allocations in Handlers]
**Learning:** Using generic `Response[Res]` structs in HTTP handlers causes a heap allocation on every enveloped response because passing a generic struct to `sonic.Encoder` (or any `any` interface) boxes it and forces it to escape to the heap.
**Action:** Use a handler-specific `sync.Pool` instantiated during `NewHandler[Req, Res]` to cache generic `Response[Res]` allocations. Zero out any pointers (`resp.Data = zero`) before returning the struct to the pool to prevent memory leaks from retained requests.

## 2026-03-14 - [Pool Generic Envelopes in Error func]
**Learning:** The `Error` func is also generating a new `&Response[any]{}` on every failure to envelope the response. Passing it to sonic encodes an allocation.
**Action:** Use a `sync.Pool` to reuse `Response[any]` objects within the `Error` function. This reduces allocations and speeds up the response encoding on the error path, keeping it consistent with the success path optimization.
