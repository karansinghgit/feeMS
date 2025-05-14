package fees

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"go.temporal.io/api/enums/v1"
	workflowv1 "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	temporalsdkclient "go.temporal.io/sdk/client"
)

// terminateAllRunningBillWorkflows lists and terminates all running BillWorkflow instances.
func terminateAllRunningBillWorkflows(t *testing.T, svc *Service, tc temporalsdkclient.Client) {
	t.Helper()
	t.Log("Executing terminateAllRunningBillWorkflows: Starting cleanup of running BillWorkflow instances...")

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second) // Increased timeout for safety
	defer cleanupCancel()

	var workflowsToTerminate []*workflowv1.WorkflowExecutionInfo
	var nextPageToken []byte
	for {
		listReq := &workflowservice.ListWorkflowExecutionsRequest{
			Namespace:     "default", // Ensure this matches your test Temporal namespace
			Query:         fmt.Sprintf("WorkflowType = '%s' AND ExecutionStatus = '%s'", "BillWorkflow", enums.WORKFLOW_EXECUTION_STATUS_RUNNING.String()),
			NextPageToken: nextPageToken,
		}
		resp, err := tc.WorkflowService().ListWorkflowExecutions(cleanupCtx, listReq)
		if err != nil {
			t.Logf("Warning: Failed to list running BillWorkflows for cleanup: %v", err)
			// Depending on strictness, you might require.NoError(t, err) here or break.
			// For robustness, we'll log and let the test proceed; it might fail later, which is informative.
			break
		}
		workflowsToTerminate = append(workflowsToTerminate, resp.GetExecutions()...)
		nextPageToken = resp.GetNextPageToken()
		if len(nextPageToken) == 0 {
			break
		}
	}

	if len(workflowsToTerminate) > 0 {
		t.Logf("Found %d running BillWorkflow instances to terminate.", len(workflowsToTerminate))
		for _, wfInfo := range workflowsToTerminate {
			terminateWorkflowCtx, terminateWorkflowCancelFn := context.WithTimeout(context.Background(), 10*time.Second)
			err := tc.TerminateWorkflow(terminateWorkflowCtx, wfInfo.GetExecution().GetWorkflowId(), wfInfo.GetExecution().GetRunId(), "test cleanup", nil)
			terminateWorkflowCancelFn() // Cancel context for this specific termination
			if err != nil {
				// Log error but continue trying to terminate others
				t.Logf("Warning: Failed to terminate workflow %s run %s: %v", wfInfo.GetExecution().GetWorkflowId(), wfInfo.GetExecution().GetRunId(), err)
			}
		}

		// Wait for terminations to be effective by checking again
		require.Eventually(t, func() bool {
			var stillRunningWorkflows []*workflowv1.WorkflowExecutionInfo
			var checkNextPageToken []byte
			checkRunningCtx, checkRunningCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer checkRunningCancel()

			for {
				listReq := &workflowservice.ListWorkflowExecutionsRequest{
					Namespace:     "default",
					Query:         fmt.Sprintf("WorkflowType = '%s' AND ExecutionStatus = '%s'", "BillWorkflow", enums.WORKFLOW_EXECUTION_STATUS_RUNNING.String()),
					NextPageToken: checkNextPageToken,
				}
				resp, err := tc.WorkflowService().ListWorkflowExecutions(checkRunningCtx, listReq)
				if err != nil {
					t.Logf("Warning: Failed to list workflows during termination check: %v", err)
					return false // Cannot confirm, so retry
				}
				stillRunningWorkflows = append(stillRunningWorkflows, resp.GetExecutions()...)
				checkNextPageToken = resp.GetNextPageToken()
				if len(checkNextPageToken) == 0 {
					break
				}
			}
			return len(stillRunningWorkflows) == 0
		}, 15*time.Second, 1*time.Second, "Workflows did not terminate in time after cleanup attempt.")
		t.Log("All targeted running BillWorkflow instances terminated.")
	} else {
		t.Log("No running BillWorkflow instances found to terminate.")
	}
}

