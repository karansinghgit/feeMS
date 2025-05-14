package fees

import (
	"time"
)

// BillStatus represents the status of a bill.
type BillStatus string

const (
	BillStatusOpen   BillStatus = "OPEN"
	BillStatusClosed BillStatus = "CLOSED"
)

// Bill represents a customer bill.
type Bill struct {
	ID          string     `json:"id"`
	CustomerID  string     `json:"customerId,omitempty"`
	Currency    string     `json:"currency"`
	Status      BillStatus `json:"status"`
	LineItems   []LineItem `json:"lineItems"`
	TotalAmount float64    `json:"totalAmount"`
	CreatedAt   *time.Time `json:"createdAt"`
	ClosedAt    *time.Time `json:"closedAt,omitempty"`
}

// LineItem represents an individual item on a bill.
type LineItem struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
}

// ------ API Payloads ------

// CreateBillRequest is the request payload for creating a new bill.
type CreateBillRequest struct {
	CustomerID string `json:"customerId,omitempty"`
	Currency   string `json:"currency"`
}

// CreateBillResponse is the response payload after creating a new bill.
type CreateBillResponse struct {
	BillID          string     `json:"billId"`
	WorkflowID      string     `json:"workflowId"`
	RunID           string     `json:"runId"`
	InitialStatus   BillStatus `json:"initialStatus"`
	ConfirmationMsg string     `json:"confirmationMsg"`
}

// AddLineItemRequest is the request payload for adding a line item to a bill.
type AddLineItemRequest struct {
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
}

// AddLineItemResponse is the response payload after adding a line item.
type AddLineItemResponse struct {
	LineItemID      string     `json:"lineItemId"`
	BillID          string     `json:"billId"`
	BillStatus      BillStatus `json:"billStatus"`
	ConfirmationMsg string     `json:"confirmationMsg"`
}

// CloseBillResponse is the response payload after closing a bill.
type CloseBillResponse struct {
	BillID          string `json:"billId"`
	ConfirmationMsg string `json:"confirmationMsg"`
}

// GetBillResponse is the response payload for retrieving a bill.
type GetBillResponse struct {
	Bill
}

// ListBillsParams defines parameters for listing bills.
type ListBillsParams struct {
	Status   string `query:"status"`
	Currency string `query:"currency"`
	Limit    int    `query:"limit"`
	Offset   int    `query:"offset"`
}

// ListBillsResponse is the response payload for listing bills.
type ListBillsResponse struct {
	Bills      []Bill `json:"bills"`
	TotalCount int    `json:"totalCount"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
}
