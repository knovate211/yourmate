export PATH := $(PATH):$(HOME)/go/bin

.PHONY: proto gql gen test e2e tidy

proto:
	protoc -I proto --go_out=. --go_opt=module=github.com/yourmate/backend \
		--go-grpc_out=. --go-grpc_opt=module=github.com/yourmate/backend \
		proto/identity/v1/identity.proto proto/marketplace/v1/marketplace.proto proto/community/v1/community.proto

gql:
	cd services/gateway && go tool gqlgen generate

gen: proto gql

test:
	go test ./pkg/... ./services/...

# Needs the stack running (docker compose up from the repo root). Loads the
# stack's .env for the admin and database credentials.
e2e:
	set -a && . ../.env && set +a && go test -tags e2e -count=1 -v ./e2e/...

tidy:
	go mod tidy
