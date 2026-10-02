import { t } from "./i18n";
import dayjs from "dayjs";
import utc from "dayjs/plugin/utc";
import timezone from "dayjs/plugin/timezone";
import "dayjs/locale/ru";

dayjs.extend(utc);
dayjs.extend(timezone);

export { dayjs };

export type Rule = {
  op: "tag" | "not" | "and" | "or" | "true" | "false";
  tag?: string;
  children?: Rule[];
};
export type ColorRule = { rule: Rule; color: string; stripe?: string };
export type Settings = {
  config?: string;
  tags: string[];
  ownTags: string[];
  incomingTags: string[];
  busy: Rule;
  colors: ColorRule[];
  color: string;
  poll: number;
  timezone: string;
};
export type User = { id: string; login: string; name: string };
export type Member = { user: string; editor: boolean };
export type Event = {
  creator: string;
  editPolicy?: "all" | "author";
  id: string;
  uid?: string;
  source?: string;
  version: number;
  title: string;
  start: string;
  end: string;
  timezone: string;
  allDay: boolean;
  description: string;
  location: string;
  url: string;
  cancelled: boolean;
  rrule: string;
  rdates?: string[];
  exdates?: string[];
  categories?: string[];
  members: Member[];
  overrides?: Record<string, Event>;
};
export type Tags = { add: string[] | null; remove: string[] | null };
export type EventDetail = { event: Event; tags: Record<string, Tags> };
export type Occurrence = {
  id: string;
  eventId?: string;
  rid?: string;
  version?: number;
  title?: string;
  start: string;
  end: string;
  timezone?: string;
  allDay: boolean;
  description?: string;
  location?: string;
  url?: string;
  members?: Member[];
  participants?: { id: string; name: string }[];
  tags?: string[];
  color: string;
  stripe?: string;
  editable?: boolean;
  busy?: boolean;
  cancelled?: boolean;
};
export type Source = {
  id?: string;
  name: string;
  url: string;
  timezone: string;
  tags: string[];
  interval: number;
  lastAttempt?: string;
  lastSuccess?: string;
  error?: string;
  hasToken?: boolean;
};
export type Export = {
  id?: string;
  name: string;
  condition: string;
  fields: string[];
  view: "day" | "week" | "month" | "year" | "list";
  theme: "light" | "dark" | "auto";
  timezone: string;
  poll: number;
};
export type State = {
  me: User & { settings: Settings };
  users: User[];
  tags: string[];
  sources: Source[];
  exports: Export[];
};
export type PublicView = Pick<
  Export,
  "name" | "view" | "theme" | "timezone" | "poll"
> & {
  events: Occurrence[];
};
export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}
export async function api<T>(
  path: string,
  method = "GET",
  data?: unknown,
): Promise<T> {
  const response = await fetch(path, {
    method,
    headers: { "Content-Type": "application/json", "X-NAC": "1" },
    body: data === undefined ? undefined : JSON.stringify(data),
    cache: "no-store",
  });
  if (!response.ok) {
    const body = await response.text();
    let detail = body;
    try {
      detail = JSON.parse(body).message ?? body;
    } catch {
      /* Non-JSON proxy error. */
    }
    throw new APIError(
      response.status,
      t(detail.replace(/ in type [\w.]+/g, "")),
    );
  }
  const contentType = response.headers.get("content-type");
  if (!contentType?.includes("application/json")) {
    throw new APIError(
      401,
      t("Сессия закончилась. Обновите страницу для входа."),
    );
  }
  return response.json();
}
export function dateRange(date: string, zone: string, view = "month") {
  const unit = view === "year" ? "year" : "month";
  const padding = view === "year" ? 0 : 7;
  const start = dayjs.tz(date, zone).startOf(unit).subtract(padding, "day");
  const end = dayjs.tz(date, zone).endOf(unit).add(padding, "day");
  return new URLSearchParams({
    from: start.toISOString(),
    to: end.toISOString(),
  });
}
export function displayTime(value: string, zone: string) {
  return dayjs(value).tz(zone).format("YYYY-MM-DD HH:mm:ss");
}
export function toInstant(value: string, zone: string) {
  return dayjs.tz(value, zone).toISOString();
}
export function message(error: unknown) {
  const detail = error instanceof Error ? error.message : String(error);
  try {
    return t(JSON.parse(detail).message ?? detail);
  } catch {
    return t(detail);
  }
}
export const all: Rule = { op: "true" };

export function effectiveTimezone(value?: string) {
  return value || Intl.DateTimeFormat().resolvedOptions().timeZone;
}
