#!/usr/bin/env bash
set -euo pipefail

PASSED=0
FAILED=0
TOTAL=0
FAILURES=""

pass() {
  PASSED=$((PASSED + 1))
  TOTAL=$((TOTAL + 1))
  echo "  PASS: $1"
}

fail() {
  FAILED=$((FAILED + 1))
  TOTAL=$((TOTAL + 1))
  FAILURES="${FAILURES}\n  FAIL: $1"
  echo "  FAIL: $1"
}

# --- Determine binary ---
if [ -n "${1:-}" ]; then
  BIN="$1"
elif [ -f "./release/agent-sql-darwin-arm64" ]; then
  BIN="./release/agent-sql-darwin-arm64"
else
  BIN="bun run dev --"
fi
echo "Binary: $BIN"
echo ""

# --- Temp setup ---
TMPDIR_ROOT=$(mktemp -d)
DB_PATH="$TMPDIR_ROOT/smoke.db"
CONFIG_DIR="$TMPDIR_ROOT/config"
mkdir -p "$CONFIG_DIR/agent-sql"

cleanup() {
  rm -rf "$TMPDIR_ROOT"
}
trap cleanup EXIT

# Helper: run the binary with temp config
run_cmd() {
  XDG_CONFIG_HOME="$CONFIG_DIR" $BIN "$@"
}

# --- Test 1: --version ---
echo "=== Test: --version ==="
if run_cmd --version >/dev/null 2>&1; then
  pass "--version exits 0"
else
  fail "--version exits 0"
fi

# --- Test 2: --help ---
echo "=== Test: --help ==="
HELP_OUTPUT=$(run_cmd --help 2>&1 || true)
if echo "$HELP_OUTPUT" | grep -q "config" && echo "$HELP_OUTPUT" | grep -q "connection" && echo "$HELP_OUTPUT" | grep -q "query" && echo "$HELP_OUTPUT" | grep -q "schema"; then
  pass "--help contains expected commands"
else
  fail "--help contains expected commands"
fi

# --- Test 3: usage ---
echo "=== Test: usage ==="
USAGE_OUTPUT=$(run_cmd usage 2>&1 || true)
if [ -n "$USAGE_OUTPUT" ]; then
  pass "usage produces output"
else
  fail "usage produces output"
fi

# --- Test 4: config list-keys ---
echo "=== Test: config list-keys ==="
LIST_KEYS_OUTPUT=$(run_cmd config list-keys 2>/dev/null || true)
KEY_COUNT=$(echo "$LIST_KEYS_OUTPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('keys',d) if isinstance(d,dict) else d))" 2>/dev/null || echo "0")
if [ "$KEY_COUNT" = "5" ]; then
  pass "config list-keys returns valid JSON with 5 keys"
else
  fail "config list-keys returns valid JSON with 5 keys (got $KEY_COUNT)"
fi

# --- Set up temp SQLite DB ---
echo ""
echo "=== Setting up temp SQLite DB ==="
LONG_BIO="A very long biography that goes on and on about the life and times of Alice who is a remarkable person with many interesting stories to tell about her adventures in the world of software engineering and distributed systems and cloud computing and machine learning and artificial intelligence and all sorts of other topics that fill up more than two hundred characters easily"
sqlite3 "$DB_PATH" <<SQL
CREATE TABLE smoke_test (id INTEGER PRIMARY KEY, name TEXT, bio TEXT);
INSERT INTO smoke_test VALUES (1, 'Alice', '$LONG_BIO');
SQL

# --- Test 5: Add connection ---
echo "=== Test: connection add ==="
if run_cmd connection add smoke --driver sqlite --path "$DB_PATH" --default >/dev/null 2>&1; then
  pass "connection add succeeds"
else
  fail "connection add succeeds"
fi

# --- Test 6: connection list ---
echo "=== Test: connection list ==="
CONN_LIST=$(run_cmd connection list 2>&1 || true)
if echo "$CONN_LIST" | grep -q "smoke"; then
  pass "connection list shows the connection"
else
  fail "connection list shows the connection"
fi

# --- Test 7: connection test ---
echo "=== Test: connection test ==="
if run_cmd connection test 2>&1 | grep -qi "success\|ok"; then
  pass "connection test succeeds"
else
  fail "connection test succeeds"
fi

# --- Test 8: schema tables ---
echo "=== Test: schema tables ==="
TABLES_OUTPUT=$(run_cmd schema tables 2>&1 || true)
if echo "$TABLES_OUTPUT" | grep -q "smoke_test"; then
  pass "schema tables lists smoke_test"
else
  fail "schema tables lists smoke_test"
fi

