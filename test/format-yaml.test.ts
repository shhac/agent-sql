import { describe, test, expect } from "bun:test";
import yaml from "js-yaml";
import { formatYaml } from "../src/lib/format-yaml.ts";

const roundTrip = (data: unknown) => {
	const output = formatYaml(data);
	return yaml.load(output);
};

describe("formatYaml", () => {
	test("query result structure round-trips correctly", () => {
		const data = {
			columns: ["id", "name"],
			rows: [
				{ id: 1, name: "Alice" },
				{ id: 2, name: "Bob" },
			],
			pagination: { hasMore: false, rowCount: 2 },
		};
		expect(roundTrip(data)).toEqual(data);
	});

	test("preserves null values", () => {
		const data = { rows: [{ id: 1, name: null, bio: null }] };
		const output = formatYaml(data);
		expect(output).toContain("null");
		expect(roundTrip(data)).toEqual(data);
	});

	test("handles @truncated key", () => {
		const data = { rows: [{ id: 1, bio: "Short…", "@truncated": { bio: 12345 } }] };
		expect(roundTrip(data)).toEqual(data);
	});

	test("handles nested objects", () => {
		const data = {
			tables: [
				{ name: "users", columns: [{ name: "id", type: "INTEGER" }], indexes: [] },
			],
		};
		expect(roundTrip(data)).toEqual(data);
	});

	test("strings that look like booleans are preserved as strings", () => {
		const data = { rows: [{ val: "true" }, { val: "false" }, { val: "yes" }, { val: "no" }] };
		const result = roundTrip(data) as typeof data;
		expect(result.rows[0]?.val).toBe("true");
		expect(result.rows[1]?.val).toBe("false");
		expect(result.rows[2]?.val).toBe("yes");
		expect(result.rows[3]?.val).toBe("no");
	});

	test("strings that look like numbers are preserved as strings", () => {
		const data = { rows: [{ val: "123" }, { val: "1e3" }, { val: "0x1A" }] };
		const result = roundTrip(data) as typeof data;
		expect(result.rows[0]?.val).toBe("123");
		expect(result.rows[1]?.val).toBe("1e3");
		expect(result.rows[2]?.val).toBe("0x1A");
	});

	test("strings that look like dates are preserved as strings", () => {
		const data = { rows: [{ val: "2024-01-01" }] };
		const result = roundTrip(data) as typeof data;
		expect(result.rows[0]?.val).toBe("2024-01-01");
	});

	test("strings with special characters", () => {
		const data = {
			rows: [
				{ val: "has: colon" },
				{ val: 'has "quotes"' },
				{ val: "has\nnewline" },
				{ val: "has # hash" },
			],
		};
		expect(roundTrip(data)).toEqual(data);
	});

	test("empty arrays and objects", () => {
		const data = { tables: [], settings: {} };
		expect(roundTrip(data)).toEqual(data);
	});

	test("returns a string", () => {
		expect(typeof formatYaml({ foo: "bar" })).toBe("string");
	});
});
