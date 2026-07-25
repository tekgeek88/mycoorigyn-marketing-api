CREATE TABLE IF NOT EXISTS page_view_counters (
    page_path TEXT PRIMARY KEY,
    total_visits BIGINT NOT NULL DEFAULT 0 CHECK (total_visits >= 0),
    unique_visitors BIGINT NOT NULL DEFAULT 0 CHECK (unique_visitors >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS page_view_visitors (
    page_path TEXT NOT NULL REFERENCES page_view_counters(page_path) ON DELETE CASCADE,
    visitor_id TEXT NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (page_path, visitor_id)
);

CREATE INDEX IF NOT EXISTS page_view_counters_updated_at_idx
    ON page_view_counters(updated_at);

CREATE INDEX IF NOT EXISTS page_view_visitors_last_seen_at_idx
    ON page_view_visitors(last_seen_at);
