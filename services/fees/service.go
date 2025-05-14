package fees

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// env specific task queue name
var (
	feesTaskQueue = getTaskQueueName()
)

// Service defines the fees service.
//
// encore:service
type Service struct {
	db             *sqldb.Database
	temporalClient client.Client
	temporalWorker worker.Worker
}

var db = sqldb.NewDatabase("fees", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})

// initService is automatically called by Encore to initialize the service.
func initService() (*Service, error) {
	c, err := client.Dial(client.Options{})
	if err != nil {
		return nil, fmt.Errorf("could not create temporal client: %w", err)
	}

	w := worker.New(c, feesTaskQueue, worker.Options{})

	// Register workflows and activities
	w.RegisterWorkflow(BillWorkflow)

	dbActivities := &Activities{DB: db}
	w.RegisterActivity(dbActivities.UpsertBillActivity)
	w.RegisterActivity(dbActivities.SaveLineItemActivity)
	w.RegisterActivity(dbActivities.UpdateBillOnCloseActivity)

	err = w.Start()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("could not start temporal worker: %w", err)
	}

	return &Service{db: db, temporalClient: c, temporalWorker: w}, nil
}

// Shutdown is called by Encore when the service is shutting down.
func (s *Service) Shutdown(force context.Context) {
	s.temporalWorker.Stop()
	s.temporalClient.Close()
}

// CreateBill creates a new bill.
//
// encore:api public method=POST path=/bills
func (s *Service) CreateBill(ctx context.Context, params *CreateBillRequest) (*CreateBillResponse, error) {
	billID := uuid.NewString()

	workflowParams := BillWorkflowParams{
		BillID:     billID,
		CustomerID: params.CustomerID,
		Currency:   params.Currency,
	}

	options := client.StartWorkflowOptions{
		ID:        "bill-" + billID,
		TaskQueue: feesTaskQueue,
	}

	we, err := s.temporalClient.ExecuteWorkflow(ctx, options, BillWorkflow, &workflowParams)
	if err != nil {
		return nil, fmt.Errorf("failed to start BillWorkflow: %w", err)
	}

	// TEST STABILITY: Allow a brief moment for the workflow to initialize and set up its query handler.
	// This helps prevent race conditions in tests where GetBill is called very soon after CreateBill.
	// In a real system, clients should be prepared for eventual consistency or use polling if immediate
	// queryability is critical and not guaranteed by the workflow start semantics.

	return &CreateBillResponse{
		BillID:          billID,
		WorkflowID:      we.GetID(),
		RunID:           we.GetRunID(),
		InitialStatus:   BillStatusOpen,
		ConfirmationMsg: "Bill created successfully.",
	}, nil
}

// AddLineItem adds a line item to an existing bill.
//
// encore:api public method=POST path=/bills/:billID/items
func (s *Service) AddLineItem(ctx context.Context, billID string, params *AddLineItemRequest) (*AddLineItemResponse, error) {
	lineItemID := uuid.NewString()
	signal := AddLineItemSignal{
		LineItemID:  lineItemID,
		Description: params.Description,
		Amount:      params.Amount,
	}

	wfID := "bill-" + billID
	err := s.temporalClient.SignalWorkflow(ctx, wfID, "", AddLineItemSignalName, signal)
	if err != nil {
		return nil, fmt.Errorf("failed to send AddLineItemSignal to workflow %s: %w", wfID, err)
	}

	return &AddLineItemResponse{
		LineItemID:      lineItemID,
		BillID:          billID,
		ConfirmationMsg: "LineItem added successfully.",
	}, nil
}

