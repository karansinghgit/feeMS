package fees

import (
	"context"
	"fmt"

	"encore.dev/storage/sqldb"
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
