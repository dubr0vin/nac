import { test } from "node:test";
import assert from "node:assert/strict";
import { allDayRange, eventTimes, moveToSlot, dateRange } from "./dates.ts";

test("all-day boundaries follow both DST transitions", () => {
  for (const [date, start, end] of [
    ["2026-03-29", "2026-03-28T23:00:00.000Z", "2026-03-29T22:00:00.000Z"],
    ["2026-10-25", "2026-10-24T22:00:00.000Z", "2026-10-25T23:00:00.000Z"],
  ]) {
    const expected = { start, end };
    assert.deepEqual(allDayRange(date, 1, "Europe/Berlin"), expected);
    assert.deepEqual(eventTimes(date, date, true, "Europe/Berlin"), expected);
    assert.deepEqual(
      moveToSlot(
        {
          start: "2026-03-01T23:00:00Z",
          end: "2026-03-02T23:00:00Z",
          allDay: true,
          timezone: "Europe/Berlin",
        },
        date,
        "UTC",
      ),
      expected,
    );
  }
});

test("timed moves preserve duration; query boundaries use the destination offset", () => {
  assert.deepEqual(
    moveToSlot(
      {
        start: "2026-03-28T09:00:00Z",
        end: "2026-03-28T10:30:00Z",
        allDay: false,
        timezone: "Europe/Berlin",
      },
      "2026-03-29",
      "Europe/Berlin",
    ),
    {
      start: "2026-03-29T08:00:00.000Z",
      end: "2026-03-29T09:30:00.000Z",
    },
  );
  const range = dateRange("2026-03-29", "Europe/Berlin");
  assert.equal(range.get("from"), "2026-02-21T23:00:00.000Z");
  assert.equal(range.get("to"), "2026-04-07T22:00:00.000Z");
});
