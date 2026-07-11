DROP TRIGGER IF EXISTS set_early_access_submissions_updated_at ON early_access_submissions;
DROP TABLE IF EXISTS early_access_submissions;
DROP FUNCTION IF EXISTS set_updated_at();
DROP EXTENSION IF EXISTS pgcrypto;
