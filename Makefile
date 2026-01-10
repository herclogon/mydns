.PHONY: help build run stop clean test

IMAGE_NAME := mydns
CONTAINER_NAME := mydns-server
PORT := 5353

help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

build: ## Build the Docker image
	@echo "Building Docker image..."
	docker build -t $(IMAGE_NAME):latest .
	@echo "Build complete!"

run: ## Run the DNS server container
	@echo "Starting DNS server on port $(PORT)..."
	docker run -d \
		--name $(CONTAINER_NAME) \
		-p $(PORT):53/udp \
		-p $(PORT):53/tcp \
		--cap-add=NET_BIND_SERVICE \
		$(IMAGE_NAME):latest
	@echo "DNS server running! Test with: dig @localhost -p $(PORT) example.com"

run-privileged: ## Run the DNS server on port 53 (requires privileges)
	@echo "Starting DNS server on port 53 (privileged)..."
	docker run -d \
		--name $(CONTAINER_NAME) \
		-p 53:53/udp \
		-p 53:53/tcp \
		--cap-add=NET_BIND_SERVICE \
		$(IMAGE_NAME):latest
	@echo "DNS server running on port 53! Test with: dig @localhost example.com"

logs: ## Show container logs
	docker logs -f $(CONTAINER_NAME)

stop: ## Stop the DNS server container
	@echo "Stopping DNS server..."
	docker stop $(CONTAINER_NAME) || true
	docker rm $(CONTAINER_NAME) || true
	@echo "Stopped!"

clean: stop ## Stop container and remove image
	@echo "Removing Docker image..."
	docker rmi $(IMAGE_NAME):latest || true
	@echo "Clean complete!"

test: ## Test the DNS server
	@echo "Testing DNS resolution..."
	dig @localhost -p $(PORT) example.com
	@echo ""
	dig @localhost -p $(PORT) google.com

rebuild: clean build ## Clean, rebuild and run
	@echo "Rebuild complete!"

all: build run ## Build and run the server
