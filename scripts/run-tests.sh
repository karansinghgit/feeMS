#!/bin/bash
# This script runs the tests for the fees service.

echo "Running tests for the fees service..."
encore test ./services/fees -v
