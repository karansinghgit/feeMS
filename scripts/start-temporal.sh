#!/bin/bash
# This script starts the Temporal development server.

echo "Starting Temporal development server..."
echo "Make sure you have the Temporal CLI installed and configured."
echo "Logs and data will be in temporal_dev.db (by default)."

# You can customize the namespace or other parameters if needed.
temporal server start-dev --db-filename temporal_dev.db --namespace default
