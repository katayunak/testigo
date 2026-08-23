CREATE TABLE IF NOT EXISTS payments (
    id          TEXT NOT NULL,
    idem_key    TEXT NOT NULL,
    cents       BIGINT NOT NULL,
    status      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id       BIGSERIAL,
    account  TEXT NOT NULL,
    cents    BIGINT NOT NULL,
    PRIMARY KEY (id)
);
