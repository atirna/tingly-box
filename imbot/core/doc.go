// Package core is the platform-neutral vocabulary and base machinery for
// imbot. It defines the contracts every messaging platform implements — the
// [Bot] interface, inbound [Message] and its [Content] variants, outbound
// [ActionSet] interactions, the [PlatformDescriptor] capability table, the
// command registry, and the TOFU pairing manager — and the [BaseBot] helpers
// shared by every platform implementation.
//
// Nothing in this package imports a concrete platform SDK or a platform
// package. The dependency direction is one-way: platform implementations and
// the top-level imbot runtime depend on core; core depends on none of them.
// Keeping core free of platform knowledge is what makes adding a platform a
// purely additive change.
//
// Design rationale for actions, payloads, capabilities, and message restating
// lives in .design/imbot-platform-seams.md at the repo root.
package core
