SHELL := /bin/bash
.SHELLFLAGS := -eo pipefail -c

GO ?= go
NPM ?= npm
ENV_FILE ?= .env

ONNX_VERSION ?= 1.26.0
ONNX_ARCHIVE := onnxruntime-linux-x64-$(ONNX_VERSION).tgz
ONNX_URL := https://github.com/microsoft/onnxruntime/releases/download/v$(ONNX_VERSION)/$(ONNX_ARCHIVE)
ONNX_DIR := third_party/onnxruntime-linux-x64-$(ONNX_VERSION)
ONNX_LIB := $(ONNX_DIR)/lib/libonnxruntime.so.$(ONNX_VERSION)

SILERO_DIR := third_party/silero-vad
SILERO_SOURCE_MODEL ?= ../silero-vad/src/silero_vad/data/silero_vad.onnx
SILERO_MODEL ?= $(SILERO_DIR)/silero_vad.onnx
SILERO_WAV ?= ../silero-vad/tests/data/test.wav

.PHONY: help env check-vad-config api pbx dev worker seed probe probe-install test test-pbx test-silero fmt vet build install-third-party install-onnx install-silero check-silero docker-dev docker-demo

help: ## Show available make targets.
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

env: ## Create .env from .env.example when missing.
	@if [[ ! -f "$(ENV_FILE)" ]]; then \
		cp .env.example "$(ENV_FILE)"; \
		echo "Created $(ENV_FILE) from .env.example"; \
	else \
		echo "Using $(ENV_FILE)"; \
	fi

check-vad-config: env
	@provider=$$(grep -E '^SIMULSPEAK_VAD_PROVIDER=' "$(ENV_FILE)" | tail -1 | cut -d= -f2-); \
	if [[ "$$provider" != "silero" ]]; then \
		exit 0; \
	fi; \
	model=$$(grep -E '^SIMULSPEAK_VAD_MODEL_PATH=' "$(ENV_FILE)" | tail -1 | cut -d= -f2-); \
	runtime=$$(grep -E '^(SIMULSPEAK_VAD_RUNTIME_LIBRARY_PATH|SIMULSPEAK_ONNX_RUNTIME_LIBRARY_PATH)=' "$(ENV_FILE)" | tail -1 | cut -d= -f2-); \
	if [[ -z "$$runtime" ]]; then \
		runtime="$(ONNX_LIB)"; \
	fi; \
	test -f "$$model" || (echo "Missing Silero model from $(ENV_FILE): $$model" && exit 1); \
	test -f "$$runtime" || (echo "Missing ONNX Runtime from $(ENV_FILE): $$runtime" && echo "Run: make install-onnx" && exit 1)

api: check-vad-config ## Start api-server.
	@echo "Starting api-server..."
	@set -a; source "$(ENV_FILE)"; set +a; $(GO) run -buildvcs=false ./cmd/api-server

pbx: check-vad-config ## Start pbx-node.
	@echo "Starting pbx-node..."
	@set -a; source "$(ENV_FILE)"; set +a; $(GO) run -buildvcs=false ./cmd/pbx-node

dev: check-vad-config ## Start pbx-node, api-server, and worker together; Ctrl-C stops all.
	@echo "Starting pbx-node, api-server, and worker..."
	@set -a; source "$(ENV_FILE)"; set +a; \
	$(GO) run -buildvcs=false ./cmd/pbx-node & \
	pbx_pid=$$!; \
	$(GO) run -buildvcs=false ./cmd/api-server & \
	api_pid=$$!; \
	SIMULSPEAK_WORKER_CONCURRENCY=$${SIMULSPEAK_DEV_WORKER_CONCURRENCY:-1} $(GO) run -buildvcs=false ./cmd/worker & \
	worker_pid=$$!; \
	trap 'kill $$pbx_pid $$api_pid $$worker_pid 2>/dev/null || true' INT TERM EXIT; \
	wait -n $$pbx_pid $$api_pid $$worker_pid

worker: env ## Start worker.
	@set -a; source "$(ENV_FILE)"; set +a; $(GO) run -buildvcs=false ./cmd/worker

seed: env ## Run demo seed command.
	@set -a; source "$(ENV_FILE)"; set +a; $(GO) run -buildvcs=false ./cmd/seed-demo

probe: ## Start the PBX probe frontend.
	@cd web/pbx-probe && $(NPM) run dev -- --host 127.0.0.1

probe-install: ## Install PBX probe frontend dependencies.
	@cd web/pbx-probe && $(NPM) install

test: ## Run all Go tests once.
	@$(GO) test ./... -count=1

test-pbx: ## Run PBX and command tests once.
	@$(GO) test ./internal/pbx/... ./cmd/... -count=1

test-silero: check-silero ## Run real Silero ONNX inference test.
	@SIMULSPEAK_TEST_SILERO_MODEL_PATH="$(abspath $(SILERO_MODEL))" \
	SIMULSPEAK_TEST_ONNX_RUNTIME_LIBRARY_PATH="$(abspath $(ONNX_LIB))" \
	SIMULSPEAK_TEST_SILERO_WAV_PATH="$(abspath $(SILERO_WAV))" \
	$(GO) test ./internal/ai/vad -run TestVAD_SileroRealModelInference -count=1 -v

fmt: ## Format Go code.
	@$(GO) fmt ./...

vet: ## Run go vet.
	@$(GO) vet ./...

build: ## Build command binaries into ./bin.
	@mkdir -p bin
	@$(GO) build -o bin/api-server ./cmd/api-server
	@$(GO) build -o bin/pbx-node ./cmd/pbx-node
	@$(GO) build -o bin/worker ./cmd/worker
	@$(GO) build -o bin/seed-demo ./cmd/seed-demo

install-third-party: install-onnx install-silero ## Install local ONNX Runtime and Silero model assets.

install-onnx: $(ONNX_LIB) ## Install ONNX Runtime locally under third_party/.

$(ONNX_LIB):
	@mkdir -p third_party
	@if [[ ! -f "/tmp/$(ONNX_ARCHIVE)" ]]; then \
		echo "Downloading $(ONNX_URL)"; \
		curl -sSL -o "/tmp/$(ONNX_ARCHIVE)" "$(ONNX_URL)"; \
	else \
		echo "Using cached /tmp/$(ONNX_ARCHIVE)"; \
	fi
	@tar -xzf "/tmp/$(ONNX_ARCHIVE)" -C third_party
	@file "$@"

install-silero: $(SILERO_MODEL) ## Install Silero ONNX model locally under third_party/.

$(SILERO_MODEL):
	@test -f "$(SILERO_SOURCE_MODEL)" || (echo "Missing source Silero model: $(SILERO_SOURCE_MODEL)" && exit 1)
	@mkdir -p "$(SILERO_DIR)"
	@cp "$(SILERO_SOURCE_MODEL)" "$@"
	@ls -lh "$@"

check-silero: install-third-party ## Check local Silero model and ONNX Runtime paths.
	@test -f "$(SILERO_MODEL)" || (echo "Missing Silero model: $(SILERO_MODEL)" && exit 1)
	@test -f "$(ONNX_LIB)" || (echo "Missing ONNX Runtime library: $(ONNX_LIB)" && exit 1)
	@echo "Silero model: $(abspath $(SILERO_MODEL))"
	@echo "ONNX Runtime: $(abspath $(ONNX_LIB))"

docker-dev: ## Start local development dependencies.
	@docker compose -f deployments/docker-compose.dev.yml up

docker-demo: ## Start the demo compose stack.
	@docker compose -f deployments/docker-compose.demo.yml up --build
