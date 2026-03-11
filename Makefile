.PHONY: help build build-backend build-frontend test image-build image-push clean install uninstall

# Variables
REGISTRY ?= quay.io
REPOSITORY ?= kborup/placement-discovery-plugin
TAG ?= 0.2.0
IMAGE ?= $(REGISTRY)/$(REPOSITORY):$(TAG)

PLUGIN_NAME ?= placement-discovery-plugin
NAMESPACE ?= openshift-console

help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

build: build-backend build-frontend ## Build both backend and frontend

build-backend: ## Build Go backend binary
	@echo "Building Go backend..."
	CGO_ENABLED=0 go build -o bin/plugin cmd/plugin/main.go

build-frontend: ## Build frontend assets
	@echo "Building frontend..."
	cd web && npm install && npm run build

test: ## Run tests
	@echo "Running Go tests..."
	go test -v ./pkg/...
	@echo "Running frontend type check..."
	cd web && npm run type-check

fmt: ## Format Go code
	go fmt ./...

vet: ## Run go vet
	go vet ./...

lint: ## Run linters
	cd web && npm run lint

##@ Container Image

image-build: ## Build container image
	@echo "Building container image: $(IMAGE)"
	podman build -t $(IMAGE) .

image-push: ## Push container image to registry
	@echo "Pushing container image: $(IMAGE)"
	podman push $(IMAGE)

image-build-push: image-build image-push ## Build and push container image

##@ Deployment

install: ## Install the plugin using Helm
	@echo "Installing plugin to namespace $(NAMESPACE)..."
	helm upgrade --install $(PLUGIN_NAME) ./charts/placement-discovery-plugin \
		--namespace $(NAMESPACE) \
		--create-namespace \
		--set image.repository=$(REGISTRY)/$(REPOSITORY) \
		--set image.tag=$(TAG)
	@echo "\nPlugin installed! Enable it with:"
	@echo "oc patch consoles.operator.openshift.io cluster --patch '{\"spec\":{\"plugins\":[\"$(PLUGIN_NAME)\"]}}' --type=merge"

uninstall: ## Uninstall the plugin
	@echo "Uninstalling plugin..."
	helm uninstall $(PLUGIN_NAME) --namespace $(NAMESPACE)
	@echo "Disabling plugin in console..."
	@echo "Run: oc patch consoles.operator.openshift.io cluster --type=json -p='[{\"op\": \"remove\", \"path\": \"/spec/plugins\", \"value\": \"$(PLUGIN_NAME)\"}]'"

enable-plugin: ## Enable the plugin in OpenShift Console
	@echo "Enabling plugin in console..."
	oc patch consoles.operator.openshift.io cluster \
		--patch '{"spec":{"plugins":["$(PLUGIN_NAME)"]}}' \
		--type=merge
	@echo "Plugin enabled! Wait for console pods to restart."

disable-plugin: ## Disable the plugin in OpenShift Console
	@echo "Disabling plugin in console..."
	oc get consoles.operator.openshift.io cluster -o json | \
		jq '.spec.plugins = (.spec.plugins // [] | map(select(. != "$(PLUGIN_NAME)")))' | \
		oc apply -f -

##@ Cleanup

clean: ## Clean build artifacts
	rm -rf bin/
	rm -rf web/dist/
	rm -rf web/node_modules/

##@ Verification

verify: ## Verify the deployment
	@echo "Checking deployment status..."
	kubectl get deployment -n $(NAMESPACE) -l app.kubernetes.io/name=placement-discovery-plugin
	@echo "\nChecking service..."
	kubectl get service -n $(NAMESPACE) -l app.kubernetes.io/name=placement-discovery-plugin
	@echo "\nChecking ConsolePlugin..."
	kubectl get consoleplugin $(PLUGIN_NAME)
	@echo "\nChecking if plugin is enabled..."
	oc get consoles.operator.openshift.io cluster -o jsonpath='{.spec.plugins}' | grep -q $(PLUGIN_NAME) && echo "Plugin is enabled" || echo "Plugin is not enabled"

logs: ## Show plugin logs
	kubectl logs -n $(NAMESPACE) -l app.kubernetes.io/name=placement-discovery-plugin --tail=100 -f

##@ Local Development

dev-backend: ## Run backend locally
	go run cmd/plugin/main.go --port=9002 --static-path=./web/dist

dev-frontend: ## Run frontend in development mode
	cd web && npm run watch
