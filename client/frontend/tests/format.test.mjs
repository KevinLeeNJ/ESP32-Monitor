import test from "node:test";
import assert from "node:assert/strict";
import { fmtClockTime } from "../dist/services/format.js";

test("fmtClockTime hides zero and missing timestamps", () => {
  assert.equal(fmtClockTime("0001-01-01T00:00:00Z"), "—");
  assert.equal(fmtClockTime(null), "—");
  assert.notEqual(fmtClockTime("2026-11-03T12:00:00Z"), "—");
});
