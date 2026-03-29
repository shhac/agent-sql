CREATE SCHEMA IF NOT EXISTS test_schema;
CREATE TABLE IF NOT EXISTS users (id SERIAL PRIMARY KEY, name TEXT NOT NULL, email TEXT UNIQUE, bio TEXT);
CREATE TABLE IF NOT EXISTS test_schema.events (id SERIAL PRIMARY KEY, user_id INTEGER REFERENCES users(id), type TEXT NOT NULL, data JSONB);
CREATE INDEX IF NOT EXISTS idx_events_user_id ON test_schema.events(user_id);
INSERT INTO users (name, email, bio) VALUES ('Alice', 'alice@test.com', 'A developer') ON CONFLICT DO NOTHING;
INSERT INTO users (name, email) VALUES ('Bob', 'bob@test.com') ON CONFLICT DO NOTHING;
