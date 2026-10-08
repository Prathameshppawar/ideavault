// Package integration holds IdeaVault's DB-backed tests: migrations, repositories,
// service flows, the HTTP API and the agent (offline planner) end to end.
//
// All DB tests live in this one package so they never race on the shared test
// database. They run only when TEST_DATABASE_URL is set, e.g.
//
//	TEST_DATABASE_URL=postgres://ideavault:ideavault@localhost:5442/ideavault_test?sslmode=disable go test ./internal/integration
//
// WARNING: the public schema of that database is dropped and recreated.
package integration
