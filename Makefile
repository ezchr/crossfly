.PHONY: lint core full

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4 run ./...

# Crossfly Core: Dragonfly with Java Edition players.
core:
	go build -o crossfly .

# Crossfly Full: Core with mobs, Java redstone, capes and the extra commands.
full:
	go build -tags crossfly_full -o crossfly-full .
