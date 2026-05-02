module github.com/eulerbutcooler/iris/services/worker

go 1.22

require (
	github.com/eulerbutcooler/iris/packages v0.0.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.6.0
	github.com/nats-io/nats.go v1.36.0
	golang.org/x/sync v0.7.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	github.com/klauspost/compress v1.17.2 // indirect
	github.com/nats-io/nkeys v0.4.7 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	golang.org/x/crypto v0.24.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.16.0 // indirect
)

replace github.com/eulerbutcooler/iris/packages => ../../packages
