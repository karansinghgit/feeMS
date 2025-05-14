CREATE TABLE line_items (
    id TEXT PRIMARY KEY,
    bill_id TEXT NOT NULL REFERENCES bills(id) ON DELETE CASCADE,
    description TEXT NOT NULL,
    amount NUMERIC(16, 4) NOT NULL DEFAULT 0.0,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_line_items_bill_id ON line_items(bill_id); 
