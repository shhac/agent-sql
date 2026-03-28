import { loadModule, parseSync } from "libpg-query";
import { withCatchSync } from "./with-catch";

type ValidationResult = { ok: true } | { ok: false; error: string };

export async function loadPgParser(): Promise<void> {
  await loadModule();
}

export function validateReadOnlyQuery(sql: string): ValidationResult {
  if (!sql.trim()) {
    return { ok: false, error: "Empty query" };
  }

  const [parseError, parsed] = withCatchSync(() => parseSync(sql));
  if (parseError) {
    return { ok: false, error: `SQL parse error: ${parseError.message}` };
  }

  const { stmts } = parsed;

  if (!stmts || stmts.length === 0) {
    return { ok: false, error: "Empty query" };
  }

  if (stmts.length > 1) {
    return {
      ok: false,
      error:
        "Query blocked: multiple statements detected. Only single statements are allowed in read-only mode.",
    };
  }

  const [first] = stmts;
  return validateStatement(first.stmt);
}

function validateStatement(stmt: Record<string, unknown>): ValidationResult {
  const stmtType = Object.keys(stmt)[0] as string | undefined;
  if (!stmtType) {
    return { ok: false, error: "Query blocked: empty statement" };
  }
  const stmtBody = stmt[stmtType] as Record<string, unknown>;

  switch (stmtType) {
    case "SelectStmt":
      return validateSelectStmt(stmtBody);

    case "ExplainStmt":
      return validateExplainStmt(stmtBody);

    case "VariableShowStmt":
      return { ok: true };

    case "CopyStmt":
      return validateCopyStmt(stmtBody);

    default:
      return {
        ok: false,
        error: `Query blocked: ${stmtType} is not allowed in read-only mode. Only SELECT, EXPLAIN, SHOW, and COPY TO are permitted.`,
      };
  }
}

function validateSelectStmt(body: Record<string, unknown>): ValidationResult {
  if (body.intoClause) {
    return {
      ok: false,
      error:
        "Query blocked: SELECT INTO is not allowed in read-only mode. INTO clause creates a new table.",
    };
  }

  if (body.lockingClause && Array.isArray(body.lockingClause) && body.lockingClause.length > 0) {
    return {
      ok: false,
      error:
        "Query blocked: SELECT with locking clause (FOR UPDATE/SHARE) is not allowed in read-only mode.",
    };
  }

  const withClause = body.withClause as
    | { ctes?: { CommonTableExpr?: Record<string, unknown> }[] }
    | undefined;
  if (withClause?.ctes) {
    for (const cte of withClause.ctes) {
      const expr = cte.CommonTableExpr;
      if (!expr?.ctequery) {
        continue;
      }
      const cteQuery = expr.ctequery as Record<string, unknown>;
      const innerResult = validateStatement(cteQuery);
      if (!innerResult.ok) {
        return {
          ok: false,
          error: `Query blocked: CTE contains a non-read-only statement. ${innerResult.error}`,
        };
      }
    }
  }

  return { ok: true };
}

function validateExplainStmt(body: Record<string, unknown>): ValidationResult {
  const innerStmt = body.query as Record<string, unknown> | undefined;
  if (!innerStmt) {
    return { ok: true };
  }

  return validateStatement(innerStmt);
}

function validateCopyStmt(body: Record<string, unknown>): ValidationResult {
  if (body.is_from) {
    return {
      ok: false,
      error:
        "Query blocked: COPY FROM is not allowed in read-only mode. Only COPY TO (data export) is permitted.",
    };
  }

  return { ok: true };
}
