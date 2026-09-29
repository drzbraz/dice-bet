-- Seed a handful of clients with starting balances (minor units / cents)
-- so the WebSocket contract and Postman collection work out of the box.
INSERT INTO clients (id) VALUES
    ('alice'),
    ('bob'),
    ('carol'),
    ('dave');

INSERT INTO wallets (client_id, balance, currency) VALUES
    ('alice', 100000, 'EUR'), -- 1,000.00 EUR
    ('bob',    50000, 'EUR'), -- 500.00 EUR
    ('carol',  25000, 'EUR'), -- 250.00 EUR
    ('dave',       0, 'EUR'); -- 0.00 EUR, useful for exercising INSUFFICIENT_BALANCE