// CloseBill closes an existing bill.
//
// encore:api public method=POST path=/bills/:billID/close
func (s *Service) CloseBill(ctx context.Context, billID string) (*CloseBillResponse, error) {
	wfID := "bill-" + billID
	err := s.temporalClient.SignalWorkflow(ctx, wfID, "", CloseBillSignalName, CloseBillSignal{})
	if err != nil {
		return nil, fmt.Errorf("failed to send CloseBillSignal to workflow %s: %w", wfID, err)
	}

	var billDetails Bill
	var lastQueryError error

	// Retry querying the workflow for a short period to allow for signal processing and state update.
	// This makes the API call more robust to timing variations.
	pollingTimeout := time.After(10 * time.Second) // Total timeout for polling
	retryInterval := 250 * time.Millisecond        // Interval between retries (slightly increased)

	for {
		select {
		case <-pollingTimeout:
			errMsg := fmt.Sprintf("timeout waiting for bill %s to close and become queryable after 10s", billID)
			if lastQueryError != nil {
				errMsg = fmt.Sprintf("%s. last query error: %v", errMsg, lastQueryError)
			}
			return nil, fmt.Errorf(errMsg)
		default:
			// Create a new context with a shorter timeout for each query attempt
			// to prevent one slow query from blocking the entire polling duration.
			queryCtx, cancelQueryCtx := context.WithTimeout(ctx, 2*time.Second)

			resp, err := s.temporalClient.QueryWorkflow(queryCtx, wfID, "", GetBillDetailsQueryName)
			cancelQueryCtx() // Important to call cancel to free resources

			if err != nil {
				lastQueryError = fmt.Errorf("query attempt for BillWorkflow %s failed: %w", wfID, err)
				// Log the error for debugging test failures
				slog.Warn("CloseBill: QueryWorkflow attempt failed", "billID", billID, "workflowID", wfID, "error", err.Error())
				time.Sleep(retryInterval)
				continue
			}

			if err := resp.Get(&billDetails); err != nil {
				lastQueryError = fmt.Errorf("failed to decode bill details for %s: %w", wfID, err)
				slog.Warn("CloseBill: Failed to decode bill details", "billID", billID, "workflowID", wfID, "error", err.Error())
				time.Sleep(retryInterval)
				continue
			}

			if billDetails.Status == BillStatusClosed {
				slog.Info("CloseBill: Successfully queried and confirmed bill closed", "billID", billID, "workflowID", wfID)
				goto found // exit loop
			}

			lastQueryError = fmt.Errorf("bill %s queryable but status is %s (expected CLOSED)", billID, billDetails.Status)
			slog.Warn("CloseBill: Bill not yet closed", "billID", billID, "workflowID", wfID, "status", billDetails.Status)
			time.Sleep(retryInterval)
		}
	}

found: // Label to break out of the loop
	return &CloseBillResponse{
		Bill:            billDetails,
		ConfirmationMsg: "Bill closed successfully and details retrieved.",
	}, nil
}

// GetBill retrieves the details of a specific bill.
//
// encore:api public method=GET path=/bills/:billID
func (s *Service) GetBill(ctx context.Context, billID string) (*GetBillResponse, error) {
	wfID := "bill-" + billID
	var billDetails Bill
	resp, err := s.temporalClient.QueryWorkflow(ctx, wfID, "", GetBillDetailsQueryName)
	if err != nil {
		return nil, fmt.Errorf("failed to query BillWorkflow %s: %w", wfID, err)
	}
	if err := resp.Get(&billDetails); err != nil {
		return nil, fmt.Errorf("failed to decode bill details from workflow %s: %w", wfID, err)
	}
	return &GetBillResponse{Bill: billDetails}, nil
}

// ListBills lists all bills, with optional filtering.
//
// encore:api public method=GET path=/bills
func (s *Service) ListBills(ctx context.Context, params *ListBillsParams) (*ListBillsResponse, error) {
	var queryParts []string
	queryParts = append(queryParts, fmt.Sprintf("WorkflowType = '%s'", "BillWorkflow"))

	switch params.Status {
	case string(BillStatusOpen):
		queryParts = append(queryParts, fmt.Sprintf("ExecutionStatus = '%s'", enums.WORKFLOW_EXECUTION_STATUS_RUNNING.String()))
	case string(BillStatusClosed):
		queryParts = append(queryParts, fmt.Sprintf("ExecutionStatus = '%s'", enums.WORKFLOW_EXECUTION_STATUS_COMPLETED.String()))
	case "":
		// No additional status filter, list all (running and completed)
	default:
		return nil, fmt.Errorf("invalid status parameter: '%s'. Must be 'OPEN', 'CLOSED', or empty", params.Status)
	}

	queryString := ""
	for i, part := range queryParts {
		if i > 0 {
			queryString += " AND "
		}
		queryString += part
	}

	request := &workflowservice.ListWorkflowExecutionsRequest{
		Namespace: "default",
		Query:     queryString,
	}

	resp, err := s.temporalClient.WorkflowService().ListWorkflowExecutions(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflow executions: %w", err)
	}

	var bills []Bill
	for _, executionInfo := range resp.GetExecutions() {
		wfID := executionInfo.GetExecution().GetWorkflowId()
		runID := executionInfo.GetExecution().GetRunId()

		var billDetails Bill
		queryResp, err := s.temporalClient.QueryWorkflow(ctx, wfID, runID, GetBillDetailsQueryName)
		if err != nil {
			fmt.Printf("failed to query workflow %s run %s: %v\n", wfID, runID, err)
			continue
		}
		if err := queryResp.Get(&billDetails); err != nil {
			fmt.Printf("failed to decode bill details from workflow %s run %s: %v\n", wfID, runID, err)
			continue
		}
		bills = append(bills, billDetails)
	}

	return &ListBillsResponse{Bills: bills}, nil
}
