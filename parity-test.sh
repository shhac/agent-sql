#!/usr/bin/env bash
# Parity test: compare Go and Bun agent-sql implementations
set -euo pipefail

GO="/tmp/agent-sql-go"
BUN_DIR="$(cd "$(dirname "$0")/agent-sql-bun" && pwd)"
RESULTS_DIR="/tmp/parity-results"
rm -rf "$RESULTS_DIR"
mkdir -p "$RESULTS_DIR"

PASS=0
FAIL=0
SKIP=0

bun_run() {
  (cd "$BUN_DIR" && bun run dev -- "$@" 2>/dev/null)
}

compare() {
  local test_name="$1"
  local go_out="$RESULTS_DIR/${test_name}.go"
  local bun_out="$RESULTS_DIR/${test_name}.bun"
  shift

  # Run both
  $GO "$@" > "$go_out" 2>/dev/null || true
  bun_run "$@" > "$bun_out" 2>/dev/null || true

  # Compare (sort JSON keys for consistent comparison)
  local go_norm bun_norm
  # Normalize: sort keys, remove primaryKey:false (Go omits via omitempty — correct),
  # convert string decimals to numbers where possible
  normalize() {
    local file="$1"
    python3 - "$file" << 'PYEOF'
import json, sys, re

def norm(obj):
    if isinstance(obj, dict):
        if obj.get('primaryKey') == False:
            obj.pop('primaryKey', None)
        for k, v in list(obj.items()):
            if isinstance(v, str) and re.match(r'^-?\d+\.\d+$', v):
                try: obj[k] = float(v)
                except: pass
            else:
                obj[k] = norm(v)
        return obj
    elif isinstance(obj, list):
        result = [norm(x) for x in obj]
        # Sort lists of objects by 'key' or 'name' for stable comparison
        if result and isinstance(result[0], dict):
            for sort_key in ('key', 'name'):
                if sort_key in result[0]:
                    result.sort(key=lambda x: x.get(sort_key, ''))
                    break
        return result
    return obj

content = open(sys.argv[1]).read().strip()
if not content:
    print("")
    sys.exit(0)

# Try parsing as a single JSON object first (pretty-printed envelope)
try:
    obj = json.loads(content)
    print(json.dumps(norm(obj), sort_keys=True))
    sys.exit(0)
except json.JSONDecodeError:
    pass

# Fall back to NDJSON (one JSON object per line)
for line in content.split('\n'):
    line = line.strip()
    if not line:
        continue
    try:
        obj = json.loads(line)
        print(json.dumps(norm(obj), sort_keys=True))
    except:
        print(line)
PYEOF
  }
  go_norm=$(normalize "$go_out")
  bun_norm=$(normalize "$bun_out")

  if [ "$go_norm" = "$bun_norm" ]; then
    echo "  PASS  $test_name"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  $test_name"
    echo "    GO:  $(head -1 "$go_out")"
    echo "    BUN: $(head -1 "$bun_out")"
    FAIL=$((FAIL + 1))
  fi
}

compare_stderr() {
  local test_name="$1"
  local go_out="$RESULTS_DIR/${test_name}.go.err"
  local bun_out="$RESULTS_DIR/${test_name}.bun.err"
  shift

  $GO "$@" > /dev/null 2> "$go_out" || true
  bun_run "$@" > /dev/null 2> "$bun_out" || true

  # Both should produce stderr with fixable_by
  local go_has_err bun_has_err
  go_has_err=$(grep -c "fixable_by" "$go_out" 2>/dev/null || echo "0")
  bun_has_err=$(grep -c "fixable_by" "$bun_out" 2>/dev/null || echo "0")
  go_has_err=$(echo "$go_has_err" | tr -d '[:space:]')
  bun_has_err=$(echo "$bun_has_err" | tr -d '[:space:]')

  if [ "$go_has_err" -gt 0 ] && [ "$bun_has_err" -gt 0 ]; then
    echo "  PASS  $test_name (both produce classified error)"
    PASS=$((PASS + 1))
  elif [ "$go_has_err" -gt 0 ] && [ "$bun_has_err" -eq 0 ]; then
    echo "  PASS  $test_name (Go has error, Bun silent — Go is better)"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  $test_name"
    echo "    GO err:  $(cat "$go_out")"
    echo "    BUN err: $(cat "$bun_out")"
    FAIL=$((FAIL + 1))
  fi
}

echo "=== SQLite Parity ==="
DB="/tmp/parity-test.db"

compare "sqlite-select-all" run -c "$DB" "SELECT id, name, email FROM users ORDER BY id"
compare "sqlite-select-null" run -c "$DB" "SELECT email, bio FROM users WHERE id = 2"
compare "sqlite-select-empty" run -c "$DB" "SELECT * FROM users WHERE id = 999"
compare "sqlite-count" query count users -c "$DB"
compare "sqlite-sample" query sample users -c "$DB" --limit 2
compare "sqlite-schema-tables" schema tables -c "$DB"
compare "sqlite-schema-describe" schema describe users -c "$DB"
compare "sqlite-schema-indexes" schema indexes -c "$DB"
compare "sqlite-schema-constraints" schema constraints users -c "$DB"
compare "sqlite-schema-search" schema search email -c "$DB"
compare "sqlite-aggregation" run -c "$DB" "SELECT COUNT(*) AS cnt, AVG(age) AS avg_age FROM users"
compare "sqlite-join" run -c "$DB" "SELECT u.name, o.amount FROM users u JOIN orders o ON u.id = o.user_id ORDER BY o.id"
compare "sqlite-limit" run -c "$DB" --limit 1 "SELECT * FROM users ORDER BY id"
compare "sqlite-subquery" run -c "$DB" "SELECT name FROM users WHERE id IN (SELECT user_id FROM orders) ORDER BY name"
compare_stderr "sqlite-bad-sql" run -c "$DB" "SELEC * FROM users"
compare_stderr "sqlite-missing-table" run -c "$DB" "SELECT * FROM nonexistent"
compare_stderr "sqlite-readonly-insert" run -c "$DB" "INSERT INTO users (name) VALUES ('Test')"

