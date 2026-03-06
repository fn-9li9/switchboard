CREATE TABLE IF NOT EXISTS events (
    id         BIGSERIAL    PRIMARY KEY,
    source     TEXT         NOT NULL,
    topic      TEXT         NOT NULL,
    payload    JSONB        NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_events_topic      ON events (topic);
CREATE INDEX idx_events_created_at ON events (created_at DESC);
CREATE INDEX idx_events_source     ON events (source);