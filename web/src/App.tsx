import { useTranslation } from "react-i18next";
import i18n, { t, scheduleLabels } from "./i18n";
import { useEffect, useRef, useState } from "react";
import {
  Alert,
  DEFAULT_THEME,
  Badge,
  UnstyledButton,
  Button,
  Menu,
  Center,
  Container,
  Group,
  Loader,
  Modal,
  Paper,
  Stack,
  Text,
  Title,
  useMantineColorScheme,
} from "@mantine/core";
import {
  Schedule,
  AgendaView,
  type ScheduleViewLevel,
  type ScheduleEventData,
  type ScheduleEventProps,
} from "@mantine/schedule";
import {
  APIError,
  api,
  dateRange,
  dayjs,
  displayTime,
  effectiveTimezone,
  message,
  toInstant,
  type EventDetail,
  type Occurrence,
  type PublicView,
  type State,
} from "./api";
import { EventEditor } from "./EventEditor";
import { EventPopover } from "./EventPopover";
import { SettingsPanel } from "./SettingsPanel";

function resolveColor(color: string) {
  return DEFAULT_THEME.colors[color === "grey" ? "gray" : color]?.[6] ?? color;
}

function stripeBackground(event: Occurrence) {
  return event.stripe
    ? `repeating-linear-gradient(135deg, ${event.color} 0 8px, ${event.stripe} 8px 16px)`
    : undefined;
}

const renderEvent: NonNullable<ScheduleEventProps["renderEvent"]> = (
  event,
  props,
) => {
  const occurrence = event.payload?.event as Occurrence;
  return (
    <UnstyledButton
      {...props}
      style={{
        ...props.style,
        "--nac-event-stripes": stripeBackground(occurrence),
        textShadow: occurrence.stripe ? "0 1px 2px #000" : undefined,
      }}
    />
  );
};

function Calendar({
  events,
  date,
  view,
  zone,
  onDate,
  onView,
  onSelect,
  onMove,
  onCreate,
}: {
  events: Occurrence[];
  date: string;
  view: string;
  zone: string;
  onDate: (date: string) => void;
  onView?: (view: ScheduleViewLevel) => void;
  onSelect: (event: Occurrence, anchor: HTMLElement) => void;
  onMove?: (event: Occurrence, start: string, end: string) => void;
  onCreate?: (start: string, end: string, allDay?: boolean) => void;
}) {
  events = events.map((event) => ({
    ...event,
    color: resolveColor(event.color),
    stripe: event.stripe ? resolveColor(event.stripe) : undefined,
  }));
  const data: ScheduleEventData[] = events.map((event) => ({
    id: event.id,
    title: event.title ?? "",
    start: displayTime(
      event.start,
      event.allDay ? (event.timezone ?? zone) : zone,
    ),
    end: displayTime(event.end, event.allDay ? (event.timezone ?? zone) : zone),
    color: event.color,
    variant: "filled",
    payload: { event },
  }));
  function move(data: {
    event: ScheduleEventData;
    newStart: string;
    newEnd: string;
  }) {
    const event = data.event.payload?.event as Occurrence;
    if (event && onMove) onMove(event, data.newStart, data.newEnd);
  }
  const labels = scheduleLabels();

  if (view === "list") {
    return (
      <AgendaView
        rangeStart={dayjs(date).startOf("month").format("YYYY-MM-DD")}
        rangeEnd={dayjs(date).endOf("month").format("YYYY-MM-DD")}
        events={data}
        locale={i18n.language}
        labels={labels}
        renderEvent={renderEvent}
        onEventClick={(event, click) =>
          onSelect(event.payload?.event as Occurrence, click.currentTarget)
        }
      />
    );
  }
  return (
    <Schedule
      date={date}
      view={view as ScheduleViewLevel}
      onViewChange={onView}
      withAgenda={!!onView}
      onDateChange={onDate}
      events={data}
      locale={i18n.language}
      labels={labels}
      layout="responsive"
      dayViewProps={{
        withHeader: !!onView,
        classNames: { header: "calendar-header" },
        styles: { dayView: { "--day-view-slot-labels-width": "7rem" } },
        renderEvent,
        getCurrentTime: () => dayjs().tz(zone).format("YYYY-MM-DD HH:mm:ss"),
      }}
      weekViewProps={{
        withHeader: !!onView,
        classNames: { header: "calendar-header" },
        styles: { weekView: { "--week-view-slots-label-width": "7rem" } },
        renderEvent,
        firstDayOfWeek: 1,
        getCurrentTime: () => dayjs().tz(zone).format("YYYY-MM-DD HH:mm:ss"),
      }}
      monthViewProps={{
        withHeader: !!onView,
        classNames: { header: "calendar-header" },
        firstDayOfWeek: 1,
        renderEvent,
      }}
      yearViewProps={{
        withHeader: !!onView,
        classNames: { header: "calendar-header" },
      }}
      mobileMonthViewProps={{
        renderHeader: onView ? undefined : () => null,
        renderEvent,
      }}
      onTimeSlotClick={({ slotStart, slotEnd }) =>
        onCreate?.(slotStart, slotEnd)
      }
      onAllDaySlotClick={(date) =>
        onCreate?.(date, dayjs(date).add(1, "day").format("YYYY-MM-DD"), true)
      }
      onDayClick={(date) => onCreate?.(`${date} 09:00`, `${date} 10:00`)}
      withDragSlotSelect={!!onCreate}
      onSlotDragEnd={(start, end) => onCreate?.(start, end)}
      onEventClick={(event, click) =>
        onSelect(event.payload?.event as Occurrence, click.currentTarget)
      }
      withEventsDragAndDrop={!!onMove}
      withEventResize={!!onMove}
      canDragEvent={(event) => !!event.payload?.event.editable}
      canResizeEvent={(event) => !!event.payload?.event.editable}
      onEventDrop={move}
      onEventResize={move}
    />
  );
}

