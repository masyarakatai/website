# HuShield website -- build, test, deploy.
#
# The deploy host (pm-prod-spamfilter) is a t4g.large: Graviton/arm64, NOT
# x86_64. build-linux-arm64 asserts the result so a wrong-arch binary cannot
# reach the box and fail as "exec format error".

SHELL := /usr/bin/env bash

DEPLOY_HOST ?= 10.30.1.244
DEPLOY_USER ?= ec2-user
SSH_KEY     ?= $(HOME)/.ssh/pm-prod.pem

.PHONY: help test run build-linux-arm64 deploy

## help: List targets.
help:
	@echo "hushield-website -- targets:"
	@echo "  make test               go test ./... -race -cover"
	@echo "  make run                Run locally on 127.0.0.1:8081"
	@echo "  make build-linux-arm64  Cross-compile for the arm64 deploy host"
	@echo "  make deploy             test -> build -> ship -> restart -> health check"

## test: Full suite. Fixture-driven; makes no live API calls.
test:
	go test ./... -race -cover

## run: Local development server.
run:
	GITHUB_TOKEN=$${GITHUB_TOKEN:-$$(gh auth token 2>/dev/null)} go run ./cmd/web

## build-linux-arm64: Static binary for the deploy host.
build-linux-arm64:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/hushield-web ./cmd/web
	@file bin/hushield-web
	@file bin/hushield-web | grep -q 'ARM aarch64' \
		|| { echo "ERROR: not ARM aarch64; would not run on $(DEPLOY_HOST)." >&2; exit 1; }

## deploy: Gated on the test suite.
deploy: test build-linux-arm64
	DEPLOY_HOST=$(DEPLOY_HOST) DEPLOY_USER=$(DEPLOY_USER) SSH_KEY=$(SSH_KEY) ./scripts/deploy.sh
