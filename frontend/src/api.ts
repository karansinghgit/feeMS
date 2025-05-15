import axios from 'axios';

const API_BASE_URL = 'http://localhost:4000'; // Assuming Encore runs on port 4000

// Define interfaces based on your Go types.go
// These might need adjustments based on the exact JSON structure.
export interface Bill {
  id: string;
  customerId?: string;
  currency: string;
  status: string;
  lineItems: LineItem[];
  totalAmount: number;
  createdAt?: string;
  closedAt?: string;
}

export interface LineItem {
  id: string;
  description: string;
  amount: number;
}

export interface CreateBillRequest {
  customerId?: string;
  currency: string;
}

export interface CreateBillResponse {
  billId: string;
  workflowId: string;
  runId: string;
  initialStatus: string;
  confirmationMsg: string;
}

export interface AddLineItemRequest {
  description: string;
  amount: number;
}

export interface AddLineItemResponse {
  lineItemId: string;
  billId: string;
  confirmationMsg: string;
}

export interface CloseBillResponse extends Bill {
  confirmationMsg?: string;
}

export interface GetBillResponse {
  bill: Bill;
}

export interface ListBillsParams {
  status?: 'OPEN' | 'CLOSED' | ''; // Adjust as per your API
  // Add other params like currency, limit, offset if needed
}

export interface ListBillsResponse {
  bills: Bill[];
  totalCount: number;
  limit: number;
  offset: number;
}

export const createBill = async (data: CreateBillRequest): Promise<CreateBillResponse> => {
  const response = await axios.post<CreateBillResponse>(`${API_BASE_URL}/bills`, data);
  return response.data;
};

export const getBill = async (billID: string): Promise<GetBillResponse> => {
  try {
    console.log(`Fetching bill ${billID} from ${API_BASE_URL}/bills/${billID}`);
    const response = await axios.get<GetBillResponse>(`${API_BASE_URL}/bills/${billID}`);
    console.log('Raw response:', response);
    console.log('Response data:', response.data);
    console.log('Response headers:', response.headers);
    
    if (
      typeof response.data === 'object' &&
      response.data !== null &&
      response.data.bill && // Check for 'bill' property
      typeof response.data.bill === 'object' &&
      'id' in response.data.bill // Check for 'id' within 'bill'
    ) {
      console.log('Valid bill data received under bill property:', response.data);
      return response.data; // Return the whole GetBillResponse object
    } else {
      console.error(
        'getBill received unexpected data structure. Expected { bill: { id: ... } }:',
        response.data
      );
      // Attempt to parse if it's a string that might be JSON (less likely now but good to keep)
      if (typeof response.data === 'string') {
        try {
          const parsedData = JSON.parse(response.data);
          console.log('Parsed string data:', parsedData);
          if (
            typeof parsedData === 'object' &&
            parsedData !== null &&
            parsedData.bill &&
            typeof parsedData.bill === 'object' &&
            'id' in parsedData.bill
          ) {
            return parsedData;
          }
        } catch (parseError) {
          console.error('getBill failed to parse string response data:', parseError);
        }
      }
      throw new Error(
        `Invalid data received for bill ${billID}. Expected a Bill object under a 'bill' property, got: ${JSON.stringify(
          response.data
        )}`
      );
    }
  } catch (error) {
    console.error(`Error fetching bill ${billID}:`, error);
    if (axios.isAxiosError(error)) {
      console.error('Axios error details:', {
        status: error.response?.status,
        statusText: error.response?.statusText,
        data: error.response?.data,
        headers: error.response?.headers,
        config: {
          url: error.config?.url,
          method: error.config?.method,
          headers: error.config?.headers,
        },
      });
    }
    throw error;
  }
};

export const listBills = async (params?: ListBillsParams): Promise<ListBillsResponse> => {
  const response = await axios.get<ListBillsResponse>(`${API_BASE_URL}/bills`, { params });
  return response.data;
};

export const addLineItem = async (billID: string, data: AddLineItemRequest): Promise<AddLineItemResponse> => {
  const response = await axios.post<AddLineItemResponse>(`${API_BASE_URL}/bills/${billID}/items`, data);
  return response.data;
};

export const closeBill = async (billID: string): Promise<CloseBillResponse> => {
  const response = await axios.post<CloseBillResponse>(`${API_BASE_URL}/bills/${billID}/close`);
  return response.data;
};