# feeMS - Fees Management Service

This project implements a backend service in Golang to manage bill creation, line item accrual, and bill closure.

It uses Encore for the API layer and infrastructure provisioning, and Temporal for durable workflow execution and state management of individual bills.

## High-Level Design Overview

The `fees` service manages the lifecycle of bills. Each bill is represented by a durable Temporal Workflow instance.

- **API Layer (Encore)**: Exposes RESTful endpoints for creating bills, adding line items, closing bills, and retrieving bill information. Defined in `services/fees/service.go`.
- **Workflow Layer (Temporal)**: Manages the state and business logic for individual bills. This includes handling signals for adding line items or closing the bill, and processing queries for the bill's current state. Defined in `services/fees/workflow.go`.
- **Activities (Temporal)**: Perform side effects from workflows, such as creating/updating bill records and line items in the Postgres database. Defined in `services/fees/activities.go`.
- **Database (Postgres via Encore sqldb)**: Stores bill summaries and line items. The schema is defined in `services/fees/migrations/`.
- **Types (Go structs)**: Shared data structures for API requests/responses, workflow parameters, and internal state are located in `services/fees/types.go`.

### Data Flow Example: Creating a Bill

1.  Client sends a `POST /bills` request to the Encore API.
2.  The `CreateBill` handler in `service.go` generates a unique `BillID`.
3.  It then initiates a new `BillWorkflow` instance via the Temporal client, passing the `BillID` and other parameters.
4.  The `BillWorkflow` starts, initializes its state (e.g., status to `OPEN`), and executes the `UpsertBillActivity`.
5.  The `UpsertBillActivity` writes the initial bill metadata to the `bills` table in the Postgres database.
6.  The API handler returns the `BillID` and other relevant information to the client.

### Data Flow Example: Adding a Line Item

1.  Client sends a `POST /bills/{billId}/items` request (Note: path might be `/bills/{billId}/line-items` depending on actual service definition, confirm from `service.go`).
2.  The `AddLineItem` handler in `service.go` sends an `AddLineItemSignal` to the specific `BillWorkflow` instance identified by `billId`.
3.  The workflow receives the signal, updates its internal state (adds the line item, recalculates the total).
4.  It then executes the `SaveLineItemActivity` to persist the line item and `UpsertBillActivity` (or a dedicated update activity) to update the bill's `total_amount` and `updated_at` timestamp in the Postgres `bills` table.

## Tech Stack

*   Go (Golang)
*   Encore (for API and cloud infrastructure)
*   Temporal (for durable workflows)
*   Local Temporal Development Server (e.g., `temporal server start-dev`)
*   PostgreSQL (via Encore's `sqldb` for data persistence)

## Prerequisites

Before you begin, ensure you have the following installed:

- [Go](https://golang.org/dl/) (version 1.21 or newer recommended)
- [Encore CLI](https://encore.dev/docs/install)
- [Temporal CLI](https://docs.temporal.io/cli#installation) (for running the local Temporal development server and interacting with it)

## Project Structure

```
.
├── .gitignore
├── encore.app        # Encore application definition
├── go.mod
├── go.sum
├── README.md
├── scripts/          # Helper scripts
│   ├── start-encore.sh
│   ├── start-frontend.sh
│   ├── start-temporal.sh
│   └── run-tests.sh
└── services/
    └── fees/           # Encore service for the fees API
        ├── service.go    # Service definition, API endpoints
        ├── workflow.go   # Temporal workflow definition, signal/query handlers
        ├── activities.go # Temporal activities
        ├── types.go      # Go structs for API, workflow, and internal state
        ├── migrations/   # SQL database migrations
        │   ├── 001_create_bills_table.up.sql
        │   ├── 001_create_bills_table.down.sql
        │   ├── 002_create_line_items_table.up.sql
        │   └── 002_create_line_items_table.down.sql
        ├── service_test.go # Integration tests for the service
        └── workflow_test.go # Temporal workflow replay tests
```

## Setup

1.  **Clone the repository**:
    ```bash
    git clone <your-repo-url>
    cd feeMS
    ```
2.  **Ensure scripts are executable**:
    ```bash
    chmod +x scripts/*.sh
    ```

## Running the Application

To run the application, you'll need two separate terminals opened at the project root (`/Users/karansingh/projects/temporal/feeMS`):

**Terminal 1: Start Temporal Development Server**

```bash
./scripts/start-temporal.sh
```
This starts a local Temporal server. Keep this terminal window open.

**Terminal 2: Start Encore Application**

```bash
./scripts/start-encore.sh
```
This compiles and runs your Encore application. You can access the Encore local development dashboard (usually at `http://localhost:4000`) to view services and make API calls via the API explorer.

### Terminal 3 (Optional): Start Frontend Development Server

The project includes a React frontend application built with TypeScript. To start it, open a third terminal at the project root and run:

```bash
./scripts/start-frontend.sh
```
This script will navigate to the `frontend` directory, install/update dependencies, and start the development server (typically on `http://localhost:3000`). The frontend is configured to connect to the backend API at `http://localhost:4000`.

## API Documentation

The service exposes RESTful API endpoints. Refer to `services/fees/types.go` and `services/fees/service.go` for detailed request/response structures and paths.

### Bill Management

*   **`POST /bills`**: Create a new bill.
    *   Request Body: `fees.CreateBillRequest`
    *   Response Body: `fees.CreateBillResponse`
*   **`POST /bills/:billID/items`**: Add a line item to an existing bill.
    *   Path Parameter: `billID` (string) - The ID of the bill.
    *   Request Body: `fees.AddLineItemRequest`
    *   Response Body: `fees.AddLineItemResponse`
*   **`POST /bills/:billID/close`**: Close an existing bill.
    *   Path Parameter: `billID` (string) - The ID of the bill.
    *   Response Body: `fees.CloseBillResponse` (contains the full bill details)
*   **`GET /bills/:billID`**: Retrieve details for a specific bill.
    *   Path Parameter: `billID` (string) - The ID of the bill.
    *   Response Body: `fees.GetBillResponse` (contains the full bill details)
*   **`GET /bills`**: List all bills, optionally filtering by status.
    *   Query Parameter: `status` (string, optional) - Filter by status (e.g., `OPEN`, `CLOSED`).
    *   Response Body: `fees.ListBillsResponse`

## Testing

To run the tests for the `fees` service, navigate to the project root and use the script:

```bash
./scripts/run-tests.sh
```
This script executes `encore test ./services/fees -v` which runs both service integration tests (`service_test.go`) and workflow replay tests (`workflow_test.go`).
