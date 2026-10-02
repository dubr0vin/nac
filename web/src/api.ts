import { t } from "./i18n";
export {
  dayjs,
  dateRange,
  displayTime,
  toInstant,
  eventTimes,
  moveToSlot,
  allDayRange,
  effectiveTimezone,
} from "./dates";

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
  poll: number;
  timezone: string;
};
export type User = { id: string; login: string; name: string };
export type Member = { user: string };
export type Event = {
  taskId?: string;
  creator: string;
  editPolicy: "all" | "author";
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
export type EventDetail = {
  event: Event;
  tags: Record<string, Tags>;
  editable: boolean;
};
export type Occurrence = {
  taskId?: string;
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
  error?: Problem;
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
export type Task = {
  color?: string;
  stripe?: string;
  id: string;
  version: number;
  title: string;
  description: string;
  due: string;
  completed: boolean;
  tags: string[];
};
export type State = {
  tasks: Task[];
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
export type Problem = { code: string; params?: Record<string, unknown> };

export function problemMessage(problem: Problem) {
  const params = problem.params ?? {};
  let message = t(problem.code, {
    ns: "errors",
    defaultValue: t("http_error", { ns: "errors", ...params }),
    ...params,
  });
  for (const key of ["field", "uid", "line"]) {
    if (params[key] !== undefined)
      message = t(`context_${key}`, { ns: "errors", ...params, message });
  }
  return message;
}

export class APIError extends Error {
  constructor(
    public status: number,
    public problem: Problem,
  ) {
    super(problemMessage(problem));
  }
}

export async function readResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let problem: Problem = {
      code: `http_${response.status}`,
      params: { status: response.status },
    };
    if (response.headers.get("content-type")?.includes("application/json")) {
      const body = await response.json().catch(() => null);
      if (typeof body?.code === "string") problem = body;
    }
    throw new APIError(response.status, problem);
  }
  if (!response.headers.get("content-type")?.includes("application/json")) {
    throw new APIError(401, { code: "http_401" });
  }
  return response.json();
}

export async function api<T>(
  path: string,
  method = "GET",
  data?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(path, {
    method,
    signal,
    headers: { "Content-Type": "application/json", "X-NAC": "1" },
    body: data === undefined ? undefined : JSON.stringify(data),
    cache: "no-store",
  });
  return readResponse<T>(response);
}

export function message(error: unknown) {
  if (error instanceof APIError) return problemMessage(error.problem);
  return t(error instanceof Error ? error.message : String(error));
}
export const all: Rule = { op: "true" };
