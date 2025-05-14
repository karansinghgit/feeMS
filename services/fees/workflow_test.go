package fees

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

type BillWorkflowTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func TestBillWorkflowTestSuite(t *testing.T) {
	suite.Run(t, new(BillWorkflowTestSuite))
}

func (s *BillWorkflowTestSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()

	// The DB instance can be nil for these tests as we are mocking outcomes.
	dbActivities := &Activities{DB: nil}
	s.env.RegisterActivity(dbActivities.UpsertBillActivity)
	s.env.RegisterActivity(dbActivities.SaveLineItemActivity)
	s.env.RegisterActivity(dbActivities.UpdateBillOnCloseActivity)
}

func (s *BillWorkflowTestSuite) AfterTest(suiteName, testName string) {
	s.env.AssertExpectations(s.T())
}

// Test_BillWorkflow_CreateAndQuery tests the creation of a bill and its queryability.
func (s *BillWorkflowTestSuite) Test_BillWorkflow_CreateAndQuery() {
	params := BillWorkflowParams{
		BillID:     uuid.NewString(),
		CustomerID: "cust-123",
		Currency:   "USD",
	}

	s.env.RegisterWorkflow(BillWorkflow)

	// Mock activities
	s.env.OnActivity("UpsertBillActivity", mock.Anything, mock.Anything).Return(nil).Once()
	s.env.OnActivity("UpdateBillOnCloseActivity", mock.Anything, mock.Anything).Return(nil).Once()

	s.env.RegisterDelayedCallback(func() {
		qr, err := s.env.QueryWorkflow(GetBillDetailsQueryName)
		require.NoError(s.T(), err)

		var open Bill
		require.NoError(s.T(), qr.Get(&open))
		require.Equal(s.T(), params.BillID, open.ID)
		require.Equal(s.T(), params.CustomerID, open.CustomerID)
		require.Equal(s.T(), params.Currency, open.Currency)
		require.Equal(s.T(), BillStatusOpen, open.Status)
		require.Empty(s.T(), open.LineItems)
		require.WithinDuration(s.T(), s.env.Now(), *open.CreatedAt, 10*time.Millisecond) // Compare with mock env time
		require.Nil(s.T(), open.ClosedAt)
		require.True(s.T(), open.TotalAmount == 0)

		s.env.SignalWorkflow(CloseBillSignalName, CloseBillSignal{})
	}, 2*time.Millisecond)

	s.env.ExecuteWorkflow(BillWorkflow, &params)

	require.True(s.T(), s.env.IsWorkflowCompleted())
	require.NoError(s.T(), s.env.GetWorkflowError())

	var closed Bill
	require.NoError(s.T(), s.env.GetWorkflowResult(&closed))
	require.Equal(s.T(), BillStatusClosed, closed.Status)
	require.NotNil(s.T(), closed.ClosedAt)
	require.True(s.T(), closed.TotalAmount == 0)
}

