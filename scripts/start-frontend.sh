#!/bin/bash
# This script starts the frontend development server.

# Navigate to the frontend directory
cd "$(dirname "$0")/../frontend" || exit

# Install dependencies (or update if already installed)
# This is generally quick if node_modules already exists and is up-to-date.
# Remove this line if you prefer to manage dependencies manually.
echo "Installing/updating frontend dependencies..."
npm install

# Start the frontend development server
echo "Starting frontend development server (typically on http://localhost:3000)..."
npm start 