module github.com/eulerbutcooler/iris/services/hooks

go 1.22

require (
	github.com/eulerbutcooler/iris/packages v0.0.0
	github.com/go-chi/chi/v5 v5.1.0
	github.com/google/uuid v1.6.0
	github.com/nats-io/nats.go v1.36.0
)

require (
	github.com/klauspost/compress v1.17.2 // indirect
	github.com/nats-io/nkeys v0.4.7 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.18.0 // indirect
	golang.org/x/sys v0.16.0 // indirect
)

replace github.com/eulerbutcooler/iris/packages => ../../packages
