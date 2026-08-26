# Closed-alpha early-access approval and signup grants

This service is the approval gate for the operator-assisted MycoOrigyn closed alpha. It stores marketing applications and issues authorization to begin one hosted-farm provisioning operation. It does not create tenants, databases, platform users, memberships, passwords, sessions, subscriptions, or billing records.

## Existing release contract

The repository's release process is unchanged:

1. Pull requests and pushes to `main` use `.github/workflows/test-and-build.yaml`.
2. The operator creates a semantic `v*.*.*` tag only after review.
3. `.github/workflows/release.yaml` builds and publishes the application and migration images.
4. That existing workflow updates the production image and migration-job tags in the established ArgoCD repository.
5. The migration image runs the normal `golang-migrate` files before the new application serves traffic.

Do not deploy this feature by inventing a different workflow. Migration `000003` is delivered by the existing migration image.

## Runtime configuration

Closed-alpha staging and production require:

```text
MARKETING_EMAIL_PROVIDER=resend
MARKETING_EMAIL_FROM=MycoOrigyn <notifications@configured-domain.example>
MARKETING_EMAIL_REPLY_TO=
MARKETING_RESEND_API_KEY_FILE=/run/secrets/marketing-resend-api-key
MARKETING_EARLY_ACCESS_REVIEW_RECIPIENT=reviewer@example.com
MARKETING_PUBLIC_WEB_ORIGIN=https://www.example.com
MARKETING_REVIEW_BASE_URL=https://www.example.com/early-access/review
MYCOORIGYN_HOSTED_SIGNUP_BASE_URL=https://www.example.com/signup
MARKETING_TOKEN_SECRET_ROOT=/var/lib/mycoorigyn-marketing/protected-tokens
MARKETING_PROVISIONING_SHARED_SECRET_FILE=/run/secrets/marketing-provisioning-shared-secret
```

The two secret files must be private regular files and must not be symlinks. The provisioning shared secret must contain at least 32 bytes on one line. The protected token root must be a durable, writable, private volume. PostgreSQL stores only SHA-256 token digests and opaque protected-file references; recoverable plaintext review and signup tokens are written atomically below that root with private permissions.

`MARKETING_PUBLIC_WEB_ORIGIN` is the environment-owned canonical browser origin. In staging and production it must be HTTPS, contain no credentials/path/query/fragment, and appear in `PUBLIC_CORS_ALLOWED_ORIGINS`. The review and signup bases must use that exact origin with `/early-access/review` and `/signup` respectively. This prevents a valid capability from being delivered to another environment or to a route that does not own its browser handoff.

Optional TTL configuration:

```text
MARKETING_REVIEW_TOKEN_TTL_SECONDS=604800
MARKETING_SIGNUP_GRANT_TTL_SECONDS=604800
MARKETING_SIGNUP_GRANT_CLAIM_TTL_SECONDS=1800
```

Local development defaults email delivery to disabled and the token root to `.local/marketing-tokens`.

## Public application contract

`POST /public/early-access` is unchanged. Public responses never reveal duplicate, approval, or account state.

An accepted `waitlist` submission is stored without creating a review capability or sending a review email. A newly persisted `early_access` submission receives `approval_status=pending`, one high-entropy review capability, and one reviewer notification attempt. Safe duplicate requests within the existing duplicate window reuse the application and do not send another notification.

Failure to deliver the reviewer notification does not discard the application. Operations receive a bounded log event without the recipient, token, URL, provider body, or credential.

## Landing review page contract

The reviewer email links to:

```text
<MARKETING_REVIEW_BASE_URL>#token=<review-token>
```

The landing frontend must:

1. Read the token from the URL fragment.
2. Immediately remove the fragment with `history.replaceState`.
3. Keep the token only in bounded in-memory state; never use query parameters or browser storage.
4. Resolve the application with `POST /public/early-access/review/resolve` and body `{"token":"..."}`.
5. Render only the returned application projection.
6. Require a deliberate click on **Approve Early Access** or **Decline**.
7. Approve with `POST /public/early-access/review/approve` and the same JSON body.
8. Decline with `POST /public/early-access/review/decline` and the same JSON body.

