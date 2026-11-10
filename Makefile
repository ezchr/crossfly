.PHONY: lint core full

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4 run ./...

# Crossfly Core: Dragonfly with Java Edition players, and nothing else (no commands, operators or console).
core:
	go build -o crossfly .

# Crossfly Full: Core with the commands, operators, console and flat world, mobs, Java redstone and capes.
full:
	go build -tags crossfly_full -o crossfly-full .
