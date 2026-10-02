import dayjs from "dayjs";
import utc from "dayjs/plugin/utc.js";
import timezone from "dayjs/plugin/timezone.js";
import "dayjs/locale/ru.js";

dayjs.extend(utc);
dayjs.extend(timezone);
export { dayjs };

export function effectiveTimezone(value?: string) {
  return value || Intl.DateTimeFormat().resolvedOptions().timeZone;
}

export function displayTime(value: string, zone: string) {
  return dayjs(value).tz(zone).format("YYYY-MM-DD HH:mm:ss");
}

export function toInstant(value: string, zone: string) {
  return dayjs.tz(value, zone).toISOString();
}

export function allDayRange(date: string, days: number, zone: string) {
  // Do calendar arithmetic before resolving each boundary's UTC offset.
  return {
    start: toInstant(date, zone),
    end: toInstant(dayjs(date).add(days, "day").format("YYYY-MM-DD"), zone),
  };
}

export function eventTimes(
  start: string,
  end: string,
  allDay: boolean,
  zone: string,
) {
  if (!allDay)
    return { start: toInstant(start, zone), end: toInstant(end, zone) };
  const date = start.slice(0, 10);
  const days = dayjs(end.slice(0, 10)).diff(dayjs(date), "day");
  return allDayRange(date, Math.max(1, days), zone);
}

export function moveToSlot(
  event: { start: string; end: string; allDay: boolean; timezone: string },
  slot: string,
  zone: string,
) {
  const eventZone = event.allDay ? event.timezone : zone;
  const previousStart = dayjs(event.start).tz(eventZone);
  const previousEnd = dayjs(event.end).tz(eventZone);
  const date = slot.slice(0, 10);
  if (event.allDay) {
    const days = dayjs(previousEnd.format("YYYY-MM-DD")).diff(
      dayjs(previousStart.format("YYYY-MM-DD")),
      "day",
    );
    return allDayRange(date, Math.max(1, days), eventZone);
  }
  const start = toInstant(
    slot.length === 10 ? `${date} ${previousStart.format("HH:mm:ss")}` : slot,
    zone,
  );
  return {
    start,
    end: dayjs(start)
      .add(previousEnd.diff(previousStart), "millisecond")
      .toISOString(),
  };
}

export function dateRange(date: string, zone: string, view = "month") {
  const unit = view === "year" ? "year" : "month";
  const padding = view === "year" ? 0 : 7;
  const start = dayjs(date).startOf(unit).subtract(padding, "day");
  const end = dayjs(date).startOf(unit).add(1, unit).add(padding, "day");
  return new URLSearchParams({
    from: toInstant(start.format("YYYY-MM-DD"), zone),
    to: toInstant(end.format("YYYY-MM-DD"), zone),
  });
}