No GET route performs review resolution or mutation. An invalid, expired, revoked, wrong, or already-terminal capability cannot enumerate application details or change a decision. Repeating the same approve mutation is a bounded idempotent delivery retry: it cannot change the decision and reuses the existing signup grant and protected token. The opposite terminal decision returns a conflict.

Successful approval returns:

```json
{"status":"approved","delivery_status":"delivered"}
```

If the applicant email fails after approval, durable approval and the active grant remain. The API returns `503 approval_delivery_failed`; retry the exact same approval mutation with the retained in-memory review token. Do not reload the page or request a replacement grant. Decline returns `{"status":"declined"}` and creates no grant.

## Applicant email and landing signup handoff

Approval sends both HTML and plain text with this fragment URL:

```text
<MYCOORIGYN_HOSTED_SIGNUP_BASE_URL>#access=<signup-grant-token>
```

The future signup frontend must capture and immediately remove `#access`, retain it only in memory, collect or confirm the approved email, and hand both values to the main MycoOrigyn application's future signup coordinator. The marketing API never returns grant tokens from a normal JSON response.

## Provisioning service contract

The following routes are server-to-server only and require:

```text
Authorization: Bearer <contents of MARKETING_PROVISIONING_SHARED_SECRET_FILE>
Content-Type: application/json
```

The bearer value is constant-time compared and never belongs in a URL or log.

### Resolve approved application metadata

`POST /internal/signup-grants/resolve`

```json
{"token":"..."}
```

For an active, unexpired Early Access grant, resolution returns only the
approved email, applicant name, and farm name already stored on the associated
submission. It does not claim or consume the grant, create a signup operation,
or return internal identifiers. This lets hosted signup derive the approved
identity server-side while treating owner and farm names as editable defaults.

### Validate

`POST /internal/signup-grants/validate`

```json
{"token":"...","email":"approved@example.com"}
```

Validation verifies the token digest, normalized approved email, status, and expiration. It does not consume or reserve the grant.

### Claim

`POST /internal/signup-grants/claim`

```json
{
  "token":"...",
  "email":"approved@example.com",
  "claim_reference":"stable-provisioning-operation-reference"
}
```

Claim immediately before privileged provisioning. The reference must be stable for retries and is stored only as a digest. The same reference is idempotent. A different reference cannot claim until the bounded lease expires.

### Consume

`POST /internal/signup-grants/consume` with the same three fields.

Consume only after tenant provisioning and all required durable registrations have committed. Consumption atomically changes the claimed grant to `consumed`. The consuming claim-reference digest remains on the consumed record solely so a retry with the same stable provisioning reference can reconcile a lost response as success. The plaintext claim reference is never stored, a different claim cannot replay consumption, and release or claim is never valid after consumption. An idempotent consume replay does not depend on the former claim lease and does not change the original consumption timestamp. Successful consumption triggers best-effort, replay-safe removal of the protected plaintext grant token.

### Release

`POST /internal/signup-grants/release` with the same three fields.

If provisioning fails before its durable commit boundary, release the matching claim. The grant returns to `active` and the same approved user may safely retry. A mismatched operation cannot release another worker's claim.

The intended sequence is:

```text
validate
→ claim with stable provisioning operation reference
→ provision and durably commit the hosted farm
→ consume
```

On a pre-commit failure:

```text
release matching claim
→ retry later with the same grant
```

This reservation state prevents two provisioning workers from independently creating farms after concurrent non-consuming validations.

## Entitlement boundary

A signup grant answers only: “May this normalized identity create one hosted farm?” Its source is currently `early_access_approval`. Future sources may include subscription entitlement, trial entitlement, promotional invitation, or manual support approval. Tenant provisioning must depend on the grant interface, not on marketing submission tables.

## Production-hardening follow-up

The existing body limit, strict JSON decoder, honeypot, and duplicate window remain in place. Before broad public promotion, add an infrastructure/application rate limit, CAPTCHA or comparable bot protection, and alerting for submission and email-delivery abuse. These are intentionally not introduced as new third-party infrastructure in the closed-alpha foundation.
