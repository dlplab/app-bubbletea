# Simple Makefile for the Bubble Tea TUI

APP=launcher
PKG=./

.PHONY: build run tidy clean lint plan-destroy destroy

build:
	go build ./...

run:
	go run .

# Keep go.mod/go.sum tidy
 tidy:
	go mod tidy

clean:
	rm -f $(APP)

# Dry-run: run terraform plan -destroy in selected app path (APP_DIR required)
plan-destroy:
	@if [ -z "$(APP_DIR)" ]; then echo "Set APP_DIR=/path/to/terraform/apps/<appDir>"; exit 1; fi; \
	cd "$(APP_DIR)" && terraform plan -destroy -input=false

# Real destroy (APP_DIR required)
destroy:
	@if [ -z "$(APP_DIR)" ]; then echo "Set APP_DIR=/path/to/terraform/apps/<appDir>"; exit 1; fi; \
	cd "$(APP_DIR)" && terraform destroy -auto-approve -input=false
