## 2023-10-24 - [Avoid Generic Envelope Boxing Allocations in Handlers]
**Learning:** Using generic `Response[Res]` structs in HTTP handlers causes a heap allocation on every enveloped response because passing a generic struct to `sonic.Encoder` (or any `any` interface) boxes it and forces it to escape to the heap.
**Action:** Use a handler-specific `sync.Pool` instantiated during `NewHandler[Req, Res]` to cache generic `Response[Res]` allocations. Zero out any pointers (`resp.Data = zero`) before returning the struct to the pool to prevent memory leaks from retained requests.

## 2026-03-14 - [Pool Generic Envelopes in Error func]
**Learning:** The `Error` func is also generating a new `&Response[any]{}` on every failure to envelope the response. Passing it to sonic encodes an allocation.
**Action:** Use a `sync.Pool` to reuse `Response[any]` objects within the `Error` function. This reduces allocations and speeds up the response encoding on the error path, keeping it consistent with the success path optimization.

## 2026-03-15 - [Avoid Unnecessary MaxBytesReader Allocation on Empty Body]
**Learning:** `http.MaxBytesReader` unconditionally wraps `r.Body` and creates a new object allocation. When handling standard read-only requests (like `GET`) that carry no payload (i.e. `r.Body == nil` or `r.Body == http.NoBody`), this allocation is unnecessary and adds overhead.
**Action:** Always check `r.Body != nil && r.Body != http.NoBody` before applying `http.MaxBytesReader` to minimize memory allocations.

## 2026-03-16 - [Replace Sonic NewEncoder with Marshal for Response Writing]
**Learning:** Using `sonic.ConfigDefault.NewEncoder(w).Encode(resp)` creates significant stream encoder overhead and memory allocations (approx. 45% of total allocs in JSON response benchmarks) compared to `sonic.ConfigDefault.Marshal(resp)` followed by `w.Write(data)`. `NewEncoder` is optimized for large streaming data, but for typical API responses, `Marshal` to a `[]byte` and a single `Write` is significantly faster and allocates less memory.
**Action:** Always prefer `sonic.ConfigDefault.Marshal` + `w.Write` over `sonic.ConfigDefault.NewEncoder(w).Encode` when writing JSON responses in HTTP handlers unless the response is a true large stream.

## 2026-03-18 - [Cache CORS Preflight Headers]
**Learning:** `strings.Join` inside middleware handler functions creates unnecessary memory allocations on every request. For static configuration like CORS `AllowedMethods` and `AllowedHeaders`, this work should be done once when the middleware is initialized.
**Action:** Always pre-calculate static string concatenations or joins outside of HTTP handler closures.

## 2026-03-21 - [Bypass CanonicalMIMEHeaderKey Overhead in Static Headers]
**Learning:** `w.Header().Set("Key", "Value")` incurs a hidden performance penalty because it calls `textproto.CanonicalMIMEHeaderKey` to format the key, and allocates a new `[]string{value}` every time. For static, frequently used HTTP headers (like security headers), this creates unnecessary allocations and CPU overhead on every request.
**Action:** For static headers, pre-allocate the slice (e.g., `var val = []string{"1"}`) and bypass `.Set()` by assigning directly to the underlying map: `w.Header()["Canonical-Key"] = val`. Ensure the key string used for map access is already canonicalized.

## 2026-03-22 - [Avoid Header.Set Allocations for Static Headers]
**Learning:** `w.Header().Set("Content-Type", "...")` creates unnecessary allocations and CPU overhead by performing string manipulation and canonicalization on every request. Direct map assignment with a pre-allocated string slice `w.Header()["Content-Type"] = preAllocatedSlice` is significantly faster.
**Action:** For frequently used static HTTP headers, avoid `w.Header().Set()` and pre-allocate string slices to assign directly to the header map using canonicalized keys.

## 2026-03-23 - [DANGER: Avoid Global Slice Mutation in Header Maps]
**Learning:** While bypassing `w.Header().Set("Key", "Value")` via direct map assignment `w.Header()["Key"] = sharedSlice` saves allocations, it introduces a critical data race and state corruption vulnerability if `sharedSlice` is a globally shared or closure-captured slice (e.g., `var corsValStar = []string{"*"}`). `w.Header().Set()` mutates the underlying slice array in-place. If any downstream middleware or handler calls `w.Header().Set()` on that key, it permanently mutates the global slice for all future requests, creating massive security and logic flaws.
**Action:** When bypassing `w.Header().Set()` to avoid `CanonicalMIMEHeaderKey` string formatting overhead, you MUST allocate a new slice *per request*: `w.Header()["Key"] = []string{value}`. This still saves CPU time and string allocation by skipping formatting while completely avoiding shared state mutations.

## 2025-02-17 - [Bypass CanonicalMIMEHeaderKey Overhead for X-Trace-ID and No-Vary-Search]
**Learning:** `w.Header().Set("X-Trace-ID", traceID)` and `w.Header().Set("No-Vary-Search", nvHeader)` incur hidden performance penalties because they call `textproto.CanonicalMIMEHeaderKey` to format the key, and allocate a new `[]string{value}` every time. Note that "X-Trace-ID" is not canonicalized (the canonicalized version is "X-Trace-Id"), so `Set` always creates a new string allocation.
**Action:** Use direct map assignment `w.Header()["X-Trace-Id"] = []string{traceID}` and `w.Header()["No-Vary-Search"] = []string{nvHeader}` to avoid `CanonicalMIMEHeaderKey` string formatting overhead and memory allocations. Ensure the key string used for map access is already canonicalized (e.g., "X-Trace-Id", not "X-Trace-ID").

## 2026-03-24 - [Avoid Slice Allocation for Static Headers]
**Learning:** Pre-allocating `[]string{value}` for static headers like `No-Vary-Search` outside the handler closure, and checking header existence with `len(w.Header()["Key"]) == 0` instead of `.Get()` completely eliminates per-request heap allocations and string formatting overhead for those headers.
**Action:** Always pre-calculate static header slices outside of the HTTP handler closure and assign them directly to the map.

## 2026-03-24 - [Avoid append on sonic.Marshal data]
**Learning:** `sonic.Marshal` returns a byte slice precisely sized to its contents (`cap == len`). Consequently, `append(data, '
')` will allocate a completely new backing array and copy the entire JSON payload into memory just to add a single newline character. This is a severe O(N) memory regression.
**Action:** Use consecutive `w.Write` calls instead of `append`, as `http.ResponseWriter` inherently buffers writes via `bufio.Writer`, making the consecutive writes highly efficient.

## 2026-04-19 - [Avoid Static Header Slice Allocation in CORS Middleware]
**Learning:** `strings.Join` inside middleware handler functions creates unnecessary memory allocations on every request. Even for static header definitions bypassed from `CanonicalMIMEHeaderKey` formatting using direct map assignment, dynamically allocating `[]string{value}` in every request within the middleware closure incurs small but frequent memory overhead (e.g. 10 allocs -> 7 allocs in preflight headers).
**Action:** Always pre-allocate string slices (e.g., `[]string{value}`) for static HTTP response headers outside of the middleware closure.
