CREATE TABLE wallets (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL,
    account      varchar(64) NOT NULL,
    wallet_type  varchar(32) NOT NULL,
    balance      bigint NOT NULL DEFAULT 0,
    status       varchar(16) NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE transactions (
    id              uuid PRIMARY KEY,
    wallet_id       uuid NOT NULL,
    transfer_id     uuid NOT NULL,
    tracking_number varchar(64) NOT NULL,
    amount          bigint NOT NULL,
    type            varchar(16) NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE settings (
    wallet_type varchar(32) PRIMARY KEY,
    max_balance bigint NOT NULL
);