// Test_BillWorkflow_AddLineItemsAndClose tests the addition of line items to a bill and closing it.
func (s *BillWorkflowTestSuite) Test_BillWorkflow_AddLineItemsAndClose() {
	params := BillWorkflowParams{
		BillID:     uuid.NewString(),
		CustomerID: "cust-456",
		Currency:   "GEL",
	}
	s.env.RegisterWorkflow(BillWorkflow)

	item1ID := uuid.NewString()
	item1Amount := 100.50
	item2ID := uuid.NewString()
	item2Amount := 50.25

	// Mock activities
	s.env.OnActivity("UpsertBillActivity", mock.Anything, mock.AnythingOfType("fees.UpsertBillActivityParams")).Return(nil).Once()
	s.env.OnActivity("SaveLineItemActivity", mock.Anything, mock.MatchedBy(func(p SaveLineItemActivityParams) bool {
		return p.LineItemID == item1ID
	})).Return(nil).Once()
	s.env.OnActivity("SaveLineItemActivity", mock.Anything, mock.MatchedBy(func(p SaveLineItemActivityParams) bool {
		return p.LineItemID == item2ID
	})).Return(nil).Once()
	s.env.OnActivity("UpdateBillOnCloseActivity", mock.Anything, mock.AnythingOfType("fees.UpdateBillOnCloseActivityParams")).Return(nil).Once()

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(AddLineItemSignalName, AddLineItemSignal{
			LineItemID:  item1ID,
			Description: "Item 1",
			Amount:      item1Amount,
		})
	}, 1*time.Millisecond)

	s.env.RegisterDelayedCallback(func() {
		var billDetailsIntermediate Bill
		queryResult, err := s.env.QueryWorkflow(GetBillDetailsQueryName)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), queryResult)
		err = queryResult.Get(&billDetailsIntermediate)
		require.NoError(s.T(), err)
		require.Len(s.T(), billDetailsIntermediate.LineItems, 1)
		require.Equal(s.T(), item1ID, billDetailsIntermediate.LineItems[0].ID)
		require.True(s.T(), item1Amount == billDetailsIntermediate.LineItems[0].Amount)

		s.env.SignalWorkflow(AddLineItemSignalName, AddLineItemSignal{
			LineItemID:  item2ID,
			Description: "Item 2",
			Amount:      item2Amount,
		})
	}, 2*time.Millisecond)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(CloseBillSignalName, CloseBillSignal{})
	}, 3*time.Millisecond)

	s.env.ExecuteWorkflow(BillWorkflow, &params)

	require.True(s.T(), s.env.IsWorkflowCompleted())
	require.NoError(s.T(), s.env.GetWorkflowError())

	var finalBillDetails Bill
	err := s.env.GetWorkflowResult(&finalBillDetails)
	require.NoError(s.T(), err)

	require.Equal(s.T(), params.BillID, finalBillDetails.ID)
	require.Equal(s.T(), BillStatusClosed, finalBillDetails.Status)
	require.Len(s.T(), finalBillDetails.LineItems, 2)
	require.NotNil(s.T(), finalBillDetails.ClosedAt)

	expectedTotal := item1Amount + item2Amount
	require.True(s.T(), expectedTotal == finalBillDetails.TotalAmount, "Expected total %s, got %s", expectedTotal, finalBillDetails.TotalAmount)
}

// Test_BillWorkflow_CloseEmptyBill tests the closing of an empty bill.
func (s *BillWorkflowTestSuite) Test_BillWorkflow_CloseEmptyBill() {
	params := BillWorkflowParams{
		BillID:     uuid.NewString(),
		CustomerID: "cust-789",
		Currency:   "EUR",
	}
	s.env.RegisterWorkflow(BillWorkflow)

	// Mock activities
	s.env.OnActivity("UpsertBillActivity", mock.Anything, mock.AnythingOfType("fees.UpsertBillActivityParams")).Return(nil).Once()
	s.env.OnActivity("UpdateBillOnCloseActivity", mock.Anything, mock.AnythingOfType("fees.UpdateBillOnCloseActivityParams")).Return(nil).Once()

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(CloseBillSignalName, CloseBillSignal{})
	}, 2*time.Millisecond)

	s.env.ExecuteWorkflow(BillWorkflow, &params)

	require.True(s.T(), s.env.IsWorkflowCompleted())
	require.NoError(s.T(), s.env.GetWorkflowError())

	var finalBillDetails Bill
	err := s.env.GetWorkflowResult(&finalBillDetails)
	require.NoError(s.T(), err)

	require.Equal(s.T(), params.BillID, finalBillDetails.ID)
	require.Equal(s.T(), BillStatusClosed, finalBillDetails.Status)
	require.Empty(s.T(), finalBillDetails.LineItems)
	require.NotNil(s.T(), finalBillDetails.ClosedAt)
	require.True(s.T(), finalBillDetails.TotalAmount == 0)
}

// Test_BillWorkflow_UpsertActivityFailure tests the failure of UpsertBillActivity.
func (s *BillWorkflowTestSuite) Test_BillWorkflow_UpsertActivityFailure() {
	params := BillWorkflowParams{
		BillID:     uuid.NewString(),
		CustomerID: "cust-upsert-fail",
		Currency:   "JPY",
	}
	s.env.RegisterWorkflow(BillWorkflow)

	expectedErrText := "simulated upsert error"
	s.env.OnActivity("UpsertBillActivity", mock.Anything, mock.Anything).Return(temporal.NewNonRetryableApplicationError(expectedErrText, "UpsertError", nil)).Once()

	s.env.ExecuteWorkflow(BillWorkflow, &params)

	require.True(s.T(), s.env.IsWorkflowCompleted())
	err := s.env.GetWorkflowError()
	require.Error(s.T(), err)
	// The workflow wraps the activity error
	require.Contains(s.T(), err.Error(), "UpsertBillActivity failed")
	require.Contains(s.T(), err.Error(), expectedErrText)
}

