#!/usr/bin/env bash
set -euo pipefail

# Seed all test databases with a standard schema.
# Requires: psql, mysql, sqlcmd (or mssql-tools)

PG_URL="postgres://test:test@localhost:15432/testdb?sslmode=disable"
MYSQL_HOST="localhost"
MYSQL_PORT="13306"
MARIADB_HOST="localhost"
MARIADB_PORT="13307"
MSSQL_HOST="localhost"
MSSQL_PORT="11433"

echo "==> Seeding PostgreSQL..."
psql "$PG_URL" <<'SQL'
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS users;

CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT,
    age INTEGER,
    bio TEXT
);

INSERT INTO users (name, email, age, bio) VALUES
    ('Alice', 'alice@test.com', 30, 'Software engineer'),
    ('Bob', NULL, 25, NULL),
    ('Charlie', 'charlie@test.com', 35, E'Line one\nLine two\nLine three'),
    ('Héloïse', 'heloise@test.com', 28, '日本語テスト'),
    ('Eve', 'eve@test.com', NULL, 'Has "quotes" and ''apostrophes''');

CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    amount NUMERIC(10,2) NOT NULL,
    note TEXT
);

INSERT INTO orders (user_id, amount, note) VALUES
    (1, 99.99, 'First order'),
    (1, 149.50, NULL),
    (2, 25.00, E'Rush delivery\nHandle with care'),
    (4, 0.01, 'Minimum amount'),
    (3, 1000.00, 'Bulk order —��special chars: €£¥');

CREATE INDEX idx_orders_user ON orders(user_id);
SQL

echo "==> Seeding MySQL..."
mysql -h "$MYSQL_HOST" -P "$MYSQL_PORT" -u root -ptest testdb <<'SQL'
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS users;

CREATE TABLE users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255),
    age INT,
    bio TEXT
) CHARACTER SET utf8mb4;

INSERT INTO users (name, email, age, bio) VALUES
    ('Alice', 'alice@test.com', 30, 'Software engineer'),
    ('Bob', NULL, 25, NULL),
    ('Charlie', 'charlie@test.com', 35, 'Line one\nLine two\nLine three'),
    ('Héloïse', 'heloise@test.com', 28, '日本語テスト'),
    ('Eve', 'eve@test.com', NULL, 'Has "quotes" and ''apostrophes''');

CREATE TABLE orders (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id INT,
    amount DECIMAL(10,2) NOT NULL,
    note TEXT,
    FOREIGN KEY (user_id) REFERENCES users(id)
) CHARACTER SET utf8mb4;

INSERT INTO orders (user_id, amount, note) VALUES
    (1, 99.99, 'First order'),
    (1, 149.50, NULL),
    (2, 25.00, 'Rush delivery\nHandle with care'),
    (4, 0.01, 'Minimum amount'),
    (3, 1000.00, 'Bulk order — special chars: €£¥');

CREATE INDEX idx_orders_user ON orders(user_id);
SQL

echo "==> Seeding MariaDB..."
mysql -h "$MARIADB_HOST" -P "$MARIADB_PORT" -u root -ptest testdb <<'SQL'
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS users;

CREATE TABLE users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255),
    age INT,
    bio TEXT
) CHARACTER SET utf8mb4;

INSERT INTO users (name, email, age, bio) VALUES
    ('Alice', 'alice@test.com', 30, 'Software engineer'),
    ('Bob', NULL, 25, NULL),
    ('Charlie', 'charlie@test.com', 35, 'Line one\nLine two\nLine three'),
    ('Héloïse', 'heloise@test.com', 28, '日本語テスト'),
    ('Eve', 'eve@test.com', NULL, 'Has "quotes" and ''apostrophes''');

CREATE TABLE orders (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id INT,
    amount DECIMAL(10,2) NOT NULL,
    note TEXT,
    FOREIGN KEY (user_id) REFERENCES users(id)
) CHARACTER SET utf8mb4;

INSERT INTO orders (user_id, amount, note) VALUES
    (1, 99.99, 'First order'),
    (1, 149.50, NULL),
    (2, 25.00, 'Rush delivery\nHandle with care'),
    (4, 0.01, 'Minimum amount'),
    (3, 1000.00, 'Bulk order — special chars: €£¥');

CREATE INDEX idx_orders_user ON orders(user_id);
SQL