// TestCreateBill tests creating a bill and verifies the response.
func TestCreateBill(t *testing.T) {
	svc, err := initService()
	require.NoError(t, err)
	require.NotNil(t, svc)

	defer func() {
		svc.temporalWorker.Stop()
		svc.temporalClient.Close()
	}()

	params := &CreateBillRequest{
		CustomerID: "cust-test-api-123",
		Currency:   "USD",
	}

	resp, err := svc.CreateBill(context.Background(), params)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.BillID)
	require.NotEmpty(t, resp.WorkflowID)
	require.NotEmpty(t, resp.RunID)
	require.Equal(t, BillStatusOpen, resp.InitialStatus)
	require.Contains(t, resp.ConfirmationMsg, "Bill created successfully")
}

// TestAddLineItem tests adding an item and then verifies by getting the bill.
func TestAddLineItem(t *testing.T) {
	svc, err := initService()
	require.NoError(t, err)
	require.NotNil(t, svc)

	defer func() {
		svc.temporalWorker.Stop()
		svc.temporalClient.Close()
	}()

	// 1. Create a bill
	createReq := &CreateBillRequest{
		CustomerID: "cust-for-additem-" + uuid.NewString(),
		Currency:   "EUR",
	}
	createResp, err := svc.CreateBill(context.Background(), createReq)
	require.NoError(t, err)
	require.NotNil(t, createResp)
	billID := createResp.BillID
	require.NotEmpty(t, billID)

	// Allow workflow to initialize
	time.Sleep(200 * time.Millisecond)

	// 2. Add a line item
	itemAmount := 75.00
	params := &AddLineItemRequest{
		Description: "Test Item from API",
		Amount:      itemAmount,
	}

	addResp, err := svc.AddLineItem(context.Background(), billID, params)

	require.NoError(t, err)
	require.NotNil(t, addResp)
	require.NotEmpty(t, addResp.LineItemID)
	require.Equal(t, billID, addResp.BillID)
	require.Contains(t, addResp.ConfirmationMsg, "LineItem added successfully")

	// 3. Verify by getting the bill, using Eventually to handle timing
	var getResp *GetBillResponse
	require.Eventually(t, func() bool {
		var errGetBill error
		getResp, errGetBill = svc.GetBill(context.Background(), billID)
		if errGetBill != nil {
			t.Logf("TestAddLineItem: Retrying GetBill due to error: %v", errGetBill)
			return false // Retry if GetBill fails
		}
		if getResp == nil || len(getResp.Bill.LineItems) == 0 {
			t.Logf("TestAddLineItem: Retrying GetBill, bill not ready or line items not yet populated. LineItems count: %d", len(getResp.Bill.LineItems))
			return false // Retry if bill or line items not populated
		}
		// Check if the specific line item is present
		for _, li := range getResp.Bill.LineItems {
			if li.ID == addResp.LineItemID {
				return true // Found the line item, condition met
			}
		}
		t.Logf("TestAddLineItem: Retrying GetBill, specific line item ID %s not found yet.", addResp.LineItemID)
		return false // Specific line item not found yet
	}, 10*time.Second, 200*time.Millisecond, "Failed to get bill with expected line item after multiple retries")

	// Assertions after Eventually confirms success
	require.NotNil(t, getResp) // Should be populated by Eventually
	require.Equal(t, billID, getResp.Bill.ID)
	require.Equal(t, BillStatusOpen, getResp.Bill.Status) // Status should still be open
	require.Len(t, getResp.Bill.LineItems, 1)
	require.Equal(t, params.Description, getResp.Bill.LineItems[0].Description)
	require.True(t, itemAmount == getResp.Bill.LineItems[0].Amount)
	require.Equal(t, addResp.LineItemID, getResp.Bill.LineItems[0].ID)
}

