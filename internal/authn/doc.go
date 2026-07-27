// Package authn implements strong-auth approvals for high-risk actions
// (docs/PLAN.md Task 25 / Constitution C12): OIDC device-code login for
// `foundry login`, session JWTs bound to the verified principal, WebAuthn
// step-up for the approval endpoint, and the C11 guard that Telegram is
// never a valid high-risk approval surface.
//
// No cryptography is hand-rolled here — OIDC discovery and ID-token
// verification go through github.com/coreos/go-oidc/v3, session JWT
// signing/verification through github.com/lestrrat-go/jwx/v3, and
// WebAuthn ceremonies through github.com/go-webauthn/webauthn (this
// task's own Boundary: "no self-built crypto; libraries only").
package authn
