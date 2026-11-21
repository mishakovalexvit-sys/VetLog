CREATE TABLE leads (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT        NOT NULL,
    phone      TEXT        NOT NULL,
    clinic     TEXT        NOT NULL,
    email      TEXT,
    lang       TEXT        NOT NULL CHECK (lang IN ('ru', 'en')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Согласие на обработку хранится отдельно: это нужно для подтверждения законности обработки.
CREATE TABLE lead_consents (
    lead_id     BIGINT      PRIMARY KEY REFERENCES leads (id) ON DELETE CASCADE,
    accepted_at TIMESTAMPTZ NOT NULL,
    ip          TEXT        NOT NULL,
    user_agent  TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX leads_created_at_idx ON leads (created_at);
