## 2023-10-24 - [Avoid Generic Envelope Boxing Allocations in Handlers]
**Learning:** Using generic `Response[Res]` structs in HTTP handlers causes a heap allocation on every enveloped response because passing a generic struct to `sonic.Encoder` (or any `any` interface) boxes it and forces it to escape to the heap.
**Action:** Use a handler-specific `sync.Pool` instantiated during `NewHandler[Req, Res]` to cache generic `Response[Res]` allocations. Zero out any pointers (`resp.Data = zero`) before returning the struct to the pool to prevent memory leaks from retained requests.

## 2026-03-14 - [Pool Generic Envelopes in Error func]
**Learning:** The `Error` func is also generating a new `&Response[any]{}` on every failure to envelope the response. Passing it to sonic encodes an allocation.
**Action:** Use a `sync.Pool` to reuse `Response[any]` objects within the `Error` function. This reduces allocations and speeds up the response encoding on the error path, keeping it consistent with the success path optimization.

## 2026-03-15 - [Avoid Unnecessary MaxBytesReader Allocation on Empty Body]
**Learning:** `http.MaxBytesReader` unconditionally wraps `r.Body` and creates a new object allocation. When handling standard read-only requests (like `GET`) that carry no payload (i.e. `r.Body == nil` or `r.Body == http.NoBody`), this allocation is unnecessary and adds overhead.
**Action:** Always check `r.Body != nil && r.Body != http.NoBody` before applying `http.MaxBytesReader` to minimize memory allocations.