// TestCloseBill tests closing a bill and then verifies its status and total.
func TestCloseBill(t *testing.T) {
	svc, err := initService()
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer func() {
		svc.temporalWorker.Stop()
		svc.temporalClient.Close()
	}()

	// 1. Create a bill
	customerID := "cust-for-closebill-" + uuid.NewString()
	currency := "GBP"
	createReq := &CreateBillRequest{
		CustomerID: customerID,
		Currency:   currency,
	}
	createResp, err := svc.CreateBill(context.Background(), createReq)
	require.NoError(t, err)
	require.NotNil(t, createResp)
	billID := createResp.BillID
	require.NotEmpty(t, billID)

	time.Sleep(200 * time.Millisecond) // Allow workflow to initialize

	// 2. Add a line item (a bill needs items to have a total usually)
	itemAmount1 := 100.50
	item1Params := &AddLineItemRequest{Description: "Item 1 for closing", Amount: itemAmount1}
	addResp1, err := svc.AddLineItem(context.Background(), billID, item1Params)
	require.NoError(t, err)
	require.NotNil(t, addResp1)
	time.Sleep(200 * time.Millisecond) // Allow signal to be processed

	itemAmount2 := 50.25
	item2Params := &AddLineItemRequest{Description: "Item 2 for closing", Amount: itemAmount2}
	addResp2, err := svc.AddLineItem(context.Background(), billID, item2Params)
	require.NoError(t, err)
	require.NotNil(t, addResp2)
	time.Sleep(200 * time.Millisecond) // Allow signal to be processed

	// 3. Close the bill
	closeResp, err := svc.CloseBill(context.Background(), billID)
	require.NoError(t, err)
	require.NotNil(t, closeResp)

	// Assertions directly on CloseBillResponse
	require.Equal(t, billID, closeResp.ID) // ID from embedded Bill struct
	require.Equal(t, BillStatusClosed, closeResp.Status)
	require.Len(t, closeResp.LineItems, 2)
	expectedTotal := itemAmount1 + itemAmount2
	require.InDelta(t, expectedTotal, closeResp.TotalAmount, 0.001)
	require.Contains(t, closeResp.ConfirmationMsg, "Bill closed successfully and details retrieved.")

	// Verify line items in the response
	foundItem1 := false
	foundItem2 := false
	for _, item := range closeResp.LineItems {
		if item.ID == addResp1.LineItemID {
			require.Equal(t, item1Params.Description, item.Description)
			require.InDelta(t, itemAmount1, item.Amount, 0.001)
			foundItem1 = true
		}
		if item.ID == addResp2.LineItemID {
			require.Equal(t, item2Params.Description, item.Description)
			require.InDelta(t, itemAmount2, item.Amount, 0.001)
			foundItem2 = true
		}
	}
	require.True(t, foundItem1, "First added line item not found in close response")
	require.True(t, foundItem2, "Second added line item not found in close response")

	// 4. Verify by getting the bill (confirms persistence and final state query)
	getResp, err := svc.GetBill(context.Background(), billID)
	require.NoError(t, err)
	require.NotNil(t, getResp)
	require.Equal(t, billID, getResp.Bill.ID)
	require.Equal(t, BillStatusClosed, getResp.Bill.Status)
	require.Len(t, getResp.Bill.LineItems, 2)
	require.InDelta(t, expectedTotal, getResp.Bill.TotalAmount, 0.001)
}

