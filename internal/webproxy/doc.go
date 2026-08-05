// Package webproxy is the web proxy plugin: when a downstream model has no
// web access of its own, it borrows the web access of a configured
// {provider, model} pair.
//
// The plugin has two halves, both driven by the same resolved service:
//
//   - Request side (ToolTransform, transform.go): native web_search /
//     web_fetch tool declarations are removed from the upstream-bound request
//     — the downstream provider cannot execute them — and replaced by two
//     plain function tools any model can call.
//   - Execution side (Service.Execute, execute.go): when the downstream model
//     calls one of those function tools, the search or fetch runs against the
//     borrowed service and only its answer is fed back as a tool result. The
//     client never sees the tool call.
//
// Resolve (resolve.go) picks the effective {provider, model} for a request —
// rule level wins over scenario level — mirroring internal/visionproxy. See
// .design/web-proxy.md for the full design and README.md for the request /
// execution pipeline.
package webproxy
