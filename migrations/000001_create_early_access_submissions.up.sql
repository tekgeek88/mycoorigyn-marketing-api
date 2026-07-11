CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE IF NOT EXISTS early_access_submissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    submission_type TEXT NOT NULL CHECK (submission_type IN ('waitlist', 'early_access')),
    email TEXT NOT NULL CHECK (btrim(email) <> ''),
    name TEXT NULL,
    farm_name TEXT NULL,
    farm_type TEXT NULL,
    production_scale TEXT NULL,
    current_tracking_method TEXT NULL,
    features_of_interest TEXT[] NULL,
    interested_in_testing BOOLEAN NULL,
    message TEXT NULL,
    source TEXT NOT NULL DEFAULT 'marketing_site' CHECK (btrim(source) <> ''),
    status TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'contacted', 'testing', 'closed')),
    payload_fingerprint TEXT NOT NULL CHECK (btrim(payload_fingerprint) <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS early_access_submissions_email_idx
    ON early_access_submissions (email);

CREATE INDEX IF NOT EXISTS early_access_submissions_status_idx
    ON early_access_submissions (status);

CREATE INDEX IF NOT EXISTS early_access_submissions_created_at_idx
    ON early_access_submissions (created_at DESC);

CREATE INDEX IF NOT EXISTS early_access_submissions_payload_fingerprint_idx
    ON early_access_submissions (payload_fingerprint);

DROP TRIGGER IF EXISTS set_early_access_submissions_updated_at ON early_access_submissions;

CREATE TRIGGER set_early_access_submissions_updated_at
BEFORE UPDATE ON early_access_submissions
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();
