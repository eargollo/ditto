-- Create the test database so tests (make test, go test) use ditto_test and development (make run) uses ditto.
-- Runs once when the Postgres data volume is first created.
CREATE DATABASE ditto_test;