# --- Test 9: schema describe ---
echo "=== Test: schema describe ==="
DESCRIBE_OUTPUT=$(run_cmd schema describe smoke_test 2>&1 || true)
if echo "$DESCRIBE_OUTPUT" | grep -q "id" && echo "$DESCRIBE_OUTPUT" | grep -q "name" && echo "$DESCRIBE_OUTPUT" | grep -q "bio"; then
  pass "schema describe shows columns"
else
  fail "schema describe shows columns"
fi

# --- Test 10: run SELECT ---
echo "=== Test: run SELECT ==="
RUN_OUTPUT=$(run_cmd run "SELECT * FROM smoke_test" 2>&1 || true)
if echo "$RUN_OUTPUT" | grep -q "Alice"; then
  pass "run SELECT returns rows"
else
  fail "run SELECT returns rows"
fi

# --- Test 11: query sample ---
echo "=== Test: query sample ==="
SAMPLE_OUTPUT=$(run_cmd query sample smoke_test 2>&1 || true)
if echo "$SAMPLE_OUTPUT" | grep -q "Alice"; then
  pass "query sample returns rows"
else
  fail "query sample returns rows"
fi

# --- Test 12: query count ---
echo "=== Test: query count ==="
COUNT_OUTPUT=$(run_cmd query count smoke_test 2>&1 || true)
if echo "$COUNT_OUTPUT" | grep -q "1"; then
  pass "query count returns count"
else
  fail "query count returns count"
fi

# --- Test 13: write blocked ---
echo "=== Test: write blocked ==="
if run_cmd run "INSERT INTO smoke_test VALUES (2, 'Bob', 'hi')" >/dev/null 2>&1; then
  fail "write blocked (should have exited non-zero)"
else
  pass "write blocked exits non-zero"
fi

# --- Test 14: write allowed ---
echo "=== Test: write allowed ==="
if run_cmd run "INSERT INTO smoke_test VALUES (2, 'Bob', 'hi')" --write >/dev/null 2>&1; then
  pass "write allowed with --write exits 0"
else
  fail "write allowed with --write exits 0"
fi

# --- MySQL tests (env var gated) ---
if [ -n "${AGENT_SQL_MYSQL_TEST_URL:-}" ]; then
  echo ""
  echo "=== MySQL tests ==="

  # --- MySQL Test: Add connection ---
  echo "=== Test: mysql connection add ==="
  if run_cmd connection add mysql_smoke --driver mysql --url "$AGENT_SQL_MYSQL_TEST_URL" --default >/dev/null 2>&1; then
    pass "mysql connection add succeeds"
  else
    fail "mysql connection add succeeds"
  fi

  # --- MySQL Test: connection test ---
  echo "=== Test: mysql connection test ==="
  if run_cmd connection test 2>&1 | grep -qi "success\|ok"; then
    pass "mysql connection test succeeds"
  else
    fail "mysql connection test succeeds"
  fi

  # --- MySQL Test: run SELECT ---
  echo "=== Test: mysql run SELECT ==="
  MYSQL_RUN_OUTPUT=$(run_cmd run "SELECT 1 AS ping" 2>&1 || true)
  if echo "$MYSQL_RUN_OUTPUT" | grep -q "ping"; then
    pass "mysql run SELECT returns rows"
  else
    fail "mysql run SELECT returns rows"
  fi

  # --- MySQL Test: schema tables ---
  echo "=== Test: mysql schema tables ==="
  MYSQL_TABLES_OUTPUT=$(run_cmd schema tables 2>&1 || true)
  if echo "$MYSQL_TABLES_OUTPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); sys.exit(0 if 'tables' in d else 1)" 2>/dev/null; then
    pass "mysql schema tables returns valid JSON"
  else
    fail "mysql schema tables returns valid JSON"
  fi

  # --- MySQL Test: write blocked ---
  echo "=== Test: mysql write blocked ==="
  if run_cmd run "CREATE TABLE _smoke_tmp (id INT)" >/dev/null 2>&1; then
    # Clean up if it somehow succeeded
    run_cmd run "DROP TABLE _smoke_tmp" --write >/dev/null 2>&1 || true
    fail "mysql write blocked (should have exited non-zero)"
  else
    pass "mysql write blocked exits non-zero"
  fi
else
  echo ""
  echo "=== Skipping MySQL tests (AGENT_SQL_MYSQL_TEST_URL not set) ==="
fi

# --- Summary ---
echo ""
echo "==============================="
echo "  Smoke test summary: $PASSED/$TOTAL passed"
if [ -n "$FAILURES" ]; then
  echo ""
  echo "  Failures:"
  echo -e "$FAILURES"
fi
echo "==============================="

if [ "$FAILED" -gt 0 ]; then
  exit 1
fi
exit 0
