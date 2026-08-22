ALTER TABLE early_access_submissions
    ADD COLUMN approval_status TEXT NULL,
    ADD COLUMN reviewed_at TIMESTAMPTZ NULL,
    ADD COLUMN approved_at TIMESTAMPTZ NULL,
    ADD COLUMN declined_at TIMESTAMPTZ NULL;

UPDATE early_access_submissions
SET approval_status = 'pending'
WHERE submission_type = 'early_access';

ALTER TABLE early_access_submissions
    ADD CONSTRAINT early_access_submissions_approval_state_check
    CHECK (
        (
            submission_type = 'waitlist'
            AND approval_status IS NULL
            AND reviewed_at IS NULL
            AND approved_at IS NULL
            AND declined_at IS NULL
        )
        OR
        (
            submission_type = 'early_access'
            AND approval_status IN ('pending', 'approved', 'declined')
            AND (
                (approval_status = 'pending' AND reviewed_at IS NULL AND approved_at IS NULL AND declined_at IS NULL)
                OR
                (approval_status = 'approved' AND reviewed_at IS NOT NULL AND approved_at IS NOT NULL AND declined_at IS NULL)
                OR
                (approval_status = 'declined' AND reviewed_at IS NOT NULL AND approved_at IS NULL AND declined_at IS NOT NULL)
            )
        )
    );

CREATE INDEX early_access_submissions_approval_status_idx
    ON early_access_submissions (approval_status)
    WHERE submission_type = 'early_access';

CREATE TABLE early_access_review_capabilities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    early_access_submission_id UUID NOT NULL UNIQUE
        REFERENCES early_access_submissions(id) ON DELETE RESTRICT,
    token_digest BYTEA NOT NULL UNIQUE CHECK (octet_length(token_digest) = 32),
    secret_reference TEXT NOT NULL UNIQUE CHECK (btrim(secret_reference) <> ''),
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'decided', 'revoked', 'expired')),
    expires_at TIMESTAMPTZ NOT NULL,
    decided_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (
        (status = 'active' AND decided_at IS NULL AND revoked_at IS NULL)
        OR (status = 'decided' AND decided_at IS NOT NULL AND revoked_at IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL)
        OR (status = 'expired' AND decided_at IS NULL AND revoked_at IS NULL)
    )
);

CREATE INDEX early_access_review_capabilities_expires_at_idx
    ON early_access_review_capabilities (expires_at)
    WHERE status = 'active';

CREATE TRIGGER set_early_access_review_capabilities_updated_at
BEFORE UPDATE ON early_access_review_capabilities
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE TABLE signup_grants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    early_access_submission_id UUID NOT NULL UNIQUE
        REFERENCES early_access_submissions(id) ON DELETE RESTRICT,
    approved_email TEXT NOT NULL CHECK (btrim(approved_email) <> ''),
    token_digest BYTEA NOT NULL UNIQUE CHECK (octet_length(token_digest) = 32),
    secret_reference TEXT NOT NULL UNIQUE CHECK (btrim(secret_reference) <> ''),
    source TEXT NOT NULL DEFAULT 'early_access_approval'
        CHECK (source = 'early_access_approval'),
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'claimed', 'consumed', 'revoked', 'expired')),
    expires_at TIMESTAMPTZ NOT NULL,
    claimed_at TIMESTAMPTZ NULL,
    claim_expires_at TIMESTAMPTZ NULL,
    claim_reference_digest BYTEA NULL
        CHECK (claim_reference_digest IS NULL OR octet_length(claim_reference_digest) = 32),
    consumed_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (
        (status = 'active' AND claimed_at IS NULL AND claim_expires_at IS NULL AND claim_reference_digest IS NULL AND consumed_at IS NULL AND revoked_at IS NULL)
        OR
        (status = 'claimed' AND claimed_at IS NOT NULL AND claim_expires_at IS NOT NULL AND claim_reference_digest IS NOT NULL AND consumed_at IS NULL AND revoked_at IS NULL)
        OR
        (status = 'consumed' AND consumed_at IS NOT NULL AND revoked_at IS NULL)
        OR
        (status = 'revoked' AND revoked_at IS NOT NULL AND consumed_at IS NULL)
        OR
        (status = 'expired' AND consumed_at IS NULL AND revoked_at IS NULL)
    )
);

CREATE INDEX signup_grants_status_expires_at_idx
    ON signup_grants (status, expires_at);

CREATE TRIGGER set_signup_grants_updated_at
BEFORE UPDATE ON signup_grants
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();