function editorFromURL(): Occurrence | undefined {
  if (location.pathname !== "/event") return undefined;
  const query = new URLSearchParams(location.search);
  const eventId = query.get("id") ?? "";
  const start = query.get("start") ?? new Date().toISOString();
  return {
    id: eventId,
    eventId: eventId || undefined,
    rid: query.get("rid") ?? undefined,
    start,
    end: query.get("end") ?? dayjs(start).add(1, "hour").toISOString(),
    allDay: query.get("allDay") === "true",
    color: "teal",
  };
}

export default function App() {
  useTranslation();
  const shared = location.pathname === "/view";
  const exportID = new URLSearchParams(location.search).get("id") ?? "";
  const [state, setState] = useState<State>();
  const [publicView, setPublicView] = useState<PublicView>();
  const [events, setEvents] = useState<Occurrence[]>([]);
  const [date, setDate] = useState(dayjs().format("YYYY-MM-DD"));
  const [view, setView] = useState("week");
  const [error, setError] = useState("");
  const [updated, setUpdated] = useState("");
  const [editor, setEditor] = useState<Occurrence | undefined>(editorFromURL);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [preview, setPreview] = useState<Occurrence>();
  const [selectedEvent, setSelectedEvent] = useState<{
    event: Occurrence;
    anchor: HTMLElement;
  }>();
  const [revision, setRevision] = useState(0);
  const { setColorScheme } = useMantineColorScheme();
  const zone = effectiveTimezone(
    publicView?.timezone ?? state?.me.settings.timezone,
  );
  const refresh = () => setRevision((value) => value + 1);
  const pollSeconds = useRef(15);

  useEffect(() => {
    const restore = () => setEditor(editorFromURL());
    window.addEventListener("popstate", restore);
    return () => window.removeEventListener("popstate", restore);
  }, []);

  useEffect(() => {
    setSelectedEvent(undefined);
  }, [date, view, settingsOpen]);

  function openEditor(event: Occurrence) {
    setSelectedEvent(undefined);
    const query = new URLSearchParams({
      start: event.start,
      end: event.end,
      allDay: String(event.allDay),
    });
    if (event.eventId) query.set("id", event.eventId);
    if (event.rid) query.set("rid", event.rid);
    history.pushState({ eventPage: true }, "", `/event?${query}`);
    setEditor(event);
  }
  function closeEditor() {
    if (history.state?.eventPage) history.back();
    else {
      history.replaceState(null, "", "/");
      setEditor(undefined);
    }
  }

  useEffect(() => {
    let active = true;
    let running = false;
    let timer: ReturnType<typeof setTimeout>;
    async function load() {
      if (running) return;
      running = true;
      clearTimeout(timer);
      try {
        if (shared) {
          const today = dayjs().tz(zone).format("YYYY-MM-DD");
          const query = dateRange(today, zone, view);
          query.set("id", exportID);
          const data = await api<PublicView>(`/view/data?${query}`);
          if (!active) return;
          setPublicView(data);
          setEvents(data.events);
          setPreview((current) =>
            current
              ? data.events.find((event) => event.id === current.id)
              : undefined,
          );
          setDate(today);
          setView(data.view);
          setColorScheme(data.theme);
          pollSeconds.current = data.poll;
        } else {
          const data = await api<State>("/api/state");
          const query = dateRange(
            date,
            effectiveTimezone(data.me.settings.timezone),
            view,
          );
          const events = await api<Occurrence[]>(`/api/events?${query}`);
          if (!active) return;
          setState(data);
          setEvents(events);
          pollSeconds.current = data.me.settings.poll;
        }
        setError("");
        setUpdated(dayjs().format("HH:mm:ss"));
      } catch (error) {
        if (!active) return;
        setError(message(error));
        if (
          error instanceof APIError &&
          [401, 403, 404].includes(error.status)
        ) {
          setEvents([]);
          setPreview(undefined);
          setEditor(undefined);
          if (shared) setPublicView(undefined);
          else setState(undefined);
        }
      } finally {
        running = false;
        if (active) timer = setTimeout(load, pollSeconds.current * 1000);
      }
    }
    const focus = () => {
      if (!document.hidden) void load();
    };
    void load();
    window.addEventListener("focus", focus);
    document.addEventListener("visibilitychange", focus);
    return () => {
      active = false;
      clearTimeout(timer);
      window.removeEventListener("focus", focus);
      document.removeEventListener("visibilitychange", focus);
    };
  }, [shared, exportID, date, revision, zone, view]);

  async function move(event: Occurrence, start: string, end: string) {
    try {
      const detail = await api<EventDetail>(`/api/events/${event.eventId}`);
      if (detail.event.version !== event.version)
        throw new Error(t("Событие изменилось. Обновите страницу."));
      const master = detail.event;
      const { overrides: _, ...body } = event.rid
        ? (master.overrides?.[event.rid] ?? master)
        : master;
      await api(
        `/api/events/${event.eventId}${event.rid ? `?rid=${encodeURIComponent(event.rid)}` : ""}`,
        "PUT",
        {
          ...body,
          version: master.version,
          members: master.members,
          rrule: master.rrule,
          start: toInstant(
            start,
            event.allDay ? (event.timezone ?? zone) : zone,
          ),
          end: toInstant(end, event.allDay ? (event.timezone ?? zone) : zone),
        },
      );
      refresh();
    } catch (error) {
      setError(message(error));
    }
  }
  function createEvent(
    start = `${date} 09:00`,
    end = `${date} 10:00`,
    allDay = false,
  ) {
    openEditor({
      id: "",
      start: toInstant(start, zone),
      end: toInstant(end, zone),
      allDay,
      color: state?.me.settings.color ?? "teal",
    });
  }
  function selectEvent(event: Occurrence, anchor: HTMLElement) {
    if (!event) return;
    if (!shared && event.eventId) setSelectedEvent({ event, anchor });
    else setPreview(event);
  }

  if (editor && state) {
    return (
      <EventEditor
        key={editor.id || "new"}
        state={state}
        occurrence={editor}
        onClose={closeEditor}
        onSaved={refresh}
      />
    );
  }

  return (
    <Container fluid p={shared ? "sm" : "lg"} className="app">
      <Stack gap="lg">
        <Group justify="space-between" align="center">
          {shared ? (
            <Title order={3}>{publicView?.name ?? "NAC"}</Title>
          ) : (
            <div className="brand">
              nac<span>●</span>
            </div>
          )}
          {!shared && state && (
            <Group gap="sm" ml="auto">
              <Button onClick={() => createEvent()}>{t("+ Событие")}</Button>
              <Menu position="bottom-end">
                <Menu.Target>
                  <Button
                    variant="subtle"
                    color="gray"
                    aria-label={t("Профиль")}
                  >
                    {state.me.name || state.me.login}
                  </Button>
                </Menu.Target>
                <Menu.Dropdown>
                  <Menu.Item onClick={() => setSettingsOpen(true)}>
                    {t("Настройки")}
                  </Menu.Item>
                  <Menu.Divider />
                  <Menu.Label>{t("Язык")}</Menu.Label>
                  <Menu.Item onClick={() => void i18n.changeLanguage("ru")}>
                    Русский
                  </Menu.Item>
                  <Menu.Item onClick={() => void i18n.changeLanguage("en")}>
                    English
                  </Menu.Item>
                </Menu.Dropdown>
              </Menu>
            </Group>
          )}
        </Group>
        {error && (
          <Alert color="red" title={t("Не удалось обновить данные")}>
            {shared && !publicView
              ? t("Ссылка недоступна или отозвана.")
              : error}
          </Alert>
        )}
        {(!shared && state) || (shared && publicView) ? (
          <Paper withBorder radius="lg" className="calendar">
            <Calendar
              events={events}
              date={date}
              view={view}
              zone={zone}
              onDate={setDate}
              onView={shared ? undefined : setView}
              onSelect={selectEvent}
              onMove={shared ? undefined : move}
              onCreate={shared ? undefined : createEvent}
            />
          </Paper>
        ) : (
          !error && (
            <Center py={100}>
              <Loader />
            </Center>
          )
        )}
        <Text size="xs" c="dimmed" ta="right">
          {updated && t("Обновлено {{time}}", { time: updated })}
        </Text>
      </Stack>
      {selectedEvent && state && (
        <EventPopover
          key={selectedEvent.event.id}
          event={selectedEvent.event}
          anchor={selectedEvent.anchor}
          tags={state.tags}
          zone={zone}
          onClose={() => setSelectedEvent(undefined)}
          onSaved={refresh}
          onEdit={(series) =>
            openEditor({
              ...selectedEvent.event,
              rid: series ? undefined : selectedEvent.event.rid,
            })
          }
        />
      )}
      {settingsOpen && state && (
        <SettingsPanel
          state={state}
          onClose={() => setSettingsOpen(false)}
          onSaved={refresh}
        />
      )}
      <Modal
        opened={!!preview}
        onClose={() => setPreview(undefined)}
        title={preview?.title ?? t("Интервал времени")}
      >
        {preview && (
          <Stack>
            <Text>
              {dayjs(preview.start).tz(zone).format("D MMMM, HH:mm")} —{" "}
              {dayjs(preview.end).tz(zone).format("D MMMM, HH:mm")}
            </Text>
            {preview.description && (
              <Text style={{ whiteSpace: "pre-wrap" }}>
                {preview.description}
              </Text>
            )}
            {preview.location && <Text>{preview.location}</Text>}
            {preview.url && <Text>{preview.url}</Text>}
            {preview.participants?.map((person) => (
              <Text key={person.id}>{person.name}</Text>
            ))}

            <Group>
              {preview.tags?.map((tag) => (
                <Badge key={tag}>{tag}</Badge>
              ))}
            </Group>
          </Stack>
        )}
      </Modal>
    </Container>
  );
}
