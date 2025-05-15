import React, { useState, useEffect, FormEvent } from 'react';
import * as api from '../api';
import type { Bill, CreateBillRequest, AddLineItemRequest, ListBillsParams } from '../api';

const BillManager: React.FC = () => {
  const [bills, setBills] = useState<Bill[]>([]);
  const [selectedBill, setSelectedBill] = useState<Bill | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [isBillListLoading, setIsBillListLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);

  // Form states
  const [customerID, setCustomerID] = useState<string>('');
  const [currency, setCurrency] = useState<string>('USD');

  const [newItemDescription, setNewItemDescription] = useState<string>('');
  const [newItemAmount, setNewItemAmount] = useState<number>(0);

  const [listFilterStatus, setListFilterStatus] = useState<ListBillsParams['status']>('');

  const fetchBills = async (status?: ListBillsParams['status']) => {
    console.log('Fetching bills with status:', status);
    setIsBillListLoading(true);
    setError(null);
    try {
      const params: ListBillsParams = {};
      if (status) {
        params.status = status;
      }
      const response = await api.listBills(params);
      console.log('Response from api.listBills:', response);
      setBills(response.bills || []);
      if (!response.bills) {
        console.warn('api.listBills returned a response without a .bills array:', response);
      }
    } catch (err: any) {
      console.error('Error in fetchBills:', err);
      setError(`Failed to fetch bills: ${err.message}. The list may be incomplete or outdated.`);
    }
    setIsBillListLoading(false);
  };

  useEffect(() => {
    fetchBills(listFilterStatus);
  }, [listFilterStatus]);

  const handleCreateBill = async (e: FormEvent) => {
    e.preventDefault();
    setIsLoading(true);
    setError(null);
    setSuccessMessage(null);
    try {
      const request: CreateBillRequest = { customerId: customerID, currency };
      const response = await api.createBill(request);
      setCustomerID('');
      setCurrency('USD');
      setSuccessMessage(response.confirmationMsg || 'Bill created successfully!');
      setTimeout(() => setSuccessMessage(null), 5000);
      setTimeout(() => {
        fetchBills(listFilterStatus);
      }, 1500);
    } catch (err: any) {
      setError(`Failed to create bill: ${err.message}`);
      console.error(err);
    }
    setIsLoading(false);
  };

  const handleSelectBill = async (billID: string) => {
    if (selectedBill?.id === billID) { // Optionally, do nothing if already selected, or re-fetch
      // setSelectedBill(null); // Uncomment to allow de-selecting by clicking again
      // return;
    }
    setIsLoading(true); 
    setError(null);
    setSuccessMessage(null);
    try {
      const response = await api.getBill(billID);
      console.log('Selected Bill Details from API:', JSON.stringify(response, null, 2));
      setSelectedBill(response.bill);
    } catch (err: any) {
      setError(`Failed to fetch bill details: ${err.message}`);
      console.error(err);
      setSelectedBill(null);
    }
    setIsLoading(false);
  };

  const handleAddLineItem = async (e: FormEvent) => {
    e.preventDefault();
    if (!selectedBill) return;
    setIsLoading(true);
    setError(null);
    setSuccessMessage(null);
    try {
      const request: AddLineItemRequest = { description: newItemDescription, amount: newItemAmount };
      const response = await api.addLineItem(selectedBill.id, request);
      setNewItemDescription('');
      setNewItemAmount(0);
      setSuccessMessage(response.confirmationMsg || 'Line item added successfully!');
      setTimeout(() => setSuccessMessage(null), 3000);
      handleSelectBill(selectedBill.id); // Refresh selected bill details
      setTimeout(() => fetchBills(listFilterStatus), 500);
    } catch (err: any) {
      setError(`Failed to add line item: ${err.message}`);
      console.error(err);
    }
    setIsLoading(false);
  };

  const handleCloseBill = async (billID: string) => {
    setIsLoading(true);
    setError(null);
    setSuccessMessage(null);
    try {
      const response = await api.closeBill(billID);
      setSuccessMessage(response.confirmationMsg || 'Bill closed successfully!');
      setTimeout(() => setSuccessMessage(null), 3000);
      fetchBills(listFilterStatus); 
      if (selectedBill && selectedBill.id === billID) {
        handleSelectBill(billID); 
      }
    } catch (err: any) {
      setError(`Failed to close bill: ${err.message}`);
      console.error(err);
    }
    setIsLoading(false);
  };

  return (
    <div className="bill-manager-container">
      <div className="bill-manager-main-column">
        <div className="card create-bill-card">
          <h2>Create New Bill</h2>
          <form onSubmit={handleCreateBill} className="bill-form">
            <div className="form-group">
              <label htmlFor="customerID">Customer ID (optional): </label>
              <input id="customerID" type="text" value={customerID} onChange={(e) => setCustomerID(e.target.value)} />
            </div>
            <div className="form-group">
              <label htmlFor="currency">Currency: </label>
              <input id="currency" type="text" value={currency} onChange={(e) => setCurrency(e.target.value)} required />
            </div>
            <button type="submit" className="btn btn-primary" disabled={isLoading}>
              {isLoading && !isBillListLoading ? 'Creating...' : 'Create Bill'}
            </button>
          </form>
        </div>

        <div className="card bills-list-card">
          <h2>Bills</h2>
          <div className="bill-list-controls">
            <label htmlFor="filterStatus">Filter by status: </label>
            <select id="filterStatus" value={listFilterStatus} onChange={(e) => setListFilterStatus(e.target.value as ListBillsParams['status'])}>
              <option value="">All</option>
              <option value="OPEN">Open</option>
              <option value="CLOSED">Closed</option>
            </select>
            <button onClick={() => fetchBills(listFilterStatus)} className="btn btn-secondary" disabled={isBillListLoading}>
              {isBillListLoading ? 'Refreshing...' : 'Refresh List'}
            </button>
          </div>
          
          {isBillListLoading && <div className="loading-indicator"><p>Loading bills...</p></div>}
          
          {/* General success/error messages */} 
          {successMessage && <div className="alert alert-success"><p>{successMessage}</p></div>}
          {error && <div className="alert alert-danger"><p>{error}</p></div>}

          {!isBillListLoading && bills.length === 0 && !error && <div className="empty-state"><p>No bills found. Create one to get started!</p></div>}
          
          <ul className="bill-list">
            {bills.map((bill) => (
              <li key={bill.id} 
                  className={`bill-item ${selectedBill?.id === bill.id ? 'selected' : ''}`}
                  onClick={() => handleSelectBill(bill.id)}>
                <div className="bill-item-summary">
                  <span>ID: {bill.id}</span>
                  <span>Status: <span className={`status-badge status-${bill.status ? bill.status.toLowerCase() : 'unknown'}`}>{bill.status || 'N/A'}</span></span>
                </div>
                <p>Customer: {bill.customerId || 'N/A'}</p>
                <p>Total: {bill.currency} {typeof bill.totalAmount === 'number' ? bill.totalAmount.toFixed(2) : 'N/A'}</p>
                {/* <button onClick={(e) => { e.stopPropagation(); handleSelectBill(bill.id); }} className="btn btn-sm" disabled={isLoading || selectedBill?.id === bill.id }>
                  {selectedBill?.id === bill.id ? 'Selected' : 'View Details'}
                </button> */}
                {bill.status === 'OPEN' && (
                  <button onClick={(e) => { e.stopPropagation(); handleCloseBill(bill.id); }} className="btn btn-sm btn-warning" disabled={isLoading}>
                    {isLoading ? 'Closing...' : 'Close Bill'}
                  </button>
                )}
              </li>
            ))}
          </ul>
        </div>
      </div>

      {selectedBill && (
        <div className="bill-manager-detail-column">
          <div className="card bill-details-card">
            <h3>Bill Details: {selectedBill.id || 'N/A'}</h3>
            <button className="btn-close-details" onClick={() => setSelectedBill(null)} title="Close details">X</button>
            
            <dl className="definition-list">
              <dt>Status:</dt>
              <dd><span className={`status-badge status-${selectedBill.status ? selectedBill.status.toLowerCase() : 'unknown'}`}>{selectedBill.status || 'N/A'}</span></dd>

              <dt>Customer ID:</dt>
              <dd>{selectedBill.customerId || 'N/A'}</dd>

              <dt>Currency:</dt>
              <dd>{selectedBill.currency || 'N/A'}</dd>

              <dt>Total Amount:</dt>
              <dd>{selectedBill.currency || ''} {typeof selectedBill.totalAmount === 'number' ? selectedBill.totalAmount.toFixed(2) : (selectedBill.totalAmount === null || selectedBill.totalAmount === undefined ? 'N/A' : selectedBill.totalAmount)}</dd>

              <dt>Created At:</dt>
              <dd>{selectedBill.createdAt ? new Date(selectedBill.createdAt).toLocaleString() : 'N/A'}</dd>

              {selectedBill.closedAt && (
                <>
                  <dt>Closed At:</dt>
                  <dd>{new Date(selectedBill.closedAt).toLocaleString()}</dd>
                </>
              )}
            </dl>

            <h4>Line Items:</h4>
            {selectedBill.lineItems && selectedBill.lineItems.length > 0 ? (
              <ul className="line-item-list">
                {selectedBill.lineItems.map((item) => (
                  <li key={item.id} className="line-item">
                    <span>{item.description}</span>
                    <span>{selectedBill.currency} {typeof item.amount === 'number' ? item.amount.toFixed(2) : 'N/A'}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <div className="empty-state line-items-empty"><p>No line items yet for this bill.</p></div>
            )}

            {selectedBill.status === 'OPEN' && (
              <div className="add-line-item-form-container">
                <h5>Add New Line Item</h5>
                <form onSubmit={handleAddLineItem} className="bill-form">
                  <div className="form-group">
                    <label htmlFor="lineItemDesc">Description: </label>
                    <input id="lineItemDesc" type="text" value={newItemDescription} onChange={(e) => setNewItemDescription(e.target.value)} required />
                  </div>
                  <div className="form-group">
                    <label htmlFor="lineItemAmount">Amount: </label>
                    <input id="lineItemAmount" type="number" step="0.01" value={newItemAmount} onChange={(e) => setNewItemAmount(parseFloat(e.target.value))} required />
                  </div>
                  <button type="submit" className="btn btn-primary" disabled={isLoading}>
                    {isLoading ? 'Adding...' : 'Add Item'}
                  </button>
                </form>
              </div>
            )}
          </div>
        </div>
      )}
      {!selectedBill && (
         <div className="bill-manager-detail-column placeholder">
            <div className="card bill-details-card empty-details-placeholder">
                <p>Select a bill from the list to view its details.</p>
            </div>
        </div>
      )}
    </div>
  );
};

export default BillManager;