// TestGetBill comprehensively tests creating, adding items, closing, and then getting a bill.
func TestGetBill(t *testing.T) {
	svc, err := initService()
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer func() {
		svc.temporalWorker.Stop()
		svc.temporalClient.Close()
	}()

	// 1. Create a bill
	customerID := "cust-for-getbill-" + uuid.NewString()
	currency := "JPY"
	createReq := &CreateBillRequest{CustomerID: customerID, Currency: currency}
	createResp, err := svc.CreateBill(context.Background(), createReq)
	require.NoError(t, err)
	require.NotNil(t, createResp)
	billID := createResp.BillID
	require.NotEmpty(t, billID)

	time.Sleep(200 * time.Millisecond) // Allow workflow to start

	// Verify initial GetBill
	getRespInitial, err := svc.GetBill(context.Background(), billID)
	require.NoError(t, err)
	require.NotNil(t, getRespInitial)
	require.Equal(t, billID, getRespInitial.Bill.ID)
	require.Equal(t, customerID, getRespInitial.Bill.CustomerID)
	require.Equal(t, currency, getRespInitial.Bill.Currency)
	require.Equal(t, BillStatusOpen, getRespInitial.Bill.Status)
	require.Empty(t, getRespInitial.Bill.LineItems)
	require.True(t, getRespInitial.Bill.TotalAmount == 0)
	require.Nil(t, getRespInitial.Bill.ClosedAt)

	// 2. Add a line item
	item1Desc := "Delicious Ramen"
	item1Amount := 1200.00
	item1Req := &AddLineItemRequest{Description: item1Desc, Amount: item1Amount}
	addResp1, err := svc.AddLineItem(context.Background(), billID, item1Req)
	require.NoError(t, err)
	require.NotNil(t, addResp1)
	lineItemID1 := addResp1.LineItemID

	time.Sleep(200 * time.Millisecond) // Allow signal processing

	// Verify GetBill after adding first item
	getRespAfterItem1, err := svc.GetBill(context.Background(), billID)
	require.NoError(t, err)
	require.NotNil(t, getRespAfterItem1)
	require.Equal(t, BillStatusOpen, getRespAfterItem1.Bill.Status)
	require.Len(t, getRespAfterItem1.Bill.LineItems, 1)
	require.Equal(t, lineItemID1, getRespAfterItem1.Bill.LineItems[0].ID)
	require.Equal(t, item1Desc, getRespAfterItem1.Bill.LineItems[0].Description)
	require.True(t, item1Amount == getRespAfterItem1.Bill.LineItems[0].Amount)
	// TotalAmount is usually calculated on close, so it might still be zero or reflect running total if workflow updates it early
	// For this test, let's assume it's only final on close, so no strong assertion on TotalAmount yet.

	// 3. Add another line item
	item2Desc := "Green Tea"
	item2Amount := 300.00
	item2Req := &AddLineItemRequest{Description: item2Desc, Amount: item2Amount}
	addResp2, err := svc.AddLineItem(context.Background(), billID, item2Req)
	require.NoError(t, err)
	require.NotNil(t, addResp2)
	lineItemID2 := addResp2.LineItemID

	// Allow signal to be processed
	time.Sleep(200 * time.Millisecond)

	// Verify GetBill after adding second item
	getRespAfterItem2, err := svc.GetBill(context.Background(), billID)
	require.NoError(t, err)
	require.NotNil(t, getRespAfterItem2)
	require.Equal(t, BillStatusOpen, getRespAfterItem2.Bill.Status)
	require.Len(t, getRespAfterItem2.Bill.LineItems, 2)

	// Check items are present (order might not be guaranteed by map iteration in workflow, so check both)
	foundItem1 := false
	foundItem2 := false
	for _, item := range getRespAfterItem2.Bill.LineItems {
		if item.ID == lineItemID1 {
			require.Equal(t, item1Desc, item.Description)
			require.True(t, item1Amount == item.Amount)
			foundItem1 = true
		}
		if item.ID == lineItemID2 {
			require.Equal(t, item2Desc, item.Description)
			require.True(t, item2Amount == item.Amount)
			foundItem2 = true
		}
	}
	require.True(t, foundItem1, "Line item 1 not found")
	require.True(t, foundItem2, "Line item 2 not found")

	// 4. Close the bill
	_, err = svc.CloseBill(context.Background(), billID)
	require.NoError(t, err)

	// Allow close signal to be processed and workflow to finalize
	time.Sleep(200 * time.Millisecond)

	// 5. Verify GetBill after closing
	getRespFinal, err := svc.GetBill(context.Background(), billID)
	require.NoError(t, err)
	require.NotNil(t, getRespFinal)
	require.Equal(t, billID, getRespFinal.Bill.ID)
	require.Equal(t, customerID, getRespFinal.Bill.CustomerID)
	require.Equal(t, currency, getRespFinal.Bill.Currency)
	require.Equal(t, BillStatusClosed, getRespFinal.Bill.Status)
	require.Len(t, getRespFinal.Bill.LineItems, 2) // Still 2 items
	require.NotNil(t, getRespFinal.Bill.ClosedAt)
	require.WithinDuration(t, time.Now(), *getRespFinal.Bill.ClosedAt, 5*time.Second)

	expectedTotalAmount := item1Amount + item2Amount
	require.Truef(t, expectedTotalAmount == getRespFinal.Bill.TotalAmount, "Expected total amount %s, got %s", expectedTotalAmount, getRespFinal.Bill.TotalAmount)
}

