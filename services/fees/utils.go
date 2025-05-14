package fees

import "encore.dev"

func getTaskQueueName() string {
	envName := encore.Meta().Environment.Name
	return envName + "_FEES_TASK_QUEUE"
}