echo "==> Seeding MSSQL..."
# Try mssql-tools18 first, fall back to mssql-tools
SQLCMD=$(command -v sqlcmd 2>/dev/null || echo "")
if [ -z "$SQLCMD" ]; then
    echo "sqlcmd not found, skipping MSSQL seed"
else
    # Create the database if it doesn't exist, then seed
    $SQLCMD -S "$MSSQL_HOST,$MSSQL_PORT" -U SA -P 'TestPass123!' -C -Q "
        IF DB_ID('testdb') IS NULL CREATE DATABASE testdb;
    " 2>/dev/null || $SQLCMD -S "$MSSQL_HOST,$MSSQL_PORT" -U SA -P 'TestPass123!' -Q "
        IF DB_ID('testdb') IS NULL CREATE DATABASE testdb;
    " 2>/dev/null

    $SQLCMD -S "$MSSQL_HOST,$MSSQL_PORT" -U SA -P 'TestPass123!' -d testdb -C -Q "
        IF OBJECT_ID('orders', 'U') IS NOT NULL DROP TABLE orders;
        IF OBJECT_ID('users', 'U') IS NOT NULL DROP TABLE users;

        CREATE TABLE users (
            id INT IDENTITY(1,1) PRIMARY KEY,
            name NVARCHAR(255) NOT NULL,
            email NVARCHAR(255),
            age INT,
            bio NVARCHAR(MAX)
        );

        INSERT INTO users (name, email, age, bio) VALUES
            (N'Alice', N'alice@test.com', 30, N'Software engineer'),
            (N'Bob', NULL, 25, NULL),
            (N'Charlie', N'charlie@test.com', 35, N'Line one
Line two
Line three'),
            (N'Héloïse', N'heloise@test.com', 28, N'日本語テスト'),
            (N'Eve', N'eve@test.com', NULL, N'Has \"quotes\" and ''apostrophes''');

        CREATE TABLE orders (
            id INT IDENTITY(1,1) PRIMARY KEY,
            user_id INT FOREIGN KEY REFERENCES users(id),
            amount DECIMAL(10,2) NOT NULL,
            note NVARCHAR(MAX)
        );

        INSERT INTO orders (user_id, amount, note) VALUES
            (1, 99.99, N'First order'),
            (1, 149.50, NULL),
            (2, 25.00, N'Rush delivery
Handle with care'),
            (4, 0.01, N'Minimum amount'),
            (3, 1000.00, N'Bulk order — special chars: €£¥');

        CREATE INDEX idx_orders_user ON orders(user_id);
    " 2>/dev/null || $SQLCMD -S "$MSSQL_HOST,$MSSQL_PORT" -U SA -P 'TestPass123!' -d testdb -Q "
        IF OBJECT_ID('orders', 'U') IS NOT NULL DROP TABLE orders;
        IF OBJECT_ID('users', 'U') IS NOT NULL DROP TABLE users;

        CREATE TABLE users (
            id INT IDENTITY(1,1) PRIMARY KEY,
            name NVARCHAR(255) NOT NULL,
            email NVARCHAR(255),
            age INT,
            bio NVARCHAR(MAX)
        );

        INSERT INTO users (name, email, age, bio) VALUES
            (N'Alice', N'alice@test.com', 30, N'Software engineer'),
            (N'Bob', NULL, 25, NULL),
            (N'Charlie', N'charlie@test.com', 35, N'Line one
Line two
Line three'),
            (N'Héloïse', N'heloise@test.com', 28, N'日本語テスト'),
            (N'Eve', N'eve@test.com', NULL, N'Has \"quotes\" and ''apostrophes''');

        CREATE TABLE orders (
            id INT IDENTITY(1,1) PRIMARY KEY,
            user_id INT FOREIGN KEY REFERENCES users(id),
            amount DECIMAL(10,2) NOT NULL,
            note NVARCHAR(MAX)
        );

        INSERT INTO orders (user_id, amount, note) VALUES
            (1, 99.99, N'First order'),
            (1, 149.50, NULL),
            (2, 25.00, N'Rush delivery
Handle with care'),
            (4, 0.01, N'Minimum amount'),
            (3, 1000.00, N'Bulk order — special chars: €£¥');

        CREATE INDEX idx_orders_user ON orders(user_id);
    "
fi

echo "==> Seeding complete."