// Test_BillWorkflow_SaveLineItemActivityFailure tests the failure of SaveLineItemActivity (workflow logs and continues)
func (s *BillWorkflowTestSuite) Test_BillWorkflow_SaveLineItemActivityFailure() {
	params := BillWorkflowParams{
		BillID:     uuid.NewString(),
		CustomerID: "cust-save-fail",
		Currency:   "CAD",
	}
	s.env.RegisterWorkflow(BillWorkflow)

	item1ID := uuid.NewString()
	item1Amount := 200.00
	expectedErrText := "simulated save line item error"

	// Mock activities
	s.env.OnActivity("UpsertBillActivity", mock.Anything, mock.AnythingOfType("fees.UpsertBillActivityParams")).Return(nil).Once()
	s.env.OnActivity("SaveLineItemActivity", mock.Anything, mock.AnythingOfType("fees.SaveLineItemActivityParams")).Return(temporal.NewNonRetryableApplicationError(expectedErrText, "SaveItemError", nil)).Once()
	// UpdateBillOnCloseActivity should still be called as workflow continues
	s.env.OnActivity("UpdateBillOnCloseActivity", mock.Anything, mock.AnythingOfType("fees.UpdateBillOnCloseActivityParams")).Return(nil).Once()

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(AddLineItemSignalName, AddLineItemSignal{
			LineItemID:  item1ID,
			Description: "Item that fails to save",
			Amount:      item1Amount,
		})
	}, 1*time.Millisecond)

	s.env.RegisterDelayedCallback(func() {
		// Query to check if line item is in workflow state (it should be)
		var billDetailsIntermediate Bill
		queryResult, err := s.env.QueryWorkflow(GetBillDetailsQueryName)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), queryResult)
		err = queryResult.Get(&billDetailsIntermediate)
		require.NoError(s.T(), err)
		require.Len(s.T(), billDetailsIntermediate.LineItems, 1, "Line item should be in workflow state despite save failure")

		s.env.SignalWorkflow(CloseBillSignalName, CloseBillSignal{})
	}, 2*time.Millisecond)

	s.env.ExecuteWorkflow(BillWorkflow, &params)

	require.True(s.T(), s.env.IsWorkflowCompleted())
	require.NoError(s.T(), s.env.GetWorkflowError(), "Workflow should complete even if SaveLineItemActivity fails (as per current design)")

	var finalBillDetails Bill
	err := s.env.GetWorkflowResult(&finalBillDetails)
	require.NoError(s.T(), err)
	require.Equal(s.T(), BillStatusClosed, finalBillDetails.Status)
	require.Len(s.T(), finalBillDetails.LineItems, 1)
	require.True(s.T(), item1Amount == finalBillDetails.TotalAmount, "Total should reflect the item in workflow state")
	// Note: This test highlights that the DB might be inconsistent with workflow state if SaveLineItemActivity fails.
}

// Test_BillWorkflow_UpdateBillOnCloseActivityFailure tests the failure of UpdateBillOnCloseActivity (workflow logs and continues)
func (s *BillWorkflowTestSuite) Test_BillWorkflow_UpdateBillOnCloseActivityFailure() {
	params := BillWorkflowParams{
		BillID:     uuid.NewString(),
		CustomerID: "cust-update-fail",
		Currency:   "AUD",
	}
	s.env.RegisterWorkflow(BillWorkflow)
	expectedErrText := "simulated update bill on close error"

	// Mock activities
	s.env.OnActivity("UpsertBillActivity", mock.Anything, mock.AnythingOfType("fees.UpsertBillActivityParams")).Return(nil).Once()
	s.env.OnActivity("UpdateBillOnCloseActivity", mock.Anything, mock.AnythingOfType("fees.UpdateBillOnCloseActivityParams")).Return(temporal.NewNonRetryableApplicationError(expectedErrText, "UpdateCloseError", nil)).Once()

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(CloseBillSignalName, CloseBillSignal{})
	}, 1*time.Millisecond)

	s.env.ExecuteWorkflow(BillWorkflow, &params)

	require.True(s.T(), s.env.IsWorkflowCompleted())
	// Workflow is designed to log and continue, so no workflow error is expected here.
	require.NoError(s.T(), s.env.GetWorkflowError(), "Workflow should complete even if UpdateBillOnCloseActivity fails (as per current design)")

	var finalBillDetails Bill
	err := s.env.GetWorkflowResult(&finalBillDetails)
	require.NoError(s.T(), err)
	require.Equal(s.T(), BillStatusClosed, finalBillDetails.Status)
	// Note: This test highlights that the DB might not reflect the closed status if UpdateBillOnCloseActivity fails.
}
