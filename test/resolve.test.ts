import { describe, test, expect } from "bun:test";
import { detectDriverFromUrl } from "../src/drivers/resolve";

describe("detectDriverFromUrl", () => {
	test("detects postgres:// URLs", () => {
		expect(detectDriverFromUrl("postgres://localhost/db")).toBe("pg");
	});

	test("detects postgresql:// URLs", () => {
		expect(detectDriverFromUrl("postgresql://localhost/db")).toBe("pg");
	});

	test("detects mysql:// URLs", () => {
		expect(detectDriverFromUrl("mysql://localhost/db")).toBe("mysql");
	});

	test("detects mariadb:// URLs as mysql", () => {
		expect(detectDriverFromUrl("mariadb://localhost/db")).toBe("mysql");
	});

	test("detects sqlite:// URLs", () => {
		expect(detectDriverFromUrl("sqlite:///path/to/db")).toBe("sqlite");
	});

	test("detects .sqlite file extension", () => {
		expect(detectDriverFromUrl("/data/app.sqlite")).toBe("sqlite");
	});

	test("detects .db file extension", () => {
		expect(detectDriverFromUrl("/data/app.db")).toBe("sqlite");
	});

	test("detects .sqlite3 file extension", () => {
		expect(detectDriverFromUrl("/data/app.sqlite3")).toBe("sqlite");
	});

	test("detects .db3 file extension", () => {
		expect(detectDriverFromUrl("/data/app.db3")).toBe("sqlite");
	});

	test("returns undefined for unrecognized URLs", () => {
		expect(detectDriverFromUrl("http://example.com")).toBeUndefined();
	});

	test("file extension detection is case-insensitive", () => {
		expect(detectDriverFromUrl("/data/APP.DB")).toBe("sqlite");
	});
});
