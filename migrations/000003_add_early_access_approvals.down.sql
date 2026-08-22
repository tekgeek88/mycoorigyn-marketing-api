DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM signup_grants)
       OR EXISTS (SELECT 1 FROM early_access_review_capabilities)
       OR EXISTS (
           SELECT 1
           FROM early_access_submissions
           WHERE approval_status IN ('approved', 'declined')
       ) THEN
        RAISE EXCEPTION 'refusing to remove early-access approval state while durable review or signup-grant records exist';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS set_signup_grants_updated_at ON signup_grants;
DROP TABLE IF EXISTS signup_grants;

DROP TRIGGER IF EXISTS set_early_access_review_capabilities_updated_at ON early_access_review_capabilities;
DROP TABLE IF EXISTS early_access_review_capabilities;

DROP INDEX IF EXISTS early_access_submissions_approval_status_idx;

ALTER TABLE early_access_submissions
    DROP CONSTRAINT IF EXISTS early_access_submissions_approval_state_check,
    DROP COLUMN IF EXISTS declined_at,
    DROP COLUMN IF EXISTS approved_at,
    DROP COLUMN IF EXISTS reviewed_at,
    DROP COLUMN IF EXISTS approval_status;
