CREATE TABLE bills (
    id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('OPEN', 'CLOSED')),
    currency TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    closed_at TIMESTAMPTZ,
    total_amount TEXT NOT NULL
);

CREATE INDEX idx_bills_status ON bills (status);
CREATE INDEX idx_bills_customer_id ON bills (customer_id);