## 2023-10-24 - [Avoid Generic Envelope Boxing Allocations in Handlers]
**Learning:** Using generic `Response[Res]` structs in HTTP handlers causes a heap allocation on every enveloped response because passing a generic struct to `sonic.Encoder` (or any `any` interface) boxes it and forces it to escape to the heap.
**Action:** Use a handler-specific `sync.Pool` instantiated during `NewHandler[Req, Res]` to cache generic `Response[Res]` allocations. Zero out any pointers (`resp.Data = zero`) before returning the struct to the pool to prevent memory leaks from retained requests.