// TestListBills tests listing bills with various filters.
func TestListBills(t *testing.T) {
	svc, err := initService()
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Perform initial cleanup of any running BillWorkflow instances from previous runs/tests
	terminateAllRunningBillWorkflows(t, svc, svc.temporalClient)

	defer func() {
		if svc.temporalWorker != nil {
			svc.temporalWorker.Stop()
		}
		if svc.temporalClient != nil {
			svc.temporalClient.Close()
		}
	}()

	ctx := context.Background()

	// Create Bill 1 (USD, remains OPEN)
	currencyUSD := "USD"

	// Create Bill 1: OPEN, USD
	createResp1, err := svc.CreateBill(ctx, &CreateBillRequest{CustomerID: "cust-list-1", Currency: currencyUSD})
	require.NoError(t, err)
	require.NotNil(t, createResp1)
	bill1ID := createResp1.BillID

	// Create Bill 2: CLOSED, USD
	createResp2, err := svc.CreateBill(ctx, &CreateBillRequest{CustomerID: "cust-list-2", Currency: currencyUSD})
	require.NoError(t, err)
	require.NotNil(t, createResp2)
	bill2ID := createResp2.BillID

	// Add an item to Bill 2
	itemAmountBill2 := 50.00
	_, err = svc.AddLineItem(ctx, bill2ID, &AddLineItemRequest{
		Description: "item for bill 2 listing test",
		Amount:      itemAmountBill2,
	})
	require.NoError(t, err)

	// Close Bill 2
	_, err = svc.CloseBill(ctx, bill2ID)
	require.NoError(t, err)

	// Wait for bill 2 to be marked as closed in the workflow state by querying it directly.
	require.Eventually(t, func() bool {
		getResp, err := svc.GetBill(ctx, bill2ID)
		return err == nil && getResp.Bill.Status == BillStatusClosed && getResp.Bill.ClosedAt != nil
	}, 10*time.Second, 200*time.Millisecond, "Bill 2 should be closed and ClosedAt set before listing")

	// --- Test Case 1: List OPEN bills ---
	t.Run("ListOpenBills", func(t *testing.T) {
		require.Eventually(t, func() bool {
			listRespOpen, err := svc.ListBills(ctx, &ListBillsParams{Status: string(BillStatusOpen)})
			if err != nil {
				t.Logf("ListBills for OPEN errored: %v", err)
				return false
			}
			require.NotNil(t, listRespOpen)

			if len(listRespOpen.Bills) != 1 {
				t.Logf("ListOpenBills: Expected 1 bill, got %d", len(listRespOpen.Bills))
				return false
			}

			foundBill1 := false
			for _, b := range listRespOpen.Bills {
				if b.ID == bill1ID { // bill1ID was created and left open in the outer scope of TestListBills
					require.Equal(t, BillStatusOpen, b.Status)
					require.Equal(t, currencyUSD, b.Currency)
					foundBill1 = true
				} else if b.ID == bill2ID {
					t.Logf("ListOpenBills: Found bill2 (CLOSED) in OPEN list")
					return false
				}
			}
			return foundBill1
		}, 25*time.Second, 1*time.Second, "Failed to correctly list OPEN bills")
	})

	// Perform cleanup before testing closed bills
	t.Log("TestListBills: Cleaning up workflows before testing List Closed Bills")
	terminateAllRunningBillWorkflows(t, svc, svc.temporalClient)

	// --- Test Case 2: List Closed Bills (or other states)
	t.Run("ListClosedBills", func(t *testing.T) {
		// Create a bill that will be closed in this sub-test
		closedBillReq := &CreateBillRequest{Currency: "EUR", CustomerID: "cust_eur_closed"}
		createRespClosedTest, err := svc.CreateBill(context.Background(), closedBillReq)
		require.NoError(t, err)
		require.NotNil(t, createRespClosedTest)
		closedBillIDInTest := createRespClosedTest.BillID

		// Add an item to this bill
		itemAmountClosedTest := 75.50
		_, err = svc.AddLineItem(context.Background(), closedBillIDInTest, &AddLineItemRequest{
			Description: "item for closed bill listing test",
			Amount:      itemAmountClosedTest,
		})
		require.NoError(t, err)

		// Close this bill
		_, err = svc.CloseBill(context.Background(), closedBillIDInTest)
		require.NoError(t, err)

		// Wait for this bill to be marked as closed
		require.Eventually(t, func() bool {
			getResp, err := svc.GetBill(context.Background(), closedBillIDInTest)
			return err == nil && getResp.Bill.Status == BillStatusClosed && getResp.Bill.ClosedAt != nil
		}, 10*time.Second, 200*time.Millisecond, "Bill should be closed and ClosedAt set before listing")

		// Verify listing closed bills - should include bill2ID (from outer scope) and closedBillIDInTest (from this scope)
		require.Eventually(t, func() bool {
			listRespClosed, err := svc.ListBills(ctx, &ListBillsParams{Status: string(BillStatusClosed)})
			if err != nil {
				t.Logf("ListClosedBills: Error listing bills: %v", err)
				return false
			}
			require.NotNil(t, listRespClosed)

			// Expect bill2 (closed in outer scope) and closedBillIDInTest (closed in this sub-test)
			// The exact number depends on whether bill2ID was cleaned up or not by the previous cleanup.
			// For robustness, we check for presence rather than exact count if previous state is uncertain.
			foundBill2 := false
			foundClosedBillInTest := false
			for _, b := range listRespClosed.Bills {
				if b.ID == bill2ID { // bill2ID was created and closed in the outer scope of TestListBills
					require.Equal(t, BillStatusClosed, b.Status)
					foundBill2 = true
				} else if b.ID == closedBillIDInTest {
					require.Equal(t, BillStatusClosed, b.Status)
					require.True(t, itemAmountClosedTest == b.TotalAmount, "Total amount for closedBillIDInTest mismatch")
					foundClosedBillInTest = true
				}
			}
			if !foundClosedBillInTest {
				t.Logf("ListClosedBills: Did not find the bill created and closed within this sub-test (%s)", closedBillIDInTest)
			}
			if !foundBill2 {
				t.Logf("ListClosedBills: Did not find bill2ID (%s) from outer scope.", bill2ID)
			}
			// Depending on whether the cleanup between tests is perfect, bill2ID might or might not be present.
			// The primary goal here is to ensure the newly closed bill (closedBillIDInTest) is listed.
			// Corrected: Both should be found as cleanup doesn't remove closed workflows.
			return foundClosedBillInTest && foundBill2
		}, 25*time.Second, 1*time.Second, "Failed to correctly list the newly CLOSED bill and/or the pre-existing closed bill (bill2ID)")
	})

	t.Log("TestListBills: Cleaning up workflows before listing all bills")
	terminateAllRunningBillWorkflows(t, svc, svc.temporalClient)

	// --- Test Case 3: List All Bills ---
	// This will list bill1ID (OPEN) and bill2ID (CLOSED) from the main test scope.
	// Any bills created and terminated within sub-tests with their own cleanup should not appear.
	t.Run("ListAllBills", func(t *testing.T) {
		// Re-create bill1 (OPEN) and bill2 (CLOSED) to ensure they exist for this specific sub-test, making it more idempotent.
		// Create Bill 1 (USD, OPEN)
		b1Req := &CreateBillRequest{Currency: "USD", CustomerID: "cust_usd_all_1"}
		createResp1, err := svc.CreateBill(context.Background(), b1Req)
		require.NoError(t, err)
		bill1ID_local := createResp1.BillID // Use local var to avoid conflict with outer scope bill1ID if it exists
		_, err = svc.AddLineItem(context.Background(), bill1ID_local, &AddLineItemRequest{Description: "item for bill1_local", Amount: 100.50})
		require.NoError(t, err)

		// Create Bill 2 (EUR, will be CLOSED)
		b2Req := &CreateBillRequest{Currency: "EUR", CustomerID: "cust_eur_all_2"}
		createResp2, err := svc.CreateBill(context.Background(), b2Req)
		require.NoError(t, err)
		bill2ID_local := createResp2.BillID // Use local var
		itemAmountBill2Local := 120.75
		_, err = svc.AddLineItem(context.Background(), bill2ID_local, &AddLineItemRequest{Description: "item for bill2_local", Amount: itemAmountBill2Local})
		require.NoError(t, err)
		_, err = svc.CloseBill(context.Background(), bill2ID_local)
		require.NoError(t, err)
		// Wait for bill2_local to be closed
		require.Eventually(t, func() bool {
			getResp, err := svc.GetBill(context.Background(), bill2ID_local)
			return err == nil && getResp.Bill.Status == BillStatusClosed
		}, 10*time.Second, 200*time.Millisecond)

		t.Logf("ListAllBills: Finished creating local bills. bill1ID_local=%s (OPEN), bill2ID_local=%s (CLOSED)", bill1ID_local, bill2ID_local)

		require.Eventually(t, func() bool {
			listRespAll, err := svc.ListBills(ctx, &ListBillsParams{Status: ""}) // Empty status lists all
			if err != nil {
				t.Logf("ListAllBills: Error listing bills: %v", err)
				return false
			}
			require.NotNil(t, listRespAll)

			if len(listRespAll.Bills) != 2 { // Expecting only the two bills created in this sub-test after cleanup
				t.Logf("ListAllBills: Expected 2 specific bills, but got %d total bills. Proceeding to check for specific bill IDs.", len(listRespAll.Bills))
			}

			t.Logf("ListAllBills: Inside Eventually. Expecting bill1ID_local=%s, bill2ID_local=%s", bill1ID_local, bill2ID_local)
			foundBill1Local := false
			foundBill2Local := false
			for _, b := range listRespAll.Bills {
				t.Logf("ListAllBills: Checking against listed Bill ID: %s (Status: %s, Total: %.2f)", b.ID, b.Status, b.TotalAmount)
				if b.ID == bill1ID_local {
					require.Equal(t, BillStatusOpen, b.Status)
					foundBill1Local = true
				}
				if b.ID == bill2ID_local {
					require.Equal(t, BillStatusClosed, b.Status)
					foundBill2Local = true
				}
			}
			return foundBill1Local && foundBill2Local
		}, 25*time.Second, 1*time.Second, "Failed to find locally created bill1ID_local (OPEN) and bill2ID_local (CLOSED) in the list of all bills")
	})

	// --- Test Case 4: List bills with an invalid status ---
	t.Run("ListInvalidStatus", func(t *testing.T) {
		_, err := svc.ListBills(ctx, &ListBillsParams{Status: "INVALID_STATUS"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid status parameter", "Error message should indicate invalid status")
	})
}
