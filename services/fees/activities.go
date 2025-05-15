package fees

import (
	"context"
	"fmt"

	"encore.dev/storage/sqldb"
)

// Activities holds a reference to the database for persistence operations.
type Activities struct {
	DB *sqldb.Database
}

// UpsertBillActivity creates or updates a bill in the database.
func (a *Activities) UpsertBillActivity(ctx context.Context, params UpsertBillActivityParams) error {
	_, err := a.DB.Exec(ctx, `
        INSERT INTO bills (id, customer_id, currency, status, created_at, total_amount)
        VALUES ($1, $2, $3, $4, $5, $6)
        ON CONFLICT (id) DO UPDATE SET
            customer_id = EXCLUDED.customer_id,
            currency = EXCLUDED.currency,
            status = EXCLUDED.status,
            -- created_at should not change on conflict
            total_amount = bills.total_amount -- ensure total_amount is not reset if bill already exists
    `, params.BillID, params.CustomerID, params.Currency, params.Status, params.CreatedAt, 0.0)
	if err != nil {
		return fmt.Errorf("UpsertBillActivity: failed to upsert bill %s: %w", params.BillID, err)
	}
	return nil
}

// SaveLineItemActivity saves a new line item to the database.
func (a *Activities) SaveLineItemActivity(ctx context.Context, params SaveLineItemActivityParams) error {
	_, err := a.DB.Exec(ctx, `
        INSERT INTO line_items (id, bill_id, description, amount, created_at)
        VALUES ($1, $2, $3, $4, $5)
    `, params.LineItemID, params.BillID, params.Description, params.Amount, params.CreatedAt)
	if err != nil {
		return fmt.Errorf("SaveLineItemActivity: failed to save line item %s for bill %s: %w", params.LineItemID, params.BillID, err)
	}
	return nil
}

// UpdateBillOnCloseActivity updates the bill's status, total amount, and closed_at time.
func (a *Activities) UpdateBillOnCloseActivity(ctx context.Context, params UpdateBillOnCloseActivityParams) error {
	_, err := a.DB.Exec(ctx, `
        UPDATE bills
        SET status = $2, total_amount = $3, closed_at = $4
        WHERE id = $1
    `, params.BillID, params.Status, params.TotalAmount, params.ClosedAt)
	if err != nil {
		return fmt.Errorf("UpdateBillOnCloseActivity: failed to update bill %s on close: %w", params.BillID, err)
	}
	return nil
}
