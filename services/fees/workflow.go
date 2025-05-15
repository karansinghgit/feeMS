package fees

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/workflow"
)

// BillWorkflow manages the lifecycle of a single bill.
func BillWorkflow(ctx workflow.Context, params *BillWorkflowParams) (respBill *Bill, respErr error) {
	logger := workflow.GetLogger(ctx)
	var workflowErr error

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
	})

	defer func() {
		if r := recover(); r != nil {
			logger.Error("BillWorkflow panicked", "panic", r)
			respErr = fmt.Errorf("Workflow panic: %v", r)
		}
	}()

	billID := params.BillID
	if billID == "" {
		generatedID, idErr := generateID(ctx)
		if idErr != nil {
			logger.Error("Failed to generate BillID", "error", idErr)
			return nil, fmt.Errorf("failed to generate BillID: %w", idErr)
		}
		billID = generatedID
	}

	createdAt := workflow.Now(ctx)
	bill := &Bill{
		ID:         billID,
		CustomerID: params.CustomerID,
		Currency:   params.Currency,
		Status:     BillStatusOpen,
		LineItems:  make([]LineItem, 0),
		CreatedAt:  &createdAt,
	}

	logger.Info("BillWorkflow started", "BillID", bill.ID)

	upsertParams := UpsertBillActivityParams{
		BillID:     bill.ID,
		CustomerID: bill.CustomerID,
		Currency:   bill.Currency,
		Status:     bill.Status,
		CreatedAt:  *bill.CreatedAt,
	}

	// Activity: Upsert bill
	err := workflow.ExecuteActivity(ctx, UpsertBillActivityName, upsertParams).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to execute UpsertBillActivity", "BillID", bill.ID, "error", err)
		return nil, fmt.Errorf("UpsertBillActivity failed: %w", err)
	}

	// Set up query handler
	err = workflow.SetQueryHandler(ctx, GetBillDetailsQueryName, func() (*Bill, error) {
		return bill, nil
	})
	if err != nil {
		logger.Error("Failed to register query handler", "error", err)
		return nil, err
	}

	// Main workflow loop to process signals
	for bill.Status == BillStatusOpen && workflowErr == nil {
		selector := workflow.NewSelector(ctx)

		// Handle AddLineItemSignal
		selector.AddReceive(workflow.GetSignalChannel(ctx, AddLineItemSignalName), func(c workflow.ReceiveChannel, more bool) {
			var signal AddLineItemSignal
			c.Receive(ctx, &signal)
			if !more {
				logger.Info("AddLineItemSignal channel closed.")
				return
			}

			if bill.Status != BillStatusOpen {
				logger.Warn("AddLineItemSignal received for a non-open bill, ignoring.", "BillID", bill.ID, "BillStatus", bill.Status, "AttemptedLineItemID", signal.LineItemID)
				return
			}

			lineItemID := signal.LineItemID
			if lineItemID == "" {
				generatedID, idErr := generateID(ctx)
				if idErr != nil {
					logger.Error("Failed to generate LineItemID for bill", "BillID", bill.ID, "error", idErr)
					return
				}
				lineItemID = generatedID
			}

			for _, item := range bill.LineItems {
				if item.ID == lineItemID {
					logger.Info("Duplicate LineItemID received, ignoring.", "BillID", bill.ID, "LineItemID", lineItemID)
					return
				}
			}

			itemCreatedAt := workflow.Now(ctx)
			newLineItem := LineItem{
				ID:          lineItemID,
				Description: signal.Description,
				Amount:      signal.Amount,
			}

			// Add to workflow state first
			bill.LineItems = append(bill.LineItems, newLineItem)
			logger.Info("Line item added to workflow state prior to saving", "BillID", bill.ID, "LineItemID", newLineItem.ID, "Amount", newLineItem.Amount)

			// Recalculate total amount after adding the new line item to the workflow state
			currentTotal := 0.0
			for _, item := range bill.LineItems {
				currentTotal += item.Amount
			}
			bill.TotalAmount = currentTotal
			logger.Info("Updated bill.TotalAmount in workflow state", "BillID", bill.ID, "NewTotalAmount", bill.TotalAmount)

			saveLineItemParams := SaveLineItemActivityParams{
				LineItemID:  newLineItem.ID,
				BillID:      bill.ID,
				Description: newLineItem.Description,
				Amount:      newLineItem.Amount,
				CreatedAt:   itemCreatedAt,
			}

			// Activity: Save new line item
			actErr := workflow.ExecuteActivity(ctx, SaveLineItemActivityName, saveLineItemParams).Get(ctx, nil)
			if actErr != nil {
				logger.Error("Failed to execute SaveLineItemActivity", "BillID", bill.ID, "LineItemID", newLineItem.ID, "Description", newLineItem.Description, "Amount", newLineItem.Amount, "error", actErr)
			} else {
				logger.Info("Successfully saved line item via activity", "BillID", bill.ID, "LineItemID", newLineItem.ID)
			}
		})

		// Handle CloseBillSignal
		selector.AddReceive(workflow.GetSignalChannel(ctx, CloseBillSignalName), func(c workflow.ReceiveChannel, more bool) {
			c.Receive(ctx, nil)
			if !more {
				logger.Info("CloseBillSignal channel closed.")
				return
			}

			total := 0.0
			for _, item := range bill.LineItems {
				total += item.Amount
			}

			closedAtTimeSnapshot := workflow.Now(ctx)
			updateBillParams := UpdateBillOnCloseActivityParams{
				BillID:      bill.ID,
				Status:      BillStatusClosed,
				TotalAmount: total,
				ClosedAt:    closedAtTimeSnapshot,
			}

			logger.Info("Executing UpdateBillOnCloseActivity", "BillID", bill.ID)
			actErr := workflow.ExecuteActivity(ctx, UpdateBillOnCloseActivityName, updateBillParams).Get(ctx, nil)
			if actErr != nil {
				logger.Error("Failed to execute UpdateBillOnCloseActivity", "BillID", bill.ID, "error", actErr)
			}

			// Closing here, but in prod, we will have to retry before marking the bill closed
			bill.Status = BillStatusClosed
			bill.ClosedAt = &closedAtTimeSnapshot
			bill.TotalAmount = total
			logger.Info("Bill marked as closed in workflow state", "BillID", bill.ID, "TotalAmount", bill.TotalAmount, "ActivitySuccess", actErr == nil)
		})

		// Block until a signal is received or workflow is canceled
		selector.Select(ctx)

		// If a signal handler set an error (e.g. from a hypothetical critical signal activity not covered here), break the loop.
		if workflowErr != nil {
			logger.Error("Workflow loop terminating due to critical signal processing error", "BillID", bill.ID, "error", workflowErr)
			break
		}
	}

	logger.Info("BillWorkflow completed", "BillID", bill.ID, "Status", bill.Status)
	return bill, workflowErr
}

// Helper to generate UUIDs if needed within workflow/activity (though often IDs are passed in)
func generateID(ctx workflow.Context) (string, error) {
	var id string
	sideEffectErr := workflow.SideEffect(ctx, func(ctx workflow.Context) interface{} {
		return uuid.NewString()
	}).Get(&id)

	if sideEffectErr != nil {
		workflow.GetLogger(ctx).Error("Failed to generate UUID via SideEffect", "error", sideEffectErr)
		return "", fmt.Errorf("failed to generate UUID: %w", sideEffectErr)
	}

	return id, nil
}