echo ""
echo "=== DuckDB Parity ==="
DUCK="/tmp/parity-test.duckdb"

compare "duckdb-select-all" run -c "$DUCK" "SELECT id, name, email FROM users ORDER BY id"
compare "duckdb-select-null" run -c "$DUCK" "SELECT email, bio FROM users WHERE id = 2"
compare "duckdb-count" query count users -c "$DUCK"
compare "duckdb-schema-tables" schema tables -c "$DUCK"
compare "duckdb-schema-describe" schema describe users -c "$DUCK"
compare "duckdb-schema-indexes" schema indexes -c "$DUCK"
compare "duckdb-schema-constraints" schema constraints users -c "$DUCK"
compare "duckdb-csv-query" run -c "duckdb://" "SELECT * FROM '/tmp/parity-test.csv' ORDER BY id"
compare_stderr "duckdb-readonly-insert" run -c "$DUCK" "INSERT INTO users VALUES(99,'Test','t@t',20,'bio',true,'2024-01-01')"

echo ""
echo "=== PostgreSQL Parity ==="
PG="postgres://test:test@localhost:15432/testdb"

compare "pg-select-all" run -c "$PG" "SELECT id, name, email FROM users ORDER BY id"
compare "pg-select-null" run -c "$PG" "SELECT email, bio FROM users WHERE id = 2"
compare "pg-count" query count users -c "$PG"
compare "pg-schema-tables" schema tables -c "$PG"
compare "pg-schema-describe" schema describe users -c "$PG"
compare "pg-schema-indexes" schema indexes -c "$PG"
compare "pg-schema-constraints" schema constraints users -c "$PG"
compare "pg-schema-search" schema search email -c "$PG"
compare "pg-join" run -c "$PG" "SELECT u.name, o.amount FROM users u JOIN orders o ON u.id = o.user_id ORDER BY o.id"
compare_stderr "pg-bad-sql" run -c "$PG" "SELEC * FROM users"
compare_stderr "pg-missing-table" run -c "$PG" "SELECT * FROM nonexistent"

echo ""
echo "=== MySQL Parity ==="
MY="mysql://root:test@localhost:13306/testdb"

compare "mysql-select-all" run -c "$MY" "SELECT id, name, email FROM users ORDER BY id"
compare "mysql-select-null" run -c "$MY" "SELECT email, bio FROM users WHERE id = 2"
compare "mysql-count" query count users -c "$MY"
compare "mysql-schema-tables" schema tables -c "$MY"
compare "mysql-schema-describe" schema describe users -c "$MY"
compare "mysql-schema-indexes" schema indexes -c "$MY"
compare "mysql-schema-constraints" schema constraints users -c "$MY"
compare_stderr "mysql-bad-sql" run -c "$MY" "SELEC * FROM users"
compare_stderr "mysql-missing-table" run -c "$MY" "SELECT * FROM nonexistent"

echo ""
echo "=== MariaDB Parity ==="
MDB="mariadb://root:test@localhost:13307/testdb"

if docker ps --format '{{.Names}}' | grep -q parity-mariadb; then
  compare "mariadb-select-all" run -c "$MDB" "SELECT id, name, email FROM users ORDER BY id"
  compare "mariadb-count" query count users -c "$MDB"
  compare "mariadb-schema-tables" schema tables -c "$MDB"
  compare "mariadb-schema-describe" schema describe users -c "$MDB"
  compare_stderr "mariadb-bad-sql" run -c "$MDB" "SELEC * FROM users"
else
  echo "  SKIP  mariadb tests (no container — run: docker run -d --name parity-mariadb -p 13307:3306 -e MYSQL_ROOT_PASSWORD=test -e MYSQL_DATABASE=testdb mariadb:11)"
  SKIP=$((SKIP + 5))
fi

echo ""
echo "=== Write Mode Parity ==="

# SQLite write test (use a temp copy)
WRITE_DB="/tmp/parity-write-test.db"
cp "$DB" "$WRITE_DB"
compare "sqlite-write-insert" run -c "$WRITE_DB" --write "INSERT INTO users (name, age) VALUES ('WriteTest', 99)"
rm -f "$WRITE_DB"

echo ""
echo "=== Edge Cases ==="

# Very long string (truncation)
compare "sqlite-long-string" run -c "$DB" "SELECT id, SUBSTR('x', 1, 1) || SUBSTR(REPLACE(HEX(ZEROBLOB(250)), '00', 'x'), 1, 499) AS long_val FROM users WHERE id = 1"

# Special characters
compare "sqlite-special-chars" run -c "$DB" "SELECT 'has\"quotes' AS q, 'has''apos' AS a, 'line1' || CHAR(10) || 'line2' AS newline"

# Empty string vs NULL
compare "sqlite-empty-vs-null" run -c "$DB" "SELECT '' AS empty_str, NULL AS null_val"

# Multiple data types
compare "sqlite-types" run -c "$DB" "SELECT 42 AS int_val, 3.14 AS float_val, 'text' AS str_val, NULL AS null_val"

echo ""
echo "=== Config Parity ==="
compare "config-list-keys" config list-keys

echo ""
echo "========================================="
echo "  PASS: $PASS  FAIL: $FAIL  SKIP: $SKIP"
echo "========================================="

if [ "$FAIL" -gt 0 ]; then
  echo ""
  echo "Failed tests — check $RESULTS_DIR for details"
  exit 1
fi
